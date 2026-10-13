// Package lanes partitions a plan's jobs into lanes: the units a runner takes
// (specs/orun-lanes). A lane holds jobs of exactly one environment and is run
// in-process by `orun run --lane <id>`.
//
// Assign is called by the renderer after a plan's jobs are built and before
// its checksum, so a lane layout is part of the plan's identity. With no
// execution.lanes in the intent it does nothing at all: the plan carries no
// lanes and is byte-identical to a lane-unaware one (§4.6).
package lanes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// Options carries what the partition depends on besides the plan itself.
type Options struct {
	// Config is execution.lanes; nil means lanes are off.
	Config *model.IntentLanes
	// ComponentLanes maps a component name to its spec.lane.
	ComponentLanes map[string]string
	// MatchedTriggers are the trigger bindings that fired for this plan,
	// which select a rules entry.
	MatchedTriggers []string
	// Strict rejects any cycle that would need a split (--strict-lanes).
	Strict bool
	// CheckName gives a job's check title, the name of a job-mode lane.
	CheckName func(model.PlanJob) string
}

// Result reports what Assign decided beyond the plan itself.
type Result struct {
	// Mode is the effective mode after rules ("" when lanes are off).
	Mode string
	// Repairs describes every cycle repair, one line each (§7).
	Repairs []string
}

// draft is a lane under construction.
type draft struct {
	baseID      string
	env         string
	runsOn      string
	source      string
	group       string
	component   string
	concurrency int
	jobs        []int // indexes into plan.Jobs, in plan order
	splitFrom   string
	idSuffix    string // runner suffix when two lanes would share an id
	part        int    // 1-based slice number after a split; 0 when unsplit
	autoK       int    // 1-based auto lane number within its partition
	autoN       int    // auto lanes in its partition
}

// Assign partitions plan.Jobs into plan.Lanes and stamps each job's Lane.
func Assign(plan *model.Plan, opts Options) (Result, error) {
	cfg := opts.Config
	if plan == nil || cfg == nil {
		return Result{}, nil
	}
	mode := EffectiveMode(cfg, opts.MatchedTriggers)
	res := Result{Mode: mode}
	if len(plan.Jobs) == 0 {
		plan.Lanes = nil
		return res, nil
	}

	g := newJobGraph(plan.Jobs)
	drafts := initialDrafts(plan, cfg, mode, opts.ComponentLanes, g)

	repairs, err := repairCycles(plan, g, drafts, cfg, opts.Strict)
	if err != nil {
		return res, err
	}
	res.Repairs = repairs

	finalize(plan, g, *drafts, opts.CheckName)
	return res, nil
}

// EffectiveMode is the mode a plan is laid out in: the first rule whose
// trigger fired, else the configured mode, else job (§3.3).
func EffectiveMode(cfg *model.IntentLanes, matched []string) string {
	if cfg == nil {
		return ""
	}
	fired := make(map[string]struct{}, len(matched))
	for _, m := range matched {
		fired[m] = struct{}{}
	}
	for _, rule := range cfg.Rules {
		if _, ok := fired[rule.When.TriggerRef]; ok && rule.Mode != "" {
			return rule.Mode
		}
	}
	if cfg.Mode == "" {
		return model.LaneModeJob
	}
	return cfg.Mode
}

func laneConcurrency(cfg *model.IntentLanes) int {
	if cfg.Concurrency > 0 {
		return cfg.Concurrency
	}
	return model.DefaultLaneConcurrency
}

// initialDrafts applies resolution (§3.3) under the boundaries of §4: one
// lane per job in job mode; otherwise annotations first, then the mode.
func initialDrafts(plan *model.Plan, cfg *model.IntentLanes, mode string, compLanes map[string]string, g *jobGraph) *[]*draft {
	byKey := map[string]*draft{}
	var order []string
	var autoPending []int

	add := func(key string, mk func() *draft, i int) {
		d, ok := byKey[key]
		if !ok {
			d = mk()
			byKey[key] = d
			order = append(order, key)
		}
		d.jobs = append(d.jobs, i)
	}

	for i, job := range plan.Jobs {
		env, runsOn, comp := job.Environment, job.RunsOn, job.Component
		if mode == model.LaneModeJob {
			add("job\x00"+job.ID, func() *draft {
				return &draft{baseID: job.ID, env: env, runsOn: runsOn, source: "job", component: comp, concurrency: 1}
			}, i)
			continue
		}
		switch ann := compLanes[comp]; {
		case ann == model.LaneIsolated:
			add("iso\x00"+env+"\x00"+comp, func() *draft {
				return &draft{baseID: env + "/" + comp, env: env, runsOn: runsOn, source: "isolated", component: comp, concurrency: laneConcurrency(cfg)}
			}, i)
			continue
		case ann != "":
			conc := laneConcurrency(cfg)
			if grp, ok := cfg.Groups[ann]; ok && grp.Concurrency > 0 {
				conc = grp.Concurrency
			}
			add("grp\x00"+env+"\x00"+ann+"\x00"+runsOn, func() *draft {
				return &draft{baseID: env + "/" + ann, env: env, runsOn: runsOn, source: "group", group: ann, concurrency: conc}
			}, i)
			continue
		}
		switch mode {
		case model.LaneModeComponent:
			add("cmp\x00"+env+"\x00"+comp+"\x00"+runsOn, func() *draft {
				return &draft{baseID: env + "/" + comp, env: env, runsOn: runsOn, source: "component", component: comp, concurrency: laneConcurrency(cfg)}
			}, i)
		case model.LaneModeEnv:
			add("env\x00"+env+"\x00"+runsOn, func() *draft {
				return &draft{baseID: env, env: env, runsOn: runsOn, source: "env", concurrency: laneConcurrency(cfg)}
			}, i)
		default: // auto
			autoPending = append(autoPending, i)
		}
	}

	out := make([]*draft, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	if len(autoPending) > 0 {
		out = append(out, autoDrafts(plan, cfg, autoPending, len(out), g)...)
	}
	disambiguateRunsOn(out)
	return &out
}

// disambiguateRunsOn gives lanes that would share an id (same base, different
// runsOn) a suffix naming their runner, so ids stay unique (§4.2).
func disambiguateRunsOn(ds []*draft) {
	byBase := map[string][]*draft{}
	for _, d := range ds {
		byBase[d.baseID] = append(byBase[d.baseID], d)
	}
	for _, group := range byBase {
		if len(group) < 2 {
			continue
		}
		for _, d := range group {
			r := d.runsOn
			if r == "" {
				r = "default"
			}
			d.idSuffix = "-" + slug(r)
		}
	}
}

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// laneID is the draft's final id: the base, plus -<k> after a split.
func (d *draft) laneID() string {
	if d.part > 0 {
		return fmt.Sprintf("%s%s-%d", d.baseID, d.idSuffix, d.part)
	}
	return d.baseID + d.idSuffix
}

// finalize orders the drafts, names them, writes plan.Lanes and stamps jobs.
func finalize(plan *model.Plan, g *jobGraph, drafts []*draft, checkName func(model.PlanJob) string) {
	envRank := map[string]int{}
	for _, job := range plan.Jobs {
		if _, ok := envRank[job.Environment]; !ok {
			envRank[job.Environment] = len(envRank)
		}
	}
	minLevel := func(d *draft) int {
		m := -1
		for _, j := range d.jobs {
			if m < 0 || g.level[j] < m {
				m = g.level[j]
			}
		}
		return m
	}
	sort.SliceStable(drafts, func(a, b int) bool {
		da, db := drafts[a], drafts[b]
		if envRank[da.env] != envRank[db.env] {
			return envRank[da.env] < envRank[db.env]
		}
		if la, lb := minLevel(da), minLevel(db); la != lb {
			return la < lb
		}
		return da.laneID() < db.laneID()
	})

	plan.Lanes = make([]model.PlanLane, 0, len(drafts))
	for idx, d := range drafts {
		sort.Ints(d.jobs)
		ids := make([]string, len(d.jobs))
		for k, j := range d.jobs {
			ids[k] = plan.Jobs[j].ID
			plan.Jobs[j].Lane = d.laneID()
		}
		conc := d.concurrency
		if conc < 1 {
			conc = 1
		}
		lane := model.PlanLane{
			ID:          d.laneID(),
			Name:        laneName(plan, d, checkName),
			Index:       idx,
			Environment: d.env,
			Jobs:        ids,
			Concurrency: conc,
			RunsOn:      d.runsOn,
			Source:      d.source,
			Group:       d.group,
		}
		if d.splitFrom != "" {
			lane.SplitFrom = d.splitFrom
			lane.Reason = "cycle"
		}
		plan.Lanes = append(plan.Lanes, lane)
	}
}

const maxNameLen = 80

func laneName(plan *model.Plan, d *draft, checkName func(model.PlanJob) string) string {
	part := ""
	if d.part > 0 {
		part = fmt.Sprintf("·%d", d.part)
	}
	jobs := fmt.Sprintf("%d jobs", len(d.jobs))
	if len(d.jobs) == 1 {
		jobs = "1 job"
	}
	var name string
	switch d.source {
	case "job":
		job := plan.Jobs[d.jobs[0]]
		if checkName != nil {
			name = checkName(job)
		}
		if name == "" {
			name = job.ID
		}
	case "component":
		name = fmt.Sprintf("%s · %s%s", d.env, d.component, part)
	case "group":
		name = fmt.Sprintf("%s · %s%s · %s", d.env, d.group, part, jobs)
	case "env":
		name = fmt.Sprintf("%s · all%s · %s", d.env, part, jobs)
	case "isolated":
		name = fmt.Sprintf("%s · %s%s · isolated", d.env, d.component, part)
	case "auto":
		name = fmt.Sprintf("%s · %s %d/%d%s · %s", d.env, autoLabel(plan, d), d.autoK, d.autoN, part, listComponents(plan, d))
	}
	if r := []rune(name); len(r) > maxNameLen {
		name = string(r[:maxNameLen-1]) + "…"
	}
	return name
}

// autoLabel is the lanes' shared composition, else "auto" (§5).
func autoLabel(plan *model.Plan, d *draft) string {
	label := ""
	for _, j := range d.jobs {
		c := plan.Jobs[j].Composition
		if label == "" {
			label = c
		} else if c != label {
			return "auto"
		}
	}
	if label == "" {
		return "auto"
	}
	return label
}

// listComponents names up to three components, common suffix dropped (§5).
func listComponents(plan *model.Plan, d *draft) string {
	seen := map[string]struct{}{}
	var names []string
	for _, j := range d.jobs {
		c := plan.Jobs[j].Component
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		names = append(names, c)
	}
	sort.Strings(names)
	for _, suffix := range []string{"-worker", "-service", "-app"} {
		all := len(names) > 1
		for _, n := range names {
			if !strings.HasSuffix(n, suffix) {
				all = false
				break
			}
		}
		if all {
			for i, n := range names {
				names[i] = strings.TrimSuffix(n, suffix)
			}
			break
		}
	}
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s +%d", strings.Join(names[:3], ", "), len(names)-3)
}

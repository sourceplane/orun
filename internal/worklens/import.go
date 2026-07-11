package worklens

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ImportPlan is the deterministic output of `orun work import --dry-run`
// (cli-surface.md §3): the mapping from a specs tree to Spec/Task envelopes.
// It carries no ids and no keys — allocation happens at apply time through
// the cloud mutators, so the same tree always produces the same plan bytes
// (the golden-fixture property, P-4).
type ImportPlan struct {
	Workspace   string             `json:"workspace"`
	Root        string             `json:"root"`
	Initiatives []ImportInitiative `json:"initiatives,omitempty"`
	Specs       []ImportSpec       `json:"specs"`
	Milestones  []ImportMilestone  `json:"milestones,omitempty"`
	Tasks       []ImportTask       `json:"tasks"`
}

// ImportInitiative is one roadmap cluster mapped to an Initiative (v4 WH6):
// the cross-epic program register's rows become the top of the pyramid.
type ImportInitiative struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// ImportMilestone is one implementation-plan heading mapped to a milestone
// in the epic's ladder (v4 WH6) — the repo convention promoted to the
// primitive it always was. Ids that do not fit the milestone-key grammar
// (dotted sub-milestones) degrade to the v2 task-only mapping, visibly.
type ImportMilestone struct {
	SpecSlug string   `json:"specSlug"`
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Goal     string   `json:"goal,omitempty"`
	DoneWhen []string `json:"doneWhen,omitempty"`
	Ordinal  int      `json:"ordinal"`
}

// ImportSpec is one epic folder mapped to a Spec envelope.
type ImportSpec struct {
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	DocPath    string `json:"docPath"`              // forward-slash, relative to root
	DocSHA256  string `json:"docSha256"`            // content address of the verbatim doc body
	PlanPath   string `json:"planPath,omitempty"`   // implementation-plan.md when present
	Initiative string `json:"initiative,omitempty"` // roadmap cluster (partOf), v4
}

// ImportTask is one milestone mapped to a Task with a contract.
// IMPORTANT: lifecycle is NOT imported — hand-edited status tables stay
// behind; rungs derive from real observations after apply (design §6.4).
type ImportTask struct {
	SpecSlug    string    `json:"specSlug"`
	MilestoneID string    `json:"milestoneId"`          // e.g. "W0", "WP3", "SC2"
	Milestone   string    `json:"milestone,omitempty"`  // ladder key when the id fits the grammar (v4)
	Title       string    `json:"title"`
	Contract    *Contract `json:"contract,omitempty"`
}

var (
	titleRe     = regexp.MustCompile(`(?m)^#\s+(.+)$`)
	milestoneRe = regexp.MustCompile(`(?m)^##\s+([A-Z]{1,4}[0-9]+[a-z]?(?:\.[0-9]+)?)\s+[—–-]+\s+(.+)$`)
	depTokenRe  = regexp.MustCompile(`\b[A-Z]{1,4}[0-9]+[a-z]?\b`)
)

// ParseSpecTree walks a specs directory (each child folder with a README.md
// is an epic; its implementation-plan.md milestones become tasks) and
// returns the deterministic import plan. Archive folders are skipped.
func ParseSpecTree(root, workspace string) (*ImportPlan, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("worklens: read spec tree: %w", err)
	}
	plan := &ImportPlan{Workspace: workspace, Root: filepath.ToSlash(filepath.Clean(root))}

	var slugs []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "archive" || e.Name() == "_archive" {
			continue
		}
		if !ValidSlug(e.Name()) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), "README.md")); err != nil {
			continue
		}
		slugs = append(slugs, e.Name())
	}
	sort.Strings(slugs)

	for _, slug := range slugs {
		readmePath := filepath.Join(root, slug, "README.md")
		doc, err := os.ReadFile(readmePath)
		if err != nil {
			return nil, fmt.Errorf("worklens: %s: %w", readmePath, err)
		}
		title := slug
		if m := titleRe.FindSubmatch(doc); m != nil {
			title = strings.TrimSpace(string(m[1]))
		}
		sum := sha256.Sum256(doc)
		spec := ImportSpec{
			Slug:      slug,
			Title:     title,
			DocPath:   slug + "/README.md",
			DocSHA256: "sha256:" + hex.EncodeToString(sum[:]),
		}

		planPath := filepath.Join(root, slug, "implementation-plan.md")
		if body, err := os.ReadFile(planPath); err == nil {
			spec.PlanPath = slug + "/implementation-plan.md"
			milestones, tasks := parseMilestones(slug, string(body))
			plan.Milestones = append(plan.Milestones, milestones...)
			plan.Tasks = append(plan.Tasks, tasks...)
		}
		plan.Specs = append(plan.Specs, spec)
	}

	// v4 (WH6): the roadmap register, when present beside or above the tree,
	// maps clusters to initiatives and files each epic under its cluster.
	applyRoadmap(plan, root)
	return plan, nil
}

// clusterRowRe matches an epic-index row: | **XX** | [`epics/<name>/`](…) —
// the roadmap register's convention (specs/roadmap.md). Best-effort: rows
// that do not match are simply not initiatives.
var clusterRowRe = regexp.MustCompile("(?m)^\\|\\s*\\*\\*([A-Z][A-Z0-9]{0,5})\\*\\*[^|]*\\|\\s*\\[`(?:epics/)?([a-z0-9-]+)/?`\\]")

// applyRoadmap reads roadmap.md at the tree root or its parent and folds the
// cluster register into the plan: one Initiative per cluster, each named
// epic filed under it. Import writes intent, never decisions — an
// initiative here is an envelope, nothing more.
func applyRoadmap(plan *ImportPlan, root string) {
	var body []byte
	for _, p := range []string{filepath.Join(root, "roadmap.md"), filepath.Join(root, "..", "roadmap.md")} {
		if b, err := os.ReadFile(p); err == nil {
			body = b
			break
		}
	}
	if body == nil {
		return
	}
	specIdx := map[string]int{}
	for i := range plan.Specs {
		specIdx[plan.Specs[i].Slug] = i
	}
	seen := map[string]bool{}
	for _, m := range clusterRowRe.FindAllStringSubmatch(string(body), -1) {
		cluster, epicSlug := m[1], m[2]
		i, ok := specIdx[epicSlug]
		if !ok {
			continue // the register names an epic outside this tree
		}
		slug := strings.ToLower(cluster)
		if !seen[slug] {
			seen[slug] = true
			plan.Initiatives = append(plan.Initiatives, ImportInitiative{
				Slug:  slug,
				Title: "Cluster " + cluster,
			})
		}
		plan.Specs[i].Initiative = slug
	}
	sort.Slice(plan.Initiatives, func(a, b int) bool { return plan.Initiatives[a].Slug < plan.Initiatives[b].Slug })
}

// parseMilestones maps the repo's milestone convention
// (`## <ID> — <title>` + `**Goal:** … **Deps:** … **Done when:** …`)
// onto task contracts — the convention promoted to schema (WD-5 heritage).
// Each heading becomes a MILESTONE in the epic's ladder (v4: Goal/Done-when
// as the milestone contract) AND one task per milestone is materialized
// carrying the same contract — the v2 mapping preserved 1:1 under the new
// level, so the delivery fold's inputs are unchanged. Ids outside the
// milestone-key grammar (dotted sub-milestones) keep the v2 task-only
// mapping, visibly.
func parseMilestones(slug, body string) ([]ImportMilestone, []ImportTask) {
	locs := milestoneRe.FindAllStringSubmatchIndex(body, -1)
	var milestones []ImportMilestone
	var tasks []ImportTask
	ordinal := 0
	for i, loc := range locs {
		id := body[loc[2]:loc[3]]
		title := strings.TrimSpace(body[loc[4]:loc[5]])
		end := len(body)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		section := body[loc[1]:end]

		goal := sectionField(section, "Goal")
		doneWhen := splitList(sectionField(section, "Done when"))
		contract := &Contract{
			Goal:     goal,
			DoneWhen: doneWhen,
			Deps:     depTokens(sectionField(section, "Deps")),
			// Gates deliberately undeclared: the milestone convention names
			// no machine-verifiable gates, and honest degradation (P-7)
			// means merged imports park In Review rather than lie Done.
		}
		if contract.Goal == "" && len(contract.DoneWhen) == 0 && len(contract.Deps) == 0 {
			contract = nil
		}
		ladderKey := ""
		if ValidMilestoneKey(id) {
			ladderKey = id
			milestones = append(milestones, ImportMilestone{
				SpecSlug: slug,
				Key:      id,
				Title:    title,
				Goal:     goal,
				DoneWhen: doneWhen,
				Ordinal:  ordinal,
			})
			ordinal++
		}
		tasks = append(tasks, ImportTask{
			SpecSlug:    slug,
			MilestoneID: id,
			Milestone:   ladderKey,
			Title:       title,
			Contract:    contract,
		})
	}
	return milestones, tasks
}

// sectionField extracts the text after `**<name>:**` up to the next bold
// marker or blank line — the fields regularly share one paragraph
// ("**Deps:** W0. **Done when:** …").
func sectionField(section, name string) string {
	marker := "**" + name + ":**"
	i := strings.Index(section, marker)
	if i < 0 {
		return ""
	}
	rest := section[i+len(marker):]
	cut := len(rest)
	for _, stop := range []string{"**", "\n\n", "\n-", "\n*", "\n#"} {
		if j := strings.Index(rest, stop); j >= 0 && j < cut {
			cut = j
		}
	}
	return strings.TrimRight(strings.TrimSpace(collapseWhitespace(rest[:cut])), ".")
}

// splitList turns a done-when sentence into checklist items on ';'
// boundaries, preserving the source text otherwise (Q-4 heritage: no
// normalization beyond whitespace).
func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	var out []string
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// depTokens extracts milestone-id tokens ("W0", "SC2") from a Deps line;
// prose like "existing backend" carries no token and yields none.
func depTokens(s string) []string {
	if s == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, tok := range depTokenRe.FindAllString(s, -1) {
		if !seen[tok] {
			seen[tok] = true
			out = append(out, tok)
		}
	}
	return out
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

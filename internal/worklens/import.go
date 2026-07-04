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
	Workspace string       `json:"workspace"`
	Root      string       `json:"root"`
	Specs     []ImportSpec `json:"specs"`
	Tasks     []ImportTask `json:"tasks"`
}

// ImportSpec is one epic folder mapped to a Spec envelope.
type ImportSpec struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	DocPath   string `json:"docPath"`             // forward-slash, relative to root
	DocSHA256 string `json:"docSha256"`           // content address of the verbatim doc body
	PlanPath  string `json:"planPath,omitempty"`  // implementation-plan.md when present
}

// ImportTask is one milestone mapped to a Task with a contract.
// IMPORTANT: lifecycle is NOT imported — hand-edited status tables stay
// behind; rungs derive from real observations after apply (design §6.4).
type ImportTask struct {
	SpecSlug    string    `json:"specSlug"`
	MilestoneID string    `json:"milestoneId"` // e.g. "W0", "WP3", "SC2"
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
			plan.Tasks = append(plan.Tasks, parseMilestones(slug, string(body))...)
		}
		plan.Specs = append(plan.Specs, spec)
	}
	return plan, nil
}

// parseMilestones maps the repo's milestone convention
// (`## <ID> — <title>` + `**Goal:** … **Deps:** … **Done when:** …`)
// onto task contracts — the convention promoted to schema (WD-5 heritage).
func parseMilestones(slug, body string) []ImportTask {
	locs := milestoneRe.FindAllStringSubmatchIndex(body, -1)
	var tasks []ImportTask
	for i, loc := range locs {
		id := body[loc[2]:loc[3]]
		title := strings.TrimSpace(body[loc[4]:loc[5]])
		end := len(body)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		section := body[loc[1]:end]

		contract := &Contract{
			Goal:     sectionField(section, "Goal"),
			DoneWhen: splitList(sectionField(section, "Done when")),
			Deps:     depTokens(sectionField(section, "Deps")),
			// Gates deliberately undeclared: the milestone convention names
			// no machine-verifiable gates, and honest degradation (P-7)
			// means merged imports park In Review rather than lie Done.
		}
		if contract.Goal == "" && len(contract.DoneWhen) == 0 && len(contract.Deps) == 0 {
			contract = nil
		}
		tasks = append(tasks, ImportTask{
			SpecSlug:    slug,
			MilestoneID: id,
			Title:       title,
			Contract:    contract,
		})
	}
	return tasks
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

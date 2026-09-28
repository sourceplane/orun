package remotestate

import (
	"context"
	"net/http"
)

// Skills client (orun-initiatives-v2 IS5/IS6) — sealed, hosted policy over
// the wire. A skill is the playbook an agent follows; revisions are
// content-addressed (rev = sha256 of the canonical body), the Sourceplane
// default set ships with the cloud deploy, and an org-published revision
// of the same name shadows its default. Reads only on this seam:
// publishing requires work.approve and stays with the console and CLI-as-
// human; `orun agent run` materializes pinned revisions as native skill
// files and records them for the PR manifest (IS6).
//
// Methods take the org explicitly (the platformmcp seam's convention —
// the workspace argument travels with every call); CLI callers pass
// their resolved scope's org.

// Actor is a membership subject on the wire — the platform's principals
// (usr_/sp_/team_ ids). Carried here since WT2, where the work plane that
// used to define it was deleted; skills publishing still names who
// published a revision.
type Actor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Via  string `json:"via,omitempty"`
}

// Skill is one skill revision's metadata (SkillSummary on the wire).
type Skill struct {
	Name        string                 `json:"name"`
	Rev         string                 `json:"rev"`    // sha256:<hex> — of the canonical body, or of the bundle manifest when files ride along
	Source      string                 `json:"source"` // default | org
	Frontmatter map[string]interface{} `json:"frontmatter"`
	// Files lists the bundle's paths (saas-agent-skills SK2): `references/*`
	// an agent loads on demand and `templates/*` it copies. Empty for a
	// body-only skill; absent from an older platform, which reads the same.
	Files       []string `json:"files,omitempty"`
	PublishedBy *Actor   `json:"publishedBy,omitempty"`
	PublishedAt string   `json:"publishedAt,omitempty"`
}

// SkillFile is one file of a bundle: a path relative to the skill directory
// and its UTF-8 body.
type SkillFile struct {
	Path string `json:"path"`
	Body string `json:"body"`
}

// SkillsList mirrors SkillsListResponse.
type SkillsList struct {
	Skills []Skill `json:"skills"`
}

// SkillView is one skill with its body and its bundle files inline
// (SkillView on the wire) — one read materializes one revision whole.
type SkillView struct {
	Skill
	Body  string      `json:"body"`
	Files []SkillFile `json:"files,omitempty"`
}

func skillsPathFor(org, suffix string) string {
	return "/v1/organizations/" + urlSegment(org) + "/skills" + suffix
}

// ListSkills fetches every skill's latest revision — org rows shadowing
// the shipped defaults.
func (c *Client) ListSkills(ctx context.Context, org string) (*SkillsList, error) {
	var resp SkillsList
	if err := c.doJSON(ctx, http.MethodGet, skillsPathFor(org, ""), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetSkill fetches one skill's body + frontmatter — the latest revision,
// or exactly the one rev pins (a session re-reads what it ran under even
// after an org shadow lands).
func (c *Client) GetSkill(ctx context.Context, org, name, rev string) (*SkillView, error) {
	ref := name
	if rev != "" {
		ref += "@" + rev
	}
	var resp SkillView
	if err := c.doJSON(ctx, http.MethodGet, skillsPathFor(org, "/"+urlSegment(ref)), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

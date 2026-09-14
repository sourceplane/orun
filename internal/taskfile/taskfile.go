// Package taskfile reads authored task contract documents — the repo-side
// half of the orun-tasks plane (O2, design §3.2/§3.3). A contract lives in
// the repository as `tasks/<KEY>.TaskContract.yaml`, is authored offline like
// a SecretPolicy document, and is sealed by internal/contract.ContractID when
// it travels: `orun task create` uploads it, the task node bundles it, and
// the cloud verifies the same bytes on attach. This package never talks to
// the network — parse, validate, locate; the verbs own the rest.
package taskfile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sourceplane/orun/internal/contract"
	"github.com/sourceplane/orun/internal/remotestate"
)

const (
	wantAPIVersion = "orun.io/v1"
	wantKind       = "TaskContract"
	// Dir is the repo-root directory contract documents live in.
	Dir = "tasks"
	// Suffix is the document filename suffix, mirroring the
	// `<name>.SecretPolicy.yaml` convention.
	Suffix = ".TaskContract.yaml"
)

// keyRe is the task-key grammar (the key half of the provenance branch
// grammar; the cloud allocator is the only writer of keys, TK-I — this side
// only refuses names that could never have been issued).
var keyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,5}-[A-Z]?[0-9]+$`)

// Document is one parsed contract document.
type Document struct {
	// Key is metadata.name — the task the contract narrows.
	Key  string
	Path string
	// Contract is the authored contract, in the exact shape ContractID
	// seals. Never nil on a successful parse.
	Contract *contract.Contract
}

// rawDoc is the on-disk YAML shape. Gates is a pointer so an explicit empty
// list (`gates: []` — merge alone may finish the work) stays distinct from
// the key never having been written (gates unknown; TK-4's honest state).
type rawDoc struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Goal       string    `yaml:"goal"`
		Affects    []string  `yaml:"affects"`
		DoneWhen   []string  `yaml:"doneWhen"`
		Gates      *[]string `yaml:"gates"`
		DesignRefs []string  `yaml:"designRefs"`
		Deps       []string  `yaml:"deps"`
		Secrets    []string  `yaml:"secrets"`
		Envs       []string  `yaml:"envs"`
	} `yaml:"spec"`
}

// Parse strictly decodes one document: unknown fields are refused (a typoed
// `secerts:` silently narrowing nothing is exactly the failure a contract
// exists to prevent), as are wrong kinds and keys the allocator could never
// have issued.
func Parse(path string, body []byte) (*Document, error) {
	raw, err := decode(path, body)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(raw.Metadata.Name)
	if !keyRe.MatchString(key) {
		return nil, fmt.Errorf("taskfile %s: metadata.name %q is not a task key (ABC-123)", path, key)
	}
	if base := filepath.Base(path); base != key+Suffix {
		return nil, fmt.Errorf("taskfile %s: file for task %q must be named %s", path, key, key+Suffix)
	}
	return &Document{Key: key, Path: path, Contract: raw.contract()}, nil
}

// ParseTemplate decodes a contract document that is not yet bound to a key
// — the shape a bootstrap keeps beside its flows and hands to
// `orun task create --contract` before the allocator has issued anything.
// Same strict decoding as Parse; metadata.name is optional (kept as Key
// when it is a valid key, so a bound document parses too) and the filename
// is not checked. The returned Document has an empty Key when unbound.
func ParseTemplate(path string, body []byte) (*Document, error) {
	raw, err := decode(path, body)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(raw.Metadata.Name)
	if key != "" && !keyRe.MatchString(key) {
		return nil, fmt.Errorf("taskfile %s: metadata.name %q is not a task key (ABC-123) — omit it on a template", path, key)
	}
	return &Document{Key: key, Path: path, Contract: raw.contract()}, nil
}

// LoadTemplate reads and parses an unbound contract document at path.
func LoadTemplate(path string) (*Document, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("taskfile: %w", err)
	}
	return ParseTemplate(path, body)
}

func decode(path string, body []byte) (*rawDoc, error) {
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	var raw rawDoc
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("taskfile %s: %w", path, err)
	}
	if raw.APIVersion != wantAPIVersion {
		return nil, fmt.Errorf("taskfile %s: apiVersion %q (want %s)", path, raw.APIVersion, wantAPIVersion)
	}
	if raw.Kind != wantKind {
		return nil, fmt.Errorf("taskfile %s: kind %q (want %s)", path, raw.Kind, wantKind)
	}
	return &raw, nil
}

func (raw *rawDoc) contract() *contract.Contract {
	c := &contract.Contract{
		Goal:       raw.Spec.Goal,
		Affects:    raw.Spec.Affects,
		DoneWhen:   raw.Spec.DoneWhen,
		DesignRefs: raw.Spec.DesignRefs,
		Deps:       raw.Spec.Deps,
		Secrets:    raw.Spec.Secrets,
		Envs:       raw.Spec.Envs,
	}
	if raw.Spec.Gates != nil {
		c.Gates = *raw.Spec.Gates
		// An explicit empty list is a declaration; the canonical form
		// carries it as gatesDefined (the TS twin drops empty arrays).
		c.GatesDefined = len(*raw.Spec.Gates) == 0
	}
	return c
}

// Load reads and parses the document at path.
func Load(path string) (*Document, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("taskfile: %w", err)
	}
	return Parse(path, body)
}

// PathFor is where a task's document lives under a repo root.
func PathFor(root, key string) string {
	return filepath.Join(root, Dir, key+Suffix)
}

// FindForKey loads the document for a key if one exists; (nil, nil) when
// the file is absent — no document is a legal state (no contract ⇒ no
// narrowing, TK-4), not an error. The key is checked against the grammar
// BEFORE it becomes a path segment: create feeds this the key the cloud
// returned, and a hostile backend must not get to steer filesystem lookups.
func FindForKey(root, key string) (*Document, error) {
	if !keyRe.MatchString(key) {
		return nil, fmt.Errorf("taskfile: %q is not a task key", key)
	}
	path := PathFor(root, key)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("taskfile: %w", err)
	}
	return Load(path)
}

// Missing enumerates what stands between a contract and completeness, in
// the Complete() predicate's own terms — for the check verb to print, so
// "incomplete" always arrives with its reasons.
func Missing(c *contract.Contract) []string {
	var out []string
	if c == nil {
		return []string{"no contract"}
	}
	if c.Goal == "" {
		out = append(out, "goal")
	}
	if len(c.Affects) == 0 {
		out = append(out, "affects (≥1 component)")
	}
	if len(c.DoneWhen) == 0 {
		out = append(out, "doneWhen (≥1 criterion)")
	}
	if !c.GatesDefined && len(c.Gates) == 0 {
		out = append(out, "gates (declare a list, or an explicit empty list)")
	}
	return out
}

// Attacher is the one thing attaching a contract needs from the platform
// client. Narrow on purpose: *remotestate.Client satisfies it, and so does a
// fake, which is what lets the callers that attach contracts — `orun task
// create` and the bootstrap's `orun.task/ensure@v1` action — share one
// implementation of the seal-then-attach step instead of keeping two that can
// drift apart.
type Attacher interface {
	AttachTaskContract(ctx context.Context, org, keyOrID string, wire json.RawMessage, hash string) (*remotestate.TaskContractSeal, error)
}

// Attach seals a template's contract and attaches it to key, returning the
// content hash it was sealed under. The hash IS the identity: attaching the
// same document twice is the same attach, which is what makes a bootstrap's
// re-run heal a task rather than conflict with itself.
func Attach(ctx context.Context, to Attacher, org, key string, template *Document) (string, error) {
	if template == nil {
		return "", nil
	}
	hash, wire, err := contract.ContractID(template.Contract)
	if err != nil {
		return "", fmt.Errorf("sealing contract %s: %w", template.Path, err)
	}
	if _, err := to.AttachTaskContract(ctx, org, key, wire, hash); err != nil {
		return hash, fmt.Errorf("attaching contract %s: %w", template.Path, err)
	}
	return hash, nil
}

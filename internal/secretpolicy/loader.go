package secretpolicy

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sourceplane/orun/internal/model"
)

// Tiers holds the tier-ordered documents discovered for a scope: the loader
// returns them composition → stack → intent to match evaluation precedence
// (policy-model.md §5).
type Tiers struct {
	Composition []Document
	Stack       []Document
	Intent      []Document
}

// Ordered returns all documents in tier order (composition → stack → intent).
func (t Tiers) Ordered() []Document {
	out := make([]Document, 0, len(t.Composition)+len(t.Stack)+len(t.Intent))
	out = append(out, t.Composition...)
	out = append(out, t.Stack...)
	out = append(out, t.Intent...)
	return out
}

// fileSpec is a discovered-but-unparsed policy file.
type fileSpec struct {
	tier          Tier
	source        string
	componentType string // set only for composition fragments
	path          string
	data          []byte
}

// stackPolicyExts are the accepted stack/intent document filename suffixes.
var stackPolicyExts = []string{".SecretPolicy.yaml", ".secretpolicy.yaml", ".SecretPolicy.yml", ".secretpolicy.yml"}

// LoadStackRoot discovers composition-attached and stack-wide SecretPolicy
// documents within one resolved local stack root (already fetched by
// internal/composition). It reads stack.yaml for the stack name/version used in
// the stack-tier source label. Parse errors abort (strict); IO on absent dirs
// is tolerated. Composition fragments get their component.type injected.
func LoadStackRoot(fsys fs.FS) (composition, stack []Document, err error) {
	name, version := stackIdentity(fsys)
	specs, err := discoverStack(fsys, name, version)
	if err != nil {
		return nil, nil, err
	}
	for _, spec := range specs {
		doc, perr := parseSpec(spec)
		if perr != nil {
			return nil, nil, perr
		}
		switch spec.tier {
		case TierComposition:
			composition = append(composition, *doc)
		case TierStack:
			stack = append(stack, *doc)
		}
	}
	return composition, stack, nil
}

// LoadIntentPolicies discovers intent-overlay documents from
// policies/*.SecretPolicy.yaml at the repo root fsys. Parse errors abort.
func LoadIntentPolicies(fsys fs.FS) ([]Document, error) {
	specs, err := discoverIntent(fsys)
	if err != nil {
		return nil, err
	}
	var docs []Document
	for _, spec := range specs {
		doc, perr := parseSpec(spec)
		if perr != nil {
			return nil, perr
		}
		docs = append(docs, *doc)
	}
	return docs, nil
}

// LoadStackRootLenient mirrors LoadStackRoot but reports parse failures as
// error-severity Findings instead of aborting, so `orun policy lint` can report
// every problem in one pass.
func LoadStackRootLenient(fsys fs.FS) (composition, stack []Document, findings []Finding) {
	name, version := stackIdentity(fsys)
	specs, err := discoverStack(fsys, name, version)
	if err != nil {
		return nil, nil, []Finding{{Severity: SevError, Kind: "io", Message: err.Error()}}
	}
	for _, spec := range specs {
		doc, perr := parseSpec(spec)
		if perr != nil {
			findings = append(findings, Finding{Severity: SevError, Tier: spec.tier, Source: spec.source, Path: spec.path, Kind: "vocabulary", Message: perr.Error()})
			continue
		}
		switch spec.tier {
		case TierComposition:
			composition = append(composition, *doc)
		case TierStack:
			stack = append(stack, *doc)
		}
	}
	return composition, stack, findings
}

// LoadIntentPoliciesLenient is the lenient counterpart to LoadIntentPolicies.
func LoadIntentPoliciesLenient(fsys fs.FS) (docs []Document, findings []Finding) {
	specs, err := discoverIntent(fsys)
	if err != nil {
		return nil, []Finding{{Severity: SevError, Kind: "io", Message: err.Error()}}
	}
	for _, spec := range specs {
		doc, perr := parseSpec(spec)
		if perr != nil {
			findings = append(findings, Finding{Severity: SevError, Tier: TierIntent, Source: spec.source, Path: spec.path, Kind: "vocabulary", Message: perr.Error()})
			continue
		}
		docs = append(docs, *doc)
	}
	return docs, findings
}

func parseSpec(spec fileSpec) (*Document, error) {
	doc, err := ParseDocument(spec.data, spec.tier, spec.source, spec.path)
	if err != nil {
		return nil, err
	}
	if spec.tier == TierComposition {
		if err := doc.InjectComponentType(spec.componentType); err != nil {
			return nil, err
		}
	}
	return doc, nil
}

// discoverStack walks compositions/<type>/secret-policy.yaml (composition tier)
// and policies/*.SecretPolicy.yaml (stack tier) within a stack root.
func discoverStack(fsys fs.FS, stackName, stackVersion string) ([]fileSpec, error) {
	var specs []fileSpec

	// composition-attached fragments
	if entries, err := fs.ReadDir(fsys, "compositions"); err == nil {
		types := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				types = append(types, e.Name())
			}
		}
		sort.Strings(types)
		for _, typ := range types {
			p := path.Join("compositions", typ, "secret-policy.yaml")
			data, rerr := fs.ReadFile(fsys, p)
			if rerr != nil {
				continue
			}
			specs = append(specs, fileSpec{
				tier:          TierComposition,
				source:        "composition:" + typ,
				componentType: typ,
				path:          p,
				data:          data,
			})
		}
	}

	// stack-wide documents
	stackSpecs, err := discoverPolicyDir(fsys, "policies", TierStack, stackSource(stackName, stackVersion))
	if err != nil {
		return nil, err
	}
	specs = append(specs, stackSpecs...)
	return specs, nil
}

func discoverIntent(fsys fs.FS) ([]fileSpec, error) {
	return discoverPolicyDir(fsys, "policies", TierIntent, "intent")
}

// discoverPolicyDir lists *.SecretPolicy.yaml (and lowercase/.yml variants) in
// dir, sorted for determinism. A missing dir is not an error.
func discoverPolicyDir(fsys fs.FS, dir string, tier Tier, source string) ([]fileSpec, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, nil //nolint:nilerr // absent policies/ dir is a no-op, not a failure
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && hasPolicyExt(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var specs []fileSpec
	for _, n := range names {
		p := path.Join(dir, n)
		data, rerr := fs.ReadFile(fsys, p)
		if rerr != nil {
			return nil, fmt.Errorf("reading %s: %w", p, rerr)
		}
		specs = append(specs, fileSpec{tier: tier, source: source, path: p, data: data})
	}
	return specs, nil
}

func hasPolicyExt(name string) bool {
	for _, ext := range stackPolicyExts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

func stackSource(name, version string) string {
	if name == "" {
		name = "stack"
	}
	if version == "" {
		return "stack:" + name
	}
	return fmt.Sprintf("stack:%s@%s", name, version)
}

// stackIdentity reads stack.yaml (best-effort) for the name/version used in the
// stack-tier source label.
func stackIdentity(fsys fs.FS) (name, version string) {
	data, err := fs.ReadFile(fsys, "stack.yaml")
	if err != nil {
		return "", ""
	}
	var stack model.Stack
	if err := yaml.Unmarshal(data, &stack); err != nil {
		return "", ""
	}
	return stack.Metadata.Name, stack.Metadata.Version
}

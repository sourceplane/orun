// Package preview runs a repository's declared live preview (orun-cloud
// saas-parallel-baselining PB-11, PB-12): the dev server of the component
// intent.yaml names in `preview.component`, started from its spec.preview,
// after the previews of the components it `needs`.
//
// `orun preview` drives it from a terminal; `orun agent serve` keeps it
// running for a coding session through a Supervisor and reports each change
// as a preview_changed session event. Both share one state file and one log
// per component under <repo>/.orun/preview/.
package preview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/loader"
	"github.com/sourceplane/orun/internal/model"
)

// ErrNotDeclared reports that there is nothing to run yet: no intent.yaml, no
// `preview:` in it, or a named component that is not on disk. A coding
// session waits for it; any other error is a bad declaration.
var ErrNotDeclared = errors.New("no preview declared")

// Component is one process of a preview, resolved against the checkout.
type Component struct {
	Name string            `json:"name"`
	Dir  string            `json:"dir"`
	Run  []string          `json:"run"`
	Port int               `json:"port"`
	Path string            `json:"path"`
	Env  map[string]string `json:"env,omitempty"`
}

// Plan is what a preview runs: the needs first, in dependency order, and the
// shown component last.
type Plan struct {
	Root       string      `json:"root"`
	Components []Component `json:"components"`
}

// Target is the component the preview shows.
func (p *Plan) Target() Component { return p.Components[len(p.Components)-1] }

// Fingerprint identifies the declaration: it changes when anything that
// shapes a process does.
func (p *Plan) Fingerprint() string {
	b, _ := json.Marshal(p.Components)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// FindIntent returns the intent file directly in root, or "".
func FindIntent(root string) string {
	for _, name := range []string{"intent.yaml", "intent.yml"} {
		p := filepath.Join(root, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// Detect reads the preview the checkout at root declares. It returns an
// ErrNotDeclared error while there is nothing to run yet.
func Detect(root string) (*Plan, error) {
	path := FindIntent(root)
	if path == "" {
		return nil, fmt.Errorf("%w: no intent.yaml in %s", ErrNotDeclared, root)
	}
	intent, err := loader.LoadIntent(path)
	if err != nil {
		return nil, err
	}
	if intent.Preview == nil || strings.TrimSpace(intent.Preview.Component) == "" {
		return nil, fmt.Errorf("%w: intent.yaml has no preview.component", ErrNotDeclared)
	}
	// A discovery root that is not there yet is code that has not landed.
	for _, r := range intent.Discovery.Roots {
		if strings.TrimSpace(r) == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), filepath.FromSlash(r))); err != nil {
			return nil, fmt.Errorf("%w: discovery root %s is not on disk yet", ErrNotDeclared, r)
		}
	}
	return Load(path)
}

// Load resolves the preview the intent at intentPath declares.
func Load(intentPath string) (*Plan, error) {
	intent, _, err := loader.LoadResolvedIntent(intentPath)
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(filepath.Dir(intentPath))
	if err != nil {
		return nil, err
	}
	return Resolve(intent, root)
}

// Resolve turns a loaded intent's declaration into a Plan, with root the
// intent's directory. Every problem is reported, joined.
func Resolve(intent *model.Intent, root string) (*Plan, error) {
	if intent.Preview == nil || strings.TrimSpace(intent.Preview.Component) == "" {
		return nil, fmt.Errorf("%w: intent.yaml has no preview.component", ErrNotDeclared)
	}
	name := strings.TrimSpace(intent.Preview.Component)
	index := componentIndex(intent)
	if _, ok := index[name]; !ok {
		return nil, fmt.Errorf("%w: preview.component %q names no component (yet)", ErrNotDeclared, name)
	}
	order, errs, cycles := previewOrder(index, name)
	errs = append(errs, cycles...)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if errs := portClashes(index, order); len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	plan := &Plan{Root: root}
	for _, n := range order {
		c := index[n]
		dir, err := componentDir(root, c)
		if err != nil {
			return nil, err
		}
		pv := c.Preview
		path := pv.Path
		if path == "" {
			path = model.DefaultPreviewPath
		}
		env := map[string]string{}
		for k, v := range pv.Env {
			env[k] = v
		}
		plan.Components = append(plan.Components, Component{
			Name: n, Dir: dir, Run: append([]string(nil), pv.Run...), Port: pv.Port, Path: path, Env: env,
		})
	}
	return plan, nil
}

// portClashes reports two components of one preview on one port.
func portClashes(index map[string]model.Component, order []string) []error {
	var errs []error
	ports := map[int]string{}
	for _, n := range order {
		port := index[n].Preview.Port
		if other, dup := ports[port]; dup {
			errs = append(errs, fmt.Errorf("preview: components %s and %s both declare port %d", other, n, port))
			continue
		}
		ports[port] = n
	}
	return errs
}

// Validate reports every problem with an intent's preview declarations, for
// `orun validate`: each declared spec.preview's own fields and needs, and the
// intent's choice. No preview at all is not a problem.
func Validate(intent *model.Intent) []error {
	index := componentIndex(intent)
	names := make([]string, 0, len(index))
	for n, c := range index {
		if c.Preview != nil {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var errs []error
	for _, n := range names {
		errs = append(errs, specErrors(n, index[n].Preview)...)
		for _, need := range index[n].Preview.Needs {
			if nc, ok := index[need]; !ok {
				errs = append(errs, fmt.Errorf("component %s: spec.preview.needs names %q, which is no component", n, need))
			} else if nc.Preview == nil {
				errs = append(errs, fmt.Errorf("component %s: spec.preview.needs names %s, which declares no spec.preview", n, need))
			}
		}
	}
	if intent.Preview != nil {
		name := strings.TrimSpace(intent.Preview.Component)
		c, ok := index[name]
		switch {
		case name == "":
			errs = append(errs, fmt.Errorf("preview.component is empty"))
		case !ok:
			errs = append(errs, fmt.Errorf("preview.component %q names no component", name))
		case c.Preview == nil:
			errs = append(errs, fmt.Errorf("preview.component %s declares no spec.preview", name))
		default:
			// Each spec's own problems are reported above; add the chain's.
			order, _, cycles := previewOrder(index, name)
			errs = append(errs, cycles...)
			errs = append(errs, portClashes(index, order)...)
		}
	}
	return errs
}

// componentIndex maps names to components; the first declaration wins.
func componentIndex(intent *model.Intent) map[string]model.Component {
	index := make(map[string]model.Component, len(intent.Components))
	for _, c := range intent.Components {
		if _, seen := index[c.Name]; !seen {
			index[c.Name] = c
		}
	}
	return index
}

func specErrors(name string, pv *model.ComponentPreview) []error {
	var errs []error
	for _, msg := range model.PreviewProblems(pv.Run, pv.Port, pv.Path) {
		errs = append(errs, fmt.Errorf("component %s: spec.preview.%s", name, msg))
	}
	return errs
}

// previewOrder walks name's needs depth first and returns the components in
// start order (needs first, name last), the problems on the way, and the
// needs cycles apart.
func previewOrder(index map[string]model.Component, name string) (order []string, errs, cycles []error) {
	const (
		visiting = 1
		done     = 2
	)
	mark := map[string]int{}
	var visit func(n string, stack []string)
	visit = func(n string, stack []string) {
		c := index[n]
		if c.Preview == nil {
			if len(stack) == 0 {
				errs = append(errs, fmt.Errorf("preview.component %s declares no spec.preview", n))
			} else {
				errs = append(errs, fmt.Errorf("component %s: spec.preview.needs names %s, which declares no spec.preview", stack[len(stack)-1], n))
			}
			return
		}
		mark[n] = visiting
		errs = append(errs, specErrors(n, c.Preview)...)
		path := append(append([]string(nil), stack...), n)
		for _, need := range c.Preview.Needs {
			need = strings.TrimSpace(need)
			if _, ok := index[need]; !ok {
				errs = append(errs, fmt.Errorf("component %s: spec.preview.needs names %q, which is no component", n, need))
				continue
			}
			switch mark[need] {
			case visiting:
				cycles = append(cycles, fmt.Errorf("spec.preview.needs cycle: %s → %s", strings.Join(path, " → "), need))
			case done:
			default:
				visit(need, path)
			}
		}
		mark[n] = done
		order = append(order, n)
	}
	visit(name, nil)
	return order, errs, cycles
}

// componentDir is the directory a component's preview runs from: its path
// (or its manifest's directory) under root. It must exist.
func componentDir(root string, c model.Component) (string, error) {
	rel := strings.TrimSpace(c.Path)
	if rel == "" && c.SourcePath != "" {
		rel = filepath.Dir(filepath.FromSlash(c.SourcePath))
	}
	if strings.Contains(rel, "{{") {
		return "", fmt.Errorf("component %s: its path %q is templated; a preview runs from a plain directory", c.Name, rel)
	}
	dir := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	if filepath.IsAbs(rel) {
		dir = filepath.Clean(rel)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%w: component %s's directory %s is not on disk yet", ErrNotDeclared, c.Name, rel)
	}
	return dir, nil
}

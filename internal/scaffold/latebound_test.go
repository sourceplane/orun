package scaffold

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

// Late-bound inputs (orun-cloud saas-orun-work WA5, design §5.8): the scaffold
// stage writes a placeholder that "the live stage must replace and may never
// keep". The fixture is the tanstack-start shape in miniature — a workers.dev
// subdomain the scaffold records and the live stage deploys with.

const placeholder = "your-workers-subdomain"

const lateBoundBlueprint = `apiVersion: orun.dev/v1
kind: Blueprint
metadata: { name: app }
inputs:
  reponame: { type: string, required: true }
  subdomain:
    type: string
    default: ` + placeholder + `
    lateBound: true
modules:
  - name: identity
    mode: template
    files: { "values.json": "{{ .subdomain }}" }
  - name: deploy
    mode: template
    files: { "deploy.txt": "https://{{ .reponame }}.{{ .subdomain }}.workers.dev" }
phases:
  - name: 01-scaffold
    modules: [identity]
  - name: 02-live
    modules: [deploy]
`

func lateOpts(out string, inputs map[string]string) Options {
	return Options{
		Blueprint: []byte(lateBoundBlueprint),
		Inputs:    inputs,
		OutDir:    out,
		Store:     objectstore.NewMemStore(objectstore.AlgoSHA256),
	}
}

func lateSpecs(t *testing.T) map[string]InputSpec {
	t.Helper()
	bp, err := ParseBlueprint([]byte(lateBoundBlueprint))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return bp.Inputs
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func recorded(t *testing.T, out, name string) any {
	t.Helper()
	prov, err := ReadProvenance(out)
	if err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	return prov.Inputs[name]
}

// ── The declaration ─────────────────────────────────────────────────────────

func TestLateBoundParsesAndFalseReadsAsAbsent(t *testing.T) {
	if !lateSpecs(t)["subdomain"].LateBound {
		t.Fatal("lateBound: true was not read")
	}
	doc := strings.Replace(lateBoundBlueprint, "lateBound: true", "lateBound: false", 1)
	bp, err := ParseBlueprint([]byte(doc))
	if err != nil {
		t.Fatalf("lateBound: false must parse: %v", err)
	}
	if bp.Inputs["subdomain"].LateBound {
		t.Fatal("lateBound: false read as true")
	}
}

func TestLateBoundDeclarationsThatCannotWorkAreRefused(t *testing.T) {
	cases := []struct {
		name, from, to, want string
	}{
		{
			// The record holds a secret as `<secret>`: nothing to replace,
			// nothing to recognise as kept.
			name: "secret",
			from: "    lateBound: true\n", to: "    lateBound: true\n    secret: true\n",
			want: "lateBound and secret",
		},
		{
			// The default IS the placeholder.
			name: "no default",
			from: "    default: " + placeholder + "\n", to: "",
			want: "lateBound needs a default",
		},
		{
			// Late is relative to a phase boundary.
			name: "one phase",
			from: "  - name: 01-scaffold\n    modules: [identity]\n  - name: 02-live\n    modules: [deploy]\n",
			to:   "  - name: 01-scaffold\n    modules: [identity, deploy]\n",
			want: "at least two phases",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := strings.Replace(lateBoundBlueprint, c.from, c.to, 1)
			if doc == lateBoundBlueprint {
				t.Fatal("fixture replacement matched nothing")
			}
			_, err := ParseBlueprint([]byte(doc))
			if err == nil {
				t.Fatalf("parsed a lateBound input with %s", c.name)
			}
			if !strings.Contains(err.Error(), `"subdomain"`) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("the refusal must name the input and say why (%q); got %v", c.want, err)
			}
		})
	}
}

// ── Rule 1: a run stopped before the end keeps the placeholder ───────────────

func TestAStoppedRunPlacesThePlaceholder(t *testing.T) {
	out := t.TempDir()
	o := lateOpts(out, map[string]string{"reponame": "acme"})
	o.Until = "01-scaffold"
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("a scaffold-only run without the late value was refused: %v", err)
	}
	if got := readFile(t, filepath.Join(out, "values.json")); got != placeholder {
		t.Fatalf("the scaffold placed %q, want the placeholder", got)
	}
	if got := recorded(t, out, "subdomain"); got != placeholder {
		t.Fatalf("the record holds %v, want the placeholder it placed", got)
	}
	if _, err := os.Stat(filepath.Join(out, "deploy.txt")); err == nil {
		t.Fatal("a stopped run placed the live stage")
	}
}

// ── Rule 2: every other run must be given the value ─────────────────────────

func TestARunThatGoesPastTheStopMayNotKeepThePlaceholder(t *testing.T) {
	cases := []struct {
		name   string
		adjust func(*Options)
	}{
		{"a full build", func(*Options) {}},
		{"a resume with no stop", func(o *Options) { o.Resume = true }},
		// How the platform runs the live stage on its own.
		{"--phase the live stage", func(o *Options) { o.Only = "02-live" }},
		{"--phase --resume, as the driver runs it", func(o *Options) { o.Only, o.Resume = "02-live", true }},
		// --until the last phase stops nowhere.
		{"--until the last phase", func(o *Options) { o.Until = "02-live" }},
		// orun cannot tell a scaffold phase from a live one, so --phase is
		// held to the rule whichever it names.
		{"--phase the scaffold", func(o *Options) { o.Only = "01-scaffold" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := t.TempDir()
			o := lateOpts(out, map[string]string{"reponame": "acme"})
			c.adjust(&o)
			_, err := Run(context.Background(), o)
			if err == nil {
				t.Fatal("ran with the placeholder where the value must be")
			}
			var exit *ExitError
			if !errors.As(err, &exit) || exit.Code != 1 {
				t.Fatalf("want an input error (exit 1); got %v", err)
			}
			for _, want := range []string{"subdomain", placeholder, "--set subdomain=", "Nothing has been written"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("the refusal does not say %q:\n%v", want, err)
				}
			}
			// Before anything is placed.
			for _, f := range []string{"values.json", "deploy.txt"} {
				if _, statErr := os.Stat(filepath.Join(out, f)); statErr == nil {
					t.Fatalf("%s was placed despite the refusal", f)
				}
			}
		})
	}
}

func TestAGivenValueGoesLive(t *testing.T) {
	out := t.TempDir()
	if _, err := Run(context.Background(), lateOpts(out, map[string]string{"reponame": "acme", "subdomain": "acme-team"})); err != nil {
		t.Fatalf("a full build with the value: %v", err)
	}
	if got := readFile(t, filepath.Join(out, "deploy.txt")); got != "https://acme.acme-team.workers.dev" {
		t.Fatalf("deployed %q", got)
	}
}

// The rule is about a value nobody supplied. One the operator typed is theirs.
func TestAnExplicitValueIsTheOperatorsEvenWhenItIsThePlaceholder(t *testing.T) {
	if _, err := Run(context.Background(), lateOpts(t.TempDir(), map[string]string{"reponame": "acme", "subdomain": placeholder})); err != nil {
		t.Fatalf("an explicit value was refused: %v", err)
	}
}

// An unknown --until is the selection's to refuse, by name — not a complaint
// about an input it never got to.
func TestAnUnknownStopIsRefusedByTheSelectionFirst(t *testing.T) {
	o := lateOpts(t.TempDir(), map[string]string{"reponame": "acme"})
	o.Until = "99-nothing"
	_, err := Run(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "no phase named") {
		t.Fatalf("want the unknown phase named; got %v", err)
	}
}

// ── Rule 3: the resume rule exempts late-bound inputs ───────────────────────

// The two-stage build end to end: scaffold-only, then the live stage brings
// the value. Before WA5 the second run was refused as a conflict with the
// record, because the record held the placeholder.
func TestTheLiveStageReplacesTheRecordedPlaceholder(t *testing.T) {
	out := t.TempDir()
	scaffoldOnly := lateOpts(out, map[string]string{"reponame": "acme"})
	scaffoldOnly.Until, scaffoldOnly.Resume = "01-scaffold", true
	if _, err := Run(context.Background(), scaffoldOnly); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	inputs, err := RecoverInputs(out, map[string]string{"reponame": "acme", "subdomain": "acme-team"}, lateSpecs(t))
	if err != nil {
		t.Fatalf("the live stage's value was refused as a conflict: %v", err)
	}
	if inputs["subdomain"] != "acme-team" {
		t.Fatalf("recovered subdomain = %q, want the given value", inputs["subdomain"])
	}

	live := lateOpts(out, inputs)
	live.Only, live.Resume = "02-live", true
	if _, err := Run(context.Background(), live); err != nil {
		t.Fatalf("live: %v", err)
	}
	if got := readFile(t, filepath.Join(out, "deploy.txt")); got != "https://acme.acme-team.workers.dev" {
		t.Fatalf("the live stage deployed %q", got)
	}
	if got := recorded(t, out, "subdomain"); got != "acme-team" {
		t.Fatalf("the record holds %v; it must carry the new value", got)
	}
}

// A recorded placeholder is what the product was waiting for, not what it was
// built with. Recovered as a value, it would carry a live run that forgot the
// real one straight past rule 2.
func TestARecordedPlaceholderIsNotRecovered(t *testing.T) {
	out := t.TempDir()
	o := lateOpts(out, map[string]string{"reponame": "acme"})
	o.Until = "01-scaffold"
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	inputs, err := RecoverInputs(out, nil, lateSpecs(t))
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if inputs["reponame"] != "acme" {
		t.Fatalf("an ordinary input was not recovered: %v", inputs)
	}
	if v, ok := inputs["subdomain"]; ok {
		t.Fatalf("the placeholder was recovered as a value: %q", v)
	}

	// So a resume that goes live without the value is refused, by name.
	resume := lateOpts(out, inputs)
	resume.Resume = true
	if _, err := Run(context.Background(), resume); err == nil || !strings.Contains(err.Error(), "subdomain") {
		t.Fatalf("a live resume kept the placeholder; err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "deploy.txt")); err == nil {
		t.Fatal("the live stage was placed with the placeholder")
	}

	// And restarting the stopped build itself still works without it.
	restart := lateOpts(out, inputs)
	restart.Until, restart.Resume = "01-scaffold", true
	if _, err := Run(context.Background(), restart); err != nil {
		t.Fatalf("restarting the stopped build: %v", err)
	}
}

// A real value the record holds is recovered like any other: a resume of a
// live product in a fresh shell need not be told it again.
func TestARecordedRealValueIsRecovered(t *testing.T) {
	out := t.TempDir()
	if _, err := Run(context.Background(), lateOpts(out, map[string]string{"reponame": "acme", "subdomain": "acme-team"})); err != nil {
		t.Fatalf("run: %v", err)
	}
	inputs, err := RecoverInputs(out, nil, lateSpecs(t))
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if inputs["subdomain"] != "acme-team" {
		t.Fatalf("recovered %q", inputs["subdomain"])
	}
	// A given late-bound value replaces even a real recorded one, with no
	// conflict: the stage that binds it is the authority on it.
	inputs, err = RecoverInputs(out, map[string]string{"subdomain": "other"}, lateSpecs(t))
	if err != nil {
		t.Fatalf("a late-bound value was refused as a conflict: %v", err)
	}
	if inputs["subdomain"] != "other" {
		t.Fatalf("recovered %q, want the given value", inputs["subdomain"])
	}
}

// The exemption is for late-bound inputs only: every other disagreement with
// the record is still refused, naming both values.
func TestTheResumeRuleStillHoldsForEveryOtherInput(t *testing.T) {
	out := t.TempDir()
	if _, err := Run(context.Background(), lateOpts(out, map[string]string{"reponame": "acme", "subdomain": "acme-team"})); err != nil {
		t.Fatalf("run: %v", err)
	}
	_, err := RecoverInputs(out, map[string]string{"reponame": "different", "subdomain": "other"}, lateSpecs(t))
	if err == nil {
		t.Fatal("a disagreeing ordinary input was accepted")
	}
	if !strings.Contains(err.Error(), "reponame") || strings.Contains(err.Error(), "subdomain") {
		t.Fatalf("want only reponame refused; got %v", err)
	}
}

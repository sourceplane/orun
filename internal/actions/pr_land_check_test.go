package actions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── `checks: local`: the landing checks its commit here, then merges ────────

func (r *landRig) landChecked(t *testing.T, task, slug string, extra map[string]any) (Result, error) {
	t.Helper()
	raw := map[string]any{"task": task, "branchSlug": slug, "title": "phase(" + slug + ")", "checks": "local"}
	for k, v := range extra {
		raw[k] = v
	}
	params, err := Resolve("orun.pr/land@v1", raw)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return landWith(context.Background(), r.pen, Input{Dir: r.product, Params: params})
}

func standInCheck(t *testing.T, fn func(ctx context.Context, dir string, ev landingEvent) (string, error)) {
	t.Helper()
	prev := localCheck
	localCheck = fn
	t.Cleanup(func() { localCheck = prev })
}

// The fake GitHub serves no checks at all: a landing that read them would
// fail. A locally checked one merges without asking — with `wait` left at
// its default, which for `checks: ci` would wait.
func TestALocallyCheckedLandingMergesWithoutWaitingForCI(t *testing.T) {
	r := newLandRig(t)
	var seen landingEvent
	var headAtCheck, baseAtCheck string
	standInCheck(t, func(_ context.Context, dir string, ev landingEvent) (string, error) {
		seen = ev
		headAtCheck = gitT(t, dir, "rev-parse", "HEAD")
		baseAtCheck = gitT(t, r.bare, "rev-parse", "main")
		return "validated; the pull request plans 10 jobs, the merge to main plans 6 jobs", nil
	})
	r.write(t, map[string]string{"README.md": "hi\n", "intent.yaml": "kind: Intent\n"})

	res, err := r.landChecked(t, "BASE-1", "01-product", nil)
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	if res.Outputs["merged"] != "true" || res.Outputs["checked"] != "local" {
		t.Fatalf("outputs = %v, want merged and checked=local", res.Outputs)
	}
	if !strings.Contains(res.Outputs["checkSummary"], "10 jobs") {
		t.Errorf("checkSummary = %q, want what the check said", res.Outputs["checkSummary"])
	}
	// What was checked is the commit that was pushed, against the base it
	// merges into, under the branch name the pen gives it.
	if seen.headSHA != headAtCheck || seen.baseSHA != baseAtCheck || seen.base != "main" {
		t.Errorf("checked head %s base %s (%s), want head %s base %s (main)", seen.headSHA, seen.baseSHA, seen.base, headAtCheck, baseAtCheck)
	}
	if seen.headRef != "orun/BASE-1-01-product" || r.gh.opened[0]["head"] != seen.headRef {
		t.Errorf("checked as %q, opened as %v", seen.headRef, r.gh.opened[0]["head"])
	}
	if got := gitT(t, r.bare, "ls-tree", "-r", "--name-only", "main"); got != "README.md\nintent.yaml" {
		t.Errorf("main holds %q after the merge", got)
	}
	if did := prDid(map[string]any{}, res.Outputs); did != "Checked here and merged #1 into main." {
		t.Errorf("the step line reads %q", did)
	}
}

// A commit that does not check never becomes a pull request.
func TestALandingWhoseCheckFailsOpensNothing(t *testing.T) {
	r := newLandRig(t)
	standInCheck(t, func(context.Context, string, landingEvent) (string, error) {
		return "", errors.New("the landing's own check failed — orun plan for the pull request: component web: unknown type")
	})
	r.write(t, map[string]string{"intent.yaml": "kind: Intent\n"})
	res, err := r.landChecked(t, "BASE-1", "01-product", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("want the check's failure, got %v", err)
	}
	if len(r.gh.opened) != 0 || res.Outputs["merged"] == "true" {
		t.Fatalf("opened %d PR(s), outputs %v — a failed check must open none", len(r.gh.opened), res.Outputs)
	}
	if heads := gitT(t, r.bare, "for-each-ref", "--format=%(refname)", "refs/heads/orun/"); heads != "" {
		t.Errorf("the unchecked branch was pushed: %s", heads)
	}
	// Fixed and re-run, the same landing goes through.
	standInCheck(t, func(context.Context, string, landingEvent) (string, error) { return "validated", nil })
	r.write(t, map[string]string{"intent.yaml": "kind: Intent\n# fixed\n"})
	if res, err := r.landChecked(t, "BASE-1", "01-product", nil); err != nil || res.Outputs["merged"] != "true" {
		t.Fatalf("the re-run after the fix: %v %v", res.Outputs, err)
	}
}

func TestChecksIsCiOrLocal(t *testing.T) {
	r := newLandRig(t)
	r.write(t, map[string]string{"README.md": "hi\n"})
	if _, err := r.landChecked(t, "BASE-1", "01-product", map[string]any{"checks": "sometimes"}); err == nil || !strings.Contains(err.Error(), "ci") {
		t.Fatalf("want a refusal naming the two values, got %v", err)
	}
	if len(r.gh.opened) != 0 {
		t.Error("a bad parameter opened a pull request")
	}
}

// ── checkInBox runs this binary as a child: here, a stand-in that logs ──────

func standInOrun(t *testing.T, failOn string) (logFile string) {
	t.Helper()
	dir := t.TempDir()
	logFile = filepath.Join(dir, "calls.log")
	script := filepath.Join(dir, "orun")
	body := `#!/bin/sh
log="` + logFile + `"
echo "ARGS $* EVENT=${GITHUB_EVENT_NAME:-} RUN=${GITHUB_RUN_ID:-} OUT=${GITHUB_OUTPUT:-} TOKEN=${GITHUB_TOKEN:-}" >> "$log"
prev=""
for a in "$@"; do
  if [ "$prev" = "--event-file" ]; then cat "$a" >> "$log"; echo >> "$log"; fi
  if [ "$prev" = "--output" ]; then printf '{"jobs":[{},{},{}]}' > "$a"; fi
  prev="$a"
done
case "$* EVENT=${GITHUB_EVENT_NAME:-}" in
  *"` + failOn + `"*) echo "✗ component web: composition cloudflare-workers-assets-turbo not found"; exit 1;;
esac
echo "✓ fine"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	prev := orunBinary
	orunBinary = func() (string, error) { return script, nil }
	t.Cleanup(func() { orunBinary = prev })
	return logFile
}

func productWithIntent(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "intent.yaml"), []byte("kind: Intent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

var checkedEvent = landingEvent{repo: "acme/todos", base: "main", baseSHA: "b4se", headRef: "orun/BASE-1-01-product", headSHA: "he4d"}

func TestTheInBoxCheckValidatesThenPlansAsThePRAndAsTheMerge(t *testing.T) {
	logFile := standInOrun(t, "never")
	t.Setenv("GITHUB_RUN_ID", "999")
	t.Setenv("GITHUB_OUTPUT", "/tmp/somebody-elses-output")
	t.Setenv("GITHUB_TOKEN", "tok")

	said, err := checkInBox(context.Background(), productWithIntent(t), checkedEvent)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if said != "validated; the pull request plans 3 jobs, the merge to main plans 3 jobs" {
		t.Errorf("said %q", said)
	}
	raw, _ := os.ReadFile(logFile)
	log := string(raw)
	calls := []string{}
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, "ARGS ") {
			calls = append(calls, line)
		}
	}
	if len(calls) != 3 {
		t.Fatalf("ran %d commands, want validate and two plans:\n%s", len(calls), log)
	}
	if !strings.HasPrefix(calls[0], "ARGS validate --intent intent.yaml ") {
		t.Errorf("first: %s", calls[0])
	}
	for i, ev := range []string{"pull_request", "push"} {
		c := calls[i+1]
		if !strings.Contains(c, "plan --from-ci github --event-file ") || !strings.Contains(c, "--intent intent.yaml --changed --output ") || !strings.Contains(c, "EVENT="+ev+" ") {
			t.Errorf("plan %d: %s", i+1, c)
		}
	}
	// The build's own GitHub context never reaches the child; its token does.
	if strings.Contains(log, "RUN=999") || strings.Contains(log, "somebody-elses-output") || !strings.Contains(log, "TOKEN=tok") {
		t.Errorf("the child's environment leaked or lost something:\n%s", log)
	}
	// The events say what CI would be told about this commit.
	for _, want := range []string{
		`"action":"opened"`, `"head":{"ref":"orun/BASE-1-01-product","sha":"he4d"}`, `"base":{"ref":"main","sha":"b4se"}`,
		`"ref":"refs/heads/main"`, `"before":"b4se"`, `"after":"he4d"`, `"full_name":"acme/todos"`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("no %s in the events:\n%s", want, log)
		}
	}
}

func TestTheInBoxCheckNamesWhatFailedAndShowsItsOutput(t *testing.T) {
	standInOrun(t, "EVENT=push")
	_, err := checkInBox(context.Background(), productWithIntent(t), checkedEvent)
	if err == nil || !strings.Contains(err.Error(), "orun plan for the merge to main") || !strings.Contains(err.Error(), "composition cloudflare-workers-assets-turbo not found") {
		t.Fatalf("want the failing command named with its output, got %v", err)
	}
}

func TestATreeWithNoIntentHasNothingToPlan(t *testing.T) {
	logFile := standInOrun(t, "never")
	said, err := checkInBox(context.Background(), t.TempDir(), checkedEvent)
	if err != nil || said != "no intent to plan" {
		t.Fatalf("said %q, err %v", said, err)
	}
	if _, err := os.Stat(logFile); err == nil {
		t.Error("a tree with no intent ran orun")
	}
}

func TestRepoOfRemote(t *testing.T) {
	for remote, want := range map[string]string{
		"git@github.com:acme/todos.git":       "acme/todos",
		"https://github.com/acme/todos.git":   "acme/todos",
		"https://github.com/acme/todos":       "acme/todos",
		"ssh://git@github.com/acme/todos.git": "acme/todos",
		"/tmp/origin.git":                     "",
	} {
		if got := repoOfRemote(remote); got != want {
			t.Errorf("repoOfRemote(%q) = %q, want %q", remote, got, want)
		}
	}
}

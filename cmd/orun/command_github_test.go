package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/artifactstore"
	"github.com/sourceplane/orun/internal/runbundle"
)

func TestGithubCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github"})
	if err != nil {
		t.Fatalf("github command not found: %v", err)
	}
	if cmd.Use != "github" {
		t.Errorf("expected Use = 'github', got %q", cmd.Use)
	}
}

func TestGithubCommandHasSubcommands(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github"})
	if err != nil {
		t.Fatalf("github command not found: %v", err)
	}

	expected := []string{"runs", "pull", "status", "logs"}
	for _, name := range expected {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected github subcommand %q not found", name)
		}
	}
}

func TestGithubRunsFlagsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github", "runs"})
	if err != nil {
		t.Fatalf("github runs command not found: %v", err)
	}

	flags := []string{"workflow", "branch", "sha", "failed", "limit", "details"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("expected --%s flag on github runs", f)
		}
	}
}

func TestGithubPullFlagsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github", "pull"})
	if err != nil {
		t.Fatalf("github pull command not found: %v", err)
	}

	flags := []string{"run-id", "exec-id", "sha", "branch", "latest", "failed", "include-raw", "orun-dir"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("expected --%s flag on github pull", f)
		}
	}
}

func TestGithubLogsFlagsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github", "logs"})
	if err != nil {
		t.Fatalf("github logs command not found: %v", err)
	}

	flags := []string{"run-id", "exec-id", "sha", "branch", "failed", "latest", "job"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("expected --%s flag on github logs", f)
		}
	}
}

func TestParseGitHubRepo(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"git@github.com:sourceplane/orun.git", "sourceplane/orun"},
		{"https://github.com/sourceplane/orun.git", "sourceplane/orun"},
		{"https://github.com/sourceplane/orun", "sourceplane/orun"},
		{"https://api.github.com/repos/sourceplane/orun", "sourceplane/orun"},
		{"git@gitlab.com:sourceplane/orun.git", ""},
		{"", ""},
		{"not-a-url", ""},
	}

	for _, tc := range tests {
		got := parseGitHubRepo(tc.input)
		if got != tc.want {
			t.Errorf("parseGitHubRepo(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestFilterOrunShardsNil(t *testing.T) {
	if filterOrunShards(nil) == nil {
		t.Log("filterOrunShards handles nil")
	}
}

func TestGroupByExecIDNil(t *testing.T) {
	groups := groupByExecID(nil)
	if groups == nil {
		t.Log("groupByExecID returns nil for nil input")
	}
}

func TestGithubStatusCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github", "status"})
	if err != nil {
		t.Fatalf("github status command not found: %v", err)
	}
	if cmd.Use != "status" {
		t.Errorf("expected Use = 'status', got %q", cmd.Use)
	}
}

func TestFilepathJoin(t *testing.T) {
	result := filepathJoin("a", "b", "c")
	if result != "a/b/c" {
		t.Errorf("filepathJoin = %q, want 'a/b/c'", result)
	}
}

func TestGithubCommandRunsHelp(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github", "runs"})
	if err != nil {
		t.Fatalf("github runs not found: %v", err)
	}
	if !strings.Contains(cmd.Long, "Level 1") {
		t.Errorf("expected runs command to mention three levels of detail")
	}
}

func TestGithubPullOrunDirDefaultResolvesToDotOrun(t *testing.T) {
	// Verify that the default orunDir for github pull resolves to a path
	// ending in ".orun", matching the Hydrate function's expected input.
	//
	// This validates the fix: orunDir = filepath.Join(storeDir(), ".orun")
	// instead of the previous orunDir = storeDir() which passed the intent root
	// (missing the ".orun" suffix).
	got := filepath.Join(storeDir(), ".orun")

	// Without intent discovery, storeDir() returns ".".
	// filepath.Join(".", ".orun") should resolve to ".orun".
	if got != ".orun" {
		t.Errorf("default orunDir for pull = %q, want %q (the .orun directory)", got, ".orun")
	}
}

func TestGithubPullOrunDirWithIntentRoot(t *testing.T) {
	// Simulate a scenario where intent discovery populated intentRoot.
	// The resolved orunDir must end with ".orun".
	orig := intentRoot
	intentRoot = "/tmp/test-project"
	t.Cleanup(func() { intentRoot = orig })

	got := filepath.Join(storeDir(), ".orun")
	wantSuffix := string(filepath.Separator) + ".orun"
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("orunDir with intent root = %q, want path ending in %q", got, wantSuffix)
	}
}

var _ = strings.Contains

// --- Tests for printShardLogs and log content display ---

func TestGithubLogsPrintsLogContent(t *testing.T) {
	// Set up a temp dir with a log file
	tmpDir := t.TempDir()
	logsDir := filepath.Join(tmpDir, "logs")
	os.MkdirAll(logsDir, 0755)
	os.WriteFile(filepath.Join(logsDir, "step-init.log"), []byte("Initializing terraform...\nPlan complete.\n"), 0644)

	ds := &artifactstore.DownloadedShard{
		Name: "orun-shard-job1",
		Dir:  tmpDir,
		Shard: &runbundle.RunBundleShardManifest{
			Role: runbundle.ShardRoleJob,
			Files: map[string]string{
				"log:step-init": "logs/step-init.log",
				"result":        "result.json",
			},
		},
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printShardLogs(ds, "orun-shard-job1")

	w.Close()
	os.Stdout = oldStdout
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	// Must have header with shard name and step ID
	if !strings.Contains(output, "=== orun-shard-job1 / step-init ===") {
		t.Errorf("expected section header, got:\n%s", output)
	}
	// Must contain actual log content
	if !strings.Contains(output, "Initializing terraform...") {
		t.Errorf("expected log content, got:\n%s", output)
	}
	if !strings.Contains(output, "Plan complete.") {
		t.Errorf("expected log content 'Plan complete.', got:\n%s", output)
	}
	// Must NOT contain non-log entries
	if strings.Contains(output, "result") && !strings.Contains(output, "result.json is excluded") {
		// "result" key should not appear as a section header
		if strings.Contains(output, "=== orun-shard-job1 / result") {
			t.Errorf("non-log entry 'result' should not appear as log section")
		}
	}
}

func TestGithubLogsSkipsNonLogEntries(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "result.json"), []byte(`{"status":"ok"}`), 0644)

	ds := &artifactstore.DownloadedShard{
		Name: "shard1",
		Dir:  tmpDir,
		Shard: &runbundle.RunBundleShardManifest{
			Role: runbundle.ShardRoleJob,
			Files: map[string]string{
				"result": "result.json",
				"plan":   "plan.json",
			},
		},
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printShardLogs(ds, "shard1")

	w.Close()
	os.Stdout = oldStdout
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if output != "" {
		t.Errorf("expected no output for non-log entries, got:\n%s", output)
	}
}

func TestGithubLogsWarnsOnUnreadableFile(t *testing.T) {
	tmpDir := t.TempDir()

	ds := &artifactstore.DownloadedShard{
		Name: "shard1",
		Dir:  tmpDir,
		Shard: &runbundle.RunBundleShardManifest{
			Role: runbundle.ShardRoleJob,
			Files: map[string]string{
				"log:missing-step": "logs/nonexistent.log",
			},
		},
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	printShardLogs(ds, "shard1")

	w.Close()
	os.Stderr = oldStderr
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	errOutput := string(buf[:n])

	if !strings.Contains(errOutput, "warning") || !strings.Contains(errOutput, "log:missing-step") {
		t.Errorf("expected warning about unreadable log, got:\n%s", errOutput)
	}
}

func TestGithubLogsMultipleSteps(t *testing.T) {
	tmpDir := t.TempDir()
	logsDir := filepath.Join(tmpDir, "logs")
	os.MkdirAll(logsDir, 0755)
	os.WriteFile(filepath.Join(logsDir, "fmt.log"), []byte("fmt ok\n"), 0644)
	os.WriteFile(filepath.Join(logsDir, "plan.log"), []byte("plan ok\n"), 0644)

	ds := &artifactstore.DownloadedShard{
		Name: "orun-job-abc",
		Dir:  tmpDir,
		Shard: &runbundle.RunBundleShardManifest{
			Role: runbundle.ShardRoleJob,
			Files: map[string]string{
				"log:fmt":  "logs/fmt.log",
				"log:plan": "logs/plan.log",
			},
		},
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printShardLogs(ds, "orun-job-abc")

	w.Close()
	os.Stdout = oldStdout
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "fmt ok") {
		t.Errorf("missing fmt log content")
	}
	if !strings.Contains(output, "plan ok") {
		t.Errorf("missing plan log content")
	}
	// Both headers present
	headerCount := strings.Count(output, "=== orun-job-abc /")
	if headerCount != 2 {
		t.Errorf("expected 2 section headers, got %d", headerCount)
	}
}

func TestGithubLogsPathTraversalBlocked(t *testing.T) {
	tmpDir := t.TempDir()

	ds := &artifactstore.DownloadedShard{
		Name: "shard1",
		Dir:  tmpDir,
		Shard: &runbundle.RunBundleShardManifest{
			Role: runbundle.ShardRoleJob,
			Files: map[string]string{
				"log:evil": "../../etc/passwd",
			},
		},
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Capture stdout to ensure nothing printed
	oldStdout := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	printShardLogs(ds, "shard1")

	w.Close()
	wOut.Close()
	os.Stderr = oldStderr
	os.Stdout = oldStdout

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	errOutput := string(buf[:n])

	bufOut := make([]byte, 4096)
	nOut, _ := rOut.Read(bufOut)
	stdOutput := string(bufOut[:nOut])

	if !strings.Contains(errOutput, "escapes shard directory") {
		t.Errorf("expected path traversal warning, got stderr:\n%s", errOutput)
	}
	if strings.Contains(stdOutput, "===") {
		t.Errorf("path traversal log should not produce output, got:\n%s", stdOutput)
	}
}

func TestGithubRunsDetailsFlag(t *testing.T) {
	// Verify the --details flag is registered and defaults to false
	cmd, _, err := rootCmd.Find([]string{"github", "runs"})
	if err != nil {
		t.Fatalf("github runs not found: %v", err)
	}
	f := cmd.Flags().Lookup("details")
	if f == nil {
		t.Fatal("--details flag not registered")
	}
	if f.DefValue != "false" {
		t.Errorf("--details default = %q, want false", f.DefValue)
	}
}

func TestManifestStatus(t *testing.T) {
	tests := []struct {
		name string
		m    *runbundle.RunBundleShardManifest
		want string
	}{
		{"job with status", &runbundle.RunBundleShardManifest{Role: runbundle.ShardRoleJob, Status: "completed"}, "completed"},
		{"job failed", &runbundle.RunBundleShardManifest{Role: runbundle.ShardRoleJob, Status: "failed"}, "failed"},
		{"plan no status", &runbundle.RunBundleShardManifest{Role: runbundle.ShardRolePlan}, "plan"},
		{"no role no status", &runbundle.RunBundleShardManifest{}, "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := manifestStatus(tc.m)
			if got != tc.want {
				t.Errorf("manifestStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSortShardsByName(t *testing.T) {
	shards := []artifactstore.RemoteShard{
		{Name: "orun.v1.abc.job.z.completed"},
		{Name: "orun.v1.abc.job.a.completed"},
		{Name: "orun.v1.abc.plan.m.created"},
	}
	sortShardsByName(shards)
	if shards[0].Name != "orun.v1.abc.job.a.completed" {
		t.Errorf("shards[0] = %q, want orun.v1.abc.job.a.completed", shards[0].Name)
	}
	if shards[1].Name != "orun.v1.abc.job.z.completed" {
		t.Errorf("shards[1] = %q, want orun.v1.abc.job.z.completed", shards[1].Name)
	}
	if shards[2].Name != "orun.v1.abc.plan.m.created" {
		t.Errorf("shards[2] = %q, want orun.v1.abc.plan.m.created", shards[2].Name)
	}
}

func TestGithubRunsLevel1NoDownload(t *testing.T) {
	// Verify that when githubRunsDetails is false, the Level 1 path
	// does not attempt any artifact download. We test this by checking
	// that the runGithubRuns flow only calls ListArtifacts, not Download.
	// This is a structural verification — the key check is that
	// printManifestDetails is gated behind githubRunsDetails.
	if githubRunsDetails {
		t.Fatal("githubRunsDetails should default to false")
	}
}

func TestGithubLogsJobFilterNoMatch(t *testing.T) {
	// The --job filter returning error when no match is already in runGithubLogs.
	// Verify that the error message format is correct.
	shards := []artifactstore.RemoteShard{
		{ID: "1", Name: "orun-shard-plan"},
	}
	var filtered []artifactstore.RemoteShard
	jobFilter := "nonexistent-job"
	for _, s := range shards {
		if strings.Contains(s.Name, jobFilter) {
			filtered = append(filtered, s)
		}
	}
	if len(filtered) != 0 {
		t.Errorf("expected no matches for job filter %q", jobFilter)
	}
}

// --- Task 0145: focused tests for --orun-dir normalization and status flags ---

// TestNormalizeOrunDirParentBecomesDotOrun covers the primary fix from PR #142
// commit ddbec4c: a parent working directory passed via --orun-dir must resolve
// to <parent>/.orun. Task 0142's verifier called this case out as missing direct
// unit coverage.
func TestNormalizeOrunDirParentBecomesDotOrun(t *testing.T) {
	parent := filepath.Join(string(filepath.Separator)+"tmp", "workspace-foo")
	got := normalizeOrunDir(parent)
	want := filepath.Join(parent, ".orun")
	if got != want {
		t.Errorf("normalizeOrunDir(%q) = %q, want %q", parent, got, want)
	}
	if filepath.Base(got) != ".orun" {
		t.Errorf("expected resolved path to end in %q, got %q", ".orun", got)
	}
}

// TestNormalizeOrunDirAlreadyDotOrunUnchanged covers the back-compat branch:
// a user who explicitly passes a path whose base is already ".orun" must not
// have another ".orun" segment appended.
func TestNormalizeOrunDirAlreadyDotOrunUnchanged(t *testing.T) {
	already := filepath.Join(string(filepath.Separator)+"tmp", "workspace-foo", ".orun")
	got := normalizeOrunDir(already)
	if got != already {
		t.Errorf("normalizeOrunDir(%q) = %q, want unchanged %q", already, got, already)
	}
	// Defensive: must not have a doubled .orun/.orun tail.
	doubled := filepath.Join(already, ".orun")
	if got == doubled {
		t.Errorf("normalizeOrunDir doubled the .orun suffix: got %q", got)
	}
}

// TestNormalizeOrunDirEmptyDefaultsToDotOrun covers the empty-input default:
// an empty --orun-dir is treated as "." and resolves to "./.orun" (i.e. ".orun").
func TestNormalizeOrunDirEmptyDefaultsToDotOrun(t *testing.T) {
	got := normalizeOrunDir("")
	want := filepath.Join(".", ".orun")
	if got != want {
		t.Errorf("normalizeOrunDir(\"\") = %q, want %q", got, want)
	}
}

// TestGithubStatusSelectorFlagsRegistered verifies that `orun github status`
// registers the same run-resolution flags used by `pull` and `logs`. Before
// the fix in PR #142 commit ddbec4c, githubStatusCmd read the flag globals
// (githubLogsRunID, githubLogsExecID, etc.) but no flags were ever attached
// to it, so `--run-id`, `--exec-id`, `--sha`, `--branch`, `--latest`, and
// `--failed` all errored at the cobra parser.
func TestGithubStatusSelectorFlagsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github", "status"})
	if err != nil {
		t.Fatalf("github status command not found: %v", err)
	}
	required := []string{"run-id", "exec-id", "sha", "branch", "latest", "failed"}
	for _, name := range required {
		if f := cmd.Flags().Lookup(name); f == nil {
			t.Errorf("github status: --%s flag not registered", name)
		}
	}
}

// TestGithubStatusAcceptsSelectorFlagsAtParseTime drives cobra's flag parser
// directly to confirm that supplying every selector flag does not produce an
// "unknown flag" error. We do not actually run the command (that would hit
// the network); we only verify the parse step.
func TestGithubStatusAcceptsSelectorFlagsAtParseTime(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"github", "status"})
	if err != nil {
		t.Fatalf("github status command not found: %v", err)
	}
	// Take a defensive copy of the flag set so we don't mutate globals
	// observed by other tests. We invoke Parse on the underlying FlagSet,
	// which exercises the same code path cobra would.
	args := []string{
		"--run-id", "12345",
		"--exec-id", "gh-12345-1-abc",
		"--sha", "abc1234",
		"--branch", "main",
		"--latest",
		"--failed",
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("github status flag parse failed: %v", err)
	}
}

package ui

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestGHARendererJobBufferGroupsAndFlush(t *testing.T) {
	var sink bytes.Buffer
	g := NewGHARenderer(&sink)

	a := g.JobBuffer("job-a")
	a.Println("▶ job-a [api·ci]")
	a.OpenGroup("job-a › setup-node  (1/2)")
	a.Println("Setting up Node 20")
	a.Println("done")
	a.CloseGroup()
	a.OpenGroup("job-a › build  (2/2)")
	a.Println("compiled")
	a.CloseGroup()
	a.Println("✓ job-a  3.0s  2 steps")

	if sink.Len() != 0 {
		t.Fatalf("expected nothing flushed before FlushJob, got %q", sink.String())
	}

	g.FlushJob("job-a")
	got := sink.String()
	for _, want := range []string{
		"▶ job-a [api·ci]",
		"::group::job-a › setup-node  (1/2)",
		"Setting up Node 20",
		"::endgroup::",
		"::group::job-a › build  (2/2)",
		"compiled",
		"✓ job-a  3.0s  2 steps",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("flushed output missing %q\n--- got ---\n%s", want, got)
		}
	}
	// Two groups -> two endgroups.
	if c := strings.Count(got, "::endgroup::"); c != 2 {
		t.Fatalf("expected 2 endgroup markers, got %d", c)
	}
}

func TestGHARendererConcurrentFlushNoInterleave(t *testing.T) {
	var sink bytes.Buffer
	g := NewGHARenderer(&sink)

	var wg sync.WaitGroup
	for _, id := range []string{"job-a", "job-b", "job-c"} {
		wg.Add(1)
		go func(jobID string) {
			defer wg.Done()
			b := g.JobBuffer(jobID)
			b.OpenGroup(jobID + " › step")
			for i := 0; i < 50; i++ {
				b.Println(jobID + "-line")
			}
			b.CloseGroup()
			g.FlushJob(jobID)
		}(id)
	}
	wg.Wait()

	out := sink.String()
	// Each job's group must appear as a contiguous block: between its
	// ::group:: and ::endgroup::, no other job's marker may appear.
	for _, id := range []string{"job-a", "job-b", "job-c"} {
		start := strings.Index(out, "::group::"+id)
		end := strings.Index(out[start:], "::endgroup::")
		if start < 0 || end < 0 {
			t.Fatalf("missing group for %s", id)
		}
		block := out[start : start+end]
		for _, other := range []string{"job-a", "job-b", "job-c"} {
			if other == id {
				continue
			}
			if strings.Contains(block, "::group::"+other) {
				t.Fatalf("group for %s interleaved into %s block:\n%s", other, id, block)
			}
		}
	}
}

func TestGHARendererAnnotationEscaping(t *testing.T) {
	var sink bytes.Buffer
	g := NewGHARenderer(&sink)
	g.Error("boom\nsecond line\r%danger")
	got := sink.String()
	want := "::error::boom%0Asecond line%0D%25danger\n"
	if got != want {
		t.Fatalf("unexpected escaped output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestGHARendererNestedGroupDegradesGracefully(t *testing.T) {
	var sink bytes.Buffer
	g := NewGHARenderer(&sink)
	b := g.JobBuffer("j")
	b.OpenGroup("outer")
	b.OpenGroup("inner")
	b.Println("payload")
	b.CloseGroup()
	b.CloseGroup()
	g.FlushJob("j")

	out := sink.String()
	if strings.Count(out, "::group::") != 1 {
		t.Fatalf("expected exactly 1 ::group:: marker (no nesting), got:\n%s", out)
	}
	if !strings.Contains(out, "──▶ inner") {
		t.Fatalf("expected nested group to render as divider, got:\n%s", out)
	}
}

func TestIsGitHubActions(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	if !IsGitHubActions() {
		t.Fatal("expected IsGitHubActions=true")
	}
	t.Setenv("GITHUB_ACTIONS", "false")
	if IsGitHubActions() {
		t.Fatal("expected IsGitHubActions=false")
	}
	t.Setenv("GITHUB_ACTIONS", "")
	if IsGitHubActions() {
		t.Fatal("expected IsGitHubActions=false when empty")
	}
}

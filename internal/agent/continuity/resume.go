package continuity

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Resumed is what a successor got back from its predecessor.
type Resumed struct {
	// From is the predecessor's session id.
	From string
	// HarnessSession is the conversation to launch with --resume; empty when
	// there was no transcript to restore.
	HarnessSession string
	// Lines is how many transcript lines were restored.
	Lines int
	// SnapshotFiles is how many files the snapshot put back.
	SnapshotFiles int
	// SnapshotSkipped says why a snapshot was not applied, when it was not.
	SnapshotSkipped string
}

type resumeAnswer struct {
	Resume *struct {
		From     string `json:"from"`
		Meta     Meta   `json:"meta"`
		Snapshot *struct {
			ID      string  `json:"id"`
			Parts   int     `json:"parts"`
			Bytes   int     `json:"bytes"`
			HeadSha *string `json:"headSha"`
			Branch  *string `json:"branch"`
		} `json:"snapshot"`
	} `json:"resume"`
}

type transcriptPage struct {
	HarnessSession *string `json:"harnessSession"`
	Lines          []struct {
		Seq  int    `json:"seq"`
		Text string `json:"text"`
	} `json:"lines"`
	Next *int `json:"next"`
}

// Resume reads the predecessor's state, if this session has one, and puts it
// back: the transcript where Claude Code looks for it under this box's
// working directory, and the snapshot of unpushed work when it was taken on
// the commit now checked out. A nil result with a nil error means there was
// nothing to resume.
func Resume(ctx context.Context, cfg Config) (*Resumed, error) {
	var ans resumeAnswer
	if err := cfg.request(ctx, http.MethodGet, "/state/resume", nil, &ans); err != nil {
		return nil, err
	}
	if ans.Resume == nil {
		return nil, nil
	}
	r := &Resumed{From: ans.Resume.From}

	if hs := ans.Resume.Meta.HarnessSession; hs != "" && harnessIDRe.MatchString(hs) {
		var lines []string
		after := -1
		for i := 0; i < 100_000; i++ {
			var page transcriptPage
			if err := cfg.request(ctx, http.MethodGet, fmt.Sprintf("/state/resume/transcript?after=%d", after), nil, &page); err != nil {
				return r, fmt.Errorf("reading the transcript: %w", err)
			}
			for _, l := range page.Lines {
				lines = append(lines, l.Text)
			}
			if page.Next == nil || *page.Next <= after {
				break
			}
			after = *page.Next
		}
		if len(lines) > 0 {
			body := []byte(strings.Join(lines, "\n") + "\n")
			dirs := []string{ProjectDir(cfg.Workdir)}
			if rec := ans.Resume.Meta.ProjectDir; rec != "" && projectDirRe.MatchString(rec) && rec != dirs[0] {
				dirs = append(dirs, rec)
			}
			for _, d := range dirs {
				path := filepath.Join(cfg.ClaudeDir, "projects", d, hs+".jsonl")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					return r, err
				}
				if err := os.WriteFile(path, body, 0o600); err != nil {
					return r, err
				}
			}
			r.HarnessSession = hs
			r.Lines = len(lines)
		}
	}

	if s := ans.Resume.Snapshot; s != nil && cfg.Workdir != "" {
		head, _ := git(ctx, cfg.Workdir, "rev-parse", "HEAD")
		switch {
		case s.Parts < 1 || s.Parts > snapshotMaxParts:
			r.SnapshotSkipped = "it is malformed"
		case s.HeadSha == nil || strings.TrimSpace(head) != *s.HeadSha:
			r.SnapshotSkipped = "the branch has moved since it was taken"
		default:
			var data []byte
			for p := 0; p < s.Parts; p++ {
				part, _, err := cfg.fetchPart(ctx, p)
				if err != nil {
					return r, fmt.Errorf("reading the snapshot: %w", err)
				}
				data = append(data, part...)
			}
			n, err := applySnapshot(cfg.Workdir, data)
			if err != nil {
				return r, fmt.Errorf("applying the snapshot: %w", err)
			}
			r.SnapshotFiles = n
		}
	}
	return r, nil
}

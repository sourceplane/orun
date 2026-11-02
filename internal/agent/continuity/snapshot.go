package continuity

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A snapshot is the work the session has not pushed: modified and untracked
// files (never ignored ones — that is where .env files and node_modules
// live), as a gzip tar, with the HEAD it applies to. It is uploaded in parts
// of at most 1 MB and refused over 16 MB. Deleted files ride as one entry
// listing their paths.
const (
	snapshotPartBytes = 1 << 20
	snapshotMaxParts  = 16
	snapshotMaxBytes  = snapshotPartBytes * snapshotMaxParts
	// deletedEntry is the tar entry that lists tracked files the session
	// deleted (one path per line).
	deletedEntry = ".orun-snapshot/deleted"
)

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

func splitNul(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// skipped reports a path a snapshot never carries.
func skipped(p string) bool {
	return p == "node_modules" || strings.HasPrefix(p, "node_modules/") || strings.Contains(p, "/node_modules/") ||
		strings.HasPrefix(p, ".orun/") || strings.HasPrefix(p, ".git/")
}

// built is one snapshot ready to upload.
type built struct {
	data    []byte
	headSha string
	branch  string
	digest  string // of the working-copy state, to skip an unchanged one
}

// buildSnapshot packs the unpushed work in dir, or returns nil when the
// working copy is clean.
func buildSnapshot(ctx context.Context, dir string) (*built, error) {
	status, err := git(ctx, dir, "status", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(strings.ReplaceAll(status, "\x00", "")) == "" {
		return nil, nil
	}
	changed, err := git(ctx, dir, "ls-files", "-m", "-o", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	deleted, err := git(ctx, dir, "ls-files", "-d", "-z")
	if err != nil {
		return nil, err
	}
	head, _ := git(ctx, dir, "rev-parse", "HEAD")
	branch, _ := git(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")

	gone := map[string]bool{}
	for _, p := range splitNul(deleted) {
		gone[p] = true
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	sum := sha256.New()
	io.WriteString(sum, status)
	seen := map[string]bool{}
	for _, p := range splitNul(changed) {
		if seen[p] || gone[p] || skipped(p) {
			continue
		}
		seen[p] = true
		full := filepath.Join(dir, p)
		info, err := os.Lstat(full)
		if err != nil || !info.Mode().IsRegular() {
			continue // a symlink, a directory, a file gone since — not carried
		}
		content, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		sum.Write([]byte(p))
		sum.Write(content)
		if err := tw.WriteHeader(&tar.Header{Name: p, Mode: int64(info.Mode().Perm()), Size: int64(len(content)), ModTime: info.ModTime(), Typeflag: tar.TypeReg}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(content); err != nil {
			return nil, err
		}
		if buf.Len() > snapshotMaxBytes {
			return nil, fmt.Errorf("unpushed work is over %d MB; not snapshotted", snapshotMaxBytes>>20)
		}
	}
	if len(gone) > 0 {
		var list []string
		for p := range gone {
			list = append(list, p)
		}
		body := []byte(strings.Join(list, "\n") + "\n")
		sum.Write(body)
		if err := tw.WriteHeader(&tar.Header{Name: deletedEntry, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(body); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	if buf.Len() > snapshotMaxBytes {
		return nil, fmt.Errorf("unpushed work is over %d MB; not snapshotted", snapshotMaxBytes>>20)
	}
	io.WriteString(sum, head)
	return &built{data: buf.Bytes(), headSha: strings.TrimSpace(head), branch: strings.TrimSpace(branch), digest: hex.EncodeToString(sum.Sum(nil))}, nil
}

// uploadSnapshot sends a built snapshot in parts.
func uploadSnapshot(ctx context.Context, cfg Config, b *built) error {
	parts := (len(b.data) + snapshotPartBytes - 1) / snapshotPartBytes
	id := strconv.FormatInt(time.Now().UnixMilli(), 10)
	for i := 0; i < parts; i++ {
		end := (i + 1) * snapshotPartBytes
		if end > len(b.data) {
			end = len(b.data)
		}
		q := url.Values{}
		q.Set("id", id)
		q.Set("part", strconv.Itoa(i))
		q.Set("parts", strconv.Itoa(parts))
		if len(b.headSha) == 40 {
			q.Set("headSha", b.headSha)
		}
		if b.branch != "" && b.branch != "HEAD" {
			q.Set("branch", b.branch)
		}
		if err := cfg.request(ctx, http.MethodPut, "/state/snapshot?"+q.Encode(), b.data[i*snapshotPartBytes:end], nil); err != nil {
			return err
		}
	}
	return nil
}

// applySnapshot unpacks a snapshot into dir: its files written, its deleted
// paths removed. Only plain relative paths inside dir are touched.
func applySnapshot(dir string, data []byte) (int, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	tr := tar.NewReader(gz)
	root, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}
	inside := func(p string) (string, bool) {
		if p == "" || filepath.IsAbs(p) || strings.Contains(p, "\\") {
			return "", false
		}
		clean := filepath.Clean(p)
		if clean == "." || strings.HasPrefix(clean, "..") || skipped(clean) {
			return "", false
		}
		full := filepath.Join(root, clean)
		if !strings.HasPrefix(full, root+string(filepath.Separator)) {
			return "", false
		}
		return full, true
	}
	n := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		content, err := io.ReadAll(io.LimitReader(tr, snapshotMaxBytes))
		if err != nil {
			return n, err
		}
		if h.Name == deletedEntry {
			for _, p := range strings.Split(string(content), "\n") {
				if full, ok := inside(strings.TrimSpace(p)); ok {
					_ = os.Remove(full)
				}
			}
			continue
		}
		full, ok := inside(h.Name)
		if !ok {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return n, err
		}
		mode := os.FileMode(h.Mode).Perm()
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(full, content, mode); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/workflowbackend"
)

func newCapCmd() (*cobra.Command, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	c := &cobra.Command{}
	c.SetOut(buf)
	c.SetErr(buf)
	return c, buf
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWorkflowValidate(t *testing.T) {
	dir := t.TempDir()
	good := writeFile(t, dir, "wf.yaml", "apiVersion: torkflow/v1\nkind: Workflow\n")
	bad := writeFile(t, dir, "no.yaml", "apiVersion: sourceplane.io/v1\nkind: Component\n")

	c, buf := newCapCmd()
	if err := runWorkflowValidate(c, good); err != nil {
		t.Fatalf("validate good: %v", err)
	}
	if !strings.Contains(buf.String(), "ok:") {
		t.Fatalf("expected ok output, got %q", buf.String())
	}
	if err := runWorkflowValidate(c, bad); err == nil {
		t.Fatalf("expected validation error for a non-workflow file")
	}
}

func TestWorkflowDigestCmd(t *testing.T) {
	dir := t.TempDir()
	body := "apiVersion: torkflow/v1\n"
	p := writeFile(t, dir, "wf.yaml", body)

	c, buf := newCapCmd()
	workflowDigestCmd.SetOut(buf)
	if err := workflowDigestCmd.RunE(c, []string{p}); err != nil {
		t.Fatalf("digest: %v", err)
	}
	if strings.TrimSpace(buf.String()) != workflowbackend.DigestBytes([]byte(body)) {
		t.Fatalf("digest output mismatch: %q", buf.String())
	}
}

func writeFakeEngine(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake engine uses /bin/sh")
	}
	p := filepath.Join(t.TempDir(), "engine.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWorkflowRunWithFakeEngine(t *testing.T) {
	dir := t.TempDir()
	wf := writeFile(t, dir, "wf.yaml", "apiVersion: torkflow/v1\n")

	eng := writeFakeEngine(t, `echo '{"status":"success","steps":[{"name":"a","status":"success"}]}'`)
	t.Setenv(workflowbackend.EngineEnv, eng)
	workflowRunSet = nil

	c, buf := newCapCmd()
	if err := runWorkflowRun(context.Background(), c, wf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "success") {
		t.Fatalf("expected success summary, got %q", buf.String())
	}
}

func TestWorkflowRunFailureIsError(t *testing.T) {
	dir := t.TempDir()
	wf := writeFile(t, dir, "wf.yaml", "apiVersion: torkflow/v1\n")

	eng := writeFakeEngine(t, `echo '{"status":"failed","error":"boom"}'`)
	t.Setenv(workflowbackend.EngineEnv, eng)
	workflowRunSet = nil

	c, _ := newCapCmd()
	if err := runWorkflowRun(context.Background(), c, wf); err == nil {
		t.Fatalf("expected error for a failed workflow run")
	}
}

func TestWorkflowRunUnconfiguredEngine(t *testing.T) {
	dir := t.TempDir()
	wf := writeFile(t, dir, "wf.yaml", "apiVersion: torkflow/v1\n")
	t.Setenv(workflowbackend.EngineEnv, "")

	c, _ := newCapCmd()
	if err := runWorkflowRun(context.Background(), c, wf); err == nil {
		t.Fatalf("expected error when no engine is configured")
	}
}

func TestParseSetFlags(t *testing.T) {
	got, err := parseSetFlags([]string{"a=1", "b=two"})
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "1" || got["b"] != "two" {
		t.Fatalf("unexpected: %#v", got)
	}
	if _, err := parseSetFlags([]string{"bad"}); err == nil {
		t.Fatalf("expected error for a flag without =")
	}
}

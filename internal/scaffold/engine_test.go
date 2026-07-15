package scaffold

import (
	"strings"
	"testing"
)

func TestRenderBasic(t *testing.T) {
	out, err := Render("t", "hello {{ .name }}", map[string]any{"name": "world"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if string(out) != "hello world" {
		t.Fatalf("got %q", out)
	}
}

func TestRenderDeterministic(t *testing.T) {
	body := "{{ .a }}-{{ upper .b }}-{{ kebab .c }}"
	vals := map[string]any{"a": "x", "b": "yZ", "c": "Some Name!!"}
	first, err := Render("t", body, vals)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for i := 0; i < 50; i++ {
		again, err := Render("t", body, vals)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("non-deterministic: %q != %q", again, first)
		}
	}
	if string(first) != "x-YZ-some-name" {
		t.Fatalf("got %q", first)
	}
}

func TestRenderMissingKeyFailsClosed(t *testing.T) {
	_, err := Render("t", "hi {{ .nope }}", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
}

func TestFuncMapIsClosedAllowList(t *testing.T) {
	fm := constrainedFuncMap()
	allowed := map[string]bool{
		"lower": true, "upper": true, "title": true, "trim": true,
		"kebab": true, "slug": true, "quote": true, "default": true, "indent": true,
	}
	for name := range fm {
		if !allowed[name] {
			t.Errorf("funcmap exposes unexpected helper %q — the allow-list is closed (design §7.2)", name)
		}
	}
	// And no dangerous helper leaked in.
	for _, banned := range []string{"env", "os", "exec", "readFile", "now", "uuid", "rand"} {
		if _, ok := fm[banned]; ok {
			t.Errorf("funcmap exposes banned helper %q", banned)
		}
	}
}

func TestFuncMapHelpers(t *testing.T) {
	cases := []struct{ body, want string }{
		{`{{ lower "AbC" }}`, "abc"},
		{`{{ upper "AbC" }}`, "ABC"},
		{`{{ kebab "My Cool Service!" }}`, "my-cool-service"},
		{`{{ slug "  Leading  " }}`, "leading"},
		{`{{ trim "  x  " }}`, "x"},
		{`{{ quote "hi" }}`, `"hi"`},
		{`{{ default "fallback" .empty }}`, "fallback"},
		{`{{ default "fallback" .present }}`, "here"},
	}
	for _, c := range cases {
		out, err := Render("t", c.body, map[string]any{"present": "here", "empty": ""})
		if err != nil {
			t.Fatalf("render %q: %v", c.body, err)
		}
		if string(out) != c.want {
			t.Errorf("render %q = %q, want %q", c.body, out, c.want)
		}
	}
}

func TestReferencesInputs(t *testing.T) {
	if !referencesInputs("name: {{ .serviceName }}") {
		t.Error("expected referencesInputs true for .serviceName")
	}
	if referencesInputs("static content, {{ upper \"x\" }} no dot pipeline") {
		t.Error("expected referencesInputs false for literal-only template")
	}
	if !referencesInputs("{{ range .Items }}x{{ end }}") {
		t.Error("expected referencesInputs true for range over .Items")
	}
}

func TestIndent(t *testing.T) {
	got := indent(2, "a\nb\n\nc")
	want := "  a\n  b\n\n  c"
	if got != want {
		t.Fatalf("indent = %q, want %q", got, want)
	}
	// Sanity: no ecosystem literals accidentally used in test strings above.
	if strings.Contains(want, "pnpm") {
		t.Fatal("unexpected")
	}
}

package workflowbackend

import (
	"strings"
	"testing"
)

func TestFindOutputRefs(t *testing.T) {
	refs := FindOutputRefs(`echo ${{ steps.get-oncall.outputs.email }} and ${{steps.calc.outputs.sum}}`)
	if len(refs) != 2 || refs[0].StepID != "get-oncall" || refs[0].Name != "email" || refs[1].StepID != "calc" || refs[1].Name != "sum" {
		t.Fatalf("unexpected refs: %#v", refs)
	}
	if FindOutputRefs("plain {{ .compileTime }} text") != nil {
		t.Fatalf("compile-time templates must not match")
	}
}

func TestSubstituteOutputRefs(t *testing.T) {
	lookup := func(id, name string) (string, bool) {
		if id == "get" && name == "email" {
			return "sam@acme.dev", true
		}
		return "", false
	}
	out, err := SubstituteOutputRefs("notify ${{ steps.get.outputs.email }}", lookup)
	if err != nil || out != "notify sam@acme.dev" {
		t.Fatalf("substitution failed: %q %v", out, err)
	}
	if _, err := SubstituteOutputRefs("${{ steps.get.outputs.absent }}", lookup); err == nil {
		t.Fatalf("dangling reference must error")
	}
}

func TestMaskUnmaskRoundTrip(t *testing.T) {
	in := `run --to ${{ steps.a.outputs.x }} --and {{ .compile }} --plus ${{ steps.b.outputs.y }}`
	masked, spans := MaskOutputRefs(in)
	if strings.Contains(masked, "steps.a.outputs") || len(spans) != 2 {
		t.Fatalf("masking incomplete: %q spans=%d", masked, len(spans))
	}
	if got := UnmaskOutputRefs(masked, spans); got != in {
		t.Fatalf("round trip diverged: %q", got)
	}
}

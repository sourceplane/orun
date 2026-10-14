package trigger

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestValidateLanes_ConcurrencyAutoIsValid(t *testing.T) {
	intent := &model.Intent{}
	intent.Execution.Lanes = &model.IntentLanes{Mode: "auto", Concurrency: model.LaneConcurrencyAuto,
		Groups: map[string]model.LaneGroup{"fleet": {Concurrency: model.LaneConcurrencyAuto}}}
	if errs := ValidateLanes(intent); len(errs) != 0 {
		t.Fatalf("auto must validate: %v", errs)
	}
	intent.Execution.Lanes = &model.IntentLanes{Mode: "auto", Concurrency: -2}
	errs := ValidateLanes(intent)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "at least 1 or auto") {
		t.Fatalf("a negative count must be refused: %v", errs)
	}
}

func TestValidateLanes_DurationsMustParse(t *testing.T) {
	intent := &model.Intent{}
	intent.Execution.Defaults = &model.IntentExecutionDefaults{Timeout: "45m"}
	intent.Execution.Lanes = &model.IntentLanes{Mode: "auto", UpstreamWait: "90m"}
	if errs := ValidateLanes(intent); len(errs) != 0 {
		t.Fatalf("valid durations: %v", errs)
	}
	intent.Execution.Defaults = &model.IntentExecutionDefaults{Timeout: "soon"}
	intent.Execution.Lanes = &model.IntentLanes{Mode: "auto", UpstreamWait: "-1m"}
	errs := ValidateLanes(intent)
	if len(errs) != 2 || !strings.Contains(errs[0].Error(), "defaults.timeout") || !strings.Contains(errs[1].Error(), "upstreamWait") {
		t.Fatalf("both refused: %v", errs)
	}
}

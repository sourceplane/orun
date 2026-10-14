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

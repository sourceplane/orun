package triggerctx

import (
	"strings"
	"testing"
	"time"
)

func TestNewSystemManual_Defaults(t *testing.T) {
	t.Parallel()
	occ := NewSystemManual(SystemOptions{
		Source: TriggerSource{HeadRevision: "def456a1b2c3"},
		Now:    time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC),
	})
	assertCommonSystem(t, occ, SystemManual, ModeManual)
	if occ.PlanScope.Mode != PlanScopeFull {
		t.Errorf("PlanScope.Mode = %q want %q", occ.PlanScope.Mode, PlanScopeFull)
	}
	if occ.Source.SourceScope != "manual" {
		t.Errorf("Source.SourceScope = %q want manual", occ.Source.SourceScope)
	}
	if occ.Source.WorkingTree != WorkingTreeClean {
		t.Errorf("Source.WorkingTree = %q want clean", occ.Source.WorkingTree)
	}
	if occ.TriggerKey != "trg-manual-def456a" {
		t.Errorf("TriggerKey = %q", occ.TriggerKey)
	}
	if !occ.CreatedAt.Equal(time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("CreatedAt not pinned: %v", occ.CreatedAt)
	}
}

func TestNewSystemManual_DirtyOverridesScope(t *testing.T) {
	t.Parallel()
	occ := NewSystemManual(SystemOptions{Source: TriggerSource{WorkingTree: WorkingTreeDirty}})
	if occ.Source.SourceScope != "local-dirty" {
		t.Errorf("dirty default scope = %q want local-dirty", occ.Source.SourceScope)
	}
	if !strings.HasSuffix(occ.TriggerKey, "-local-dirty") {
		t.Errorf("TriggerKey suffix wrong: %q", occ.TriggerKey)
	}
}

func TestNewSystemManualChanged_DefaultsToChangedScope(t *testing.T) {
	t.Parallel()
	occ := NewSystemManualChanged(SystemOptions{Source: TriggerSource{HeadRevision: "def456a1b2c3"}})
	assertCommonSystem(t, occ, SystemManualChanged, ModeChanged)
	if occ.PlanScope.Mode != PlanScopeChanged {
		t.Errorf("PlanScope.Mode = %q want changed", occ.PlanScope.Mode)
	}
	if occ.Source.SourceScope != "manual-changed" {
		t.Errorf("SourceScope = %q want manual-changed", occ.Source.SourceScope)
	}
}

func TestNewSystemReplay(t *testing.T) {
	t.Parallel()
	occ := NewSystemReplay(SystemOptions{Source: TriggerSource{HeadRevision: "abc1234567890"}})
	assertCommonSystem(t, occ, SystemReplay, ModeReplay)
}

func TestNewSystemAPI(t *testing.T) {
	t.Parallel()
	occ := NewSystemAPI(SystemOptions{Source: TriggerSource{HeadRevision: "abc1234567890"}})
	assertCommonSystem(t, occ, SystemAPI, ModeAPI)
}

func TestNewSystemMigrated(t *testing.T) {
	t.Parallel()
	occ := NewSystemMigrated(SystemOptions{Source: TriggerSource{HeadRevision: "abc1234567890"}})
	assertCommonSystem(t, occ, SystemMigrated, ModeMigration)
}

func TestSystem_PlanScopeFieldsPropagate(t *testing.T) {
	t.Parallel()
	envs := []string{"dev", "stage"}
	changed := []string{"api", "console"}
	occ := NewSystemManual(SystemOptions{
		Source: TriggerSource{HeadRevision: "abcdef0123456789"},
		PlanScope: PlanScope{
			Mode:               PlanScopeChanged,
			Base:               "111",
			Head:               "222",
			ActivationMode:     "all-environments",
			ActiveEnvironments: envs,
			ChangedComponents:  changed,
		},
	})
	if occ.PlanScope.Mode != PlanScopeChanged || occ.PlanScope.Base != "111" || occ.PlanScope.Head != "222" {
		t.Errorf("PlanScope not propagated: %+v", occ.PlanScope)
	}
	// Mutating the caller's slice MUST NOT affect the returned occurrence
	// (Clone defends against this).
	envs[0] = "MUTATED"
	if occ.PlanScope.ActiveEnvironments[0] == "MUTATED" {
		t.Errorf("ActiveEnvironments shared backing array with caller slice")
	}
}

func assertCommonSystem(t *testing.T, occ TriggerOccurrence, name, mode string) {
	t.Helper()
	if occ.APIVersion != APIVersion || occ.Kind != KindName {
		t.Errorf("apiVersion/kind: %+v", occ)
	}
	if occ.TriggerType != TriggerTypeSystem {
		t.Errorf("TriggerType = %q want system", occ.TriggerType)
	}
	if occ.TriggerName != name {
		t.Errorf("TriggerName = %q want %q", occ.TriggerName, name)
	}
	if occ.Mode != mode {
		t.Errorf("Mode = %q want %q", occ.Mode, mode)
	}
	if occ.Provider != ProviderOrun || occ.Event != EventManual {
		t.Errorf("provider/event: %+v", occ)
	}
	if len(occ.MatchedBindings) != 1 || occ.MatchedBindings[0] != name {
		t.Errorf("MatchedBindings = %v want [%s]", occ.MatchedBindings, name)
	}
	if !strings.HasPrefix(occ.TriggerID, "trg_") {
		t.Errorf("TriggerID missing prefix: %q", occ.TriggerID)
	}
	if occ.TriggerKey == "" {
		t.Errorf("TriggerKey empty")
	}
}

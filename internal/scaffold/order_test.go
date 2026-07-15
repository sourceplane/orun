package scaffold

import (
	"reflect"
	"testing"
)

func bp(modules ...Module) *Blueprint {
	return &Blueprint{
		APIVersion: BlueprintAPIVersion,
		Kind:       BlueprintKind,
		Metadata:   BlueprintMetadata{Name: "t"},
		Modules:    modules,
	}
}

func TestOrderSingleModuleTrivial(t *testing.T) {
	batches, err := orderModules(bp(Module{Name: "only", Mode: ModeTemplate, Files: map[string]string{"a": "b"}}))
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	if !reflect.DeepEqual(batches, [][]string{{"only"}}) {
		t.Fatalf("batches = %v", batches)
	}
}

func TestOrderRespectsDependsOn(t *testing.T) {
	b := bp(
		Module{Name: "app", Mode: ModeConsume, DependsOn: []string{"contracts", "db"}},
		Module{Name: "contracts", Mode: ModeConsume},
		Module{Name: "db", Mode: ModeConsume, DependsOn: []string{"contracts"}},
	)
	batches, err := orderModules(b)
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	pos := map[string]int{}
	for i, batch := range batches {
		for _, n := range batch {
			pos[n] = i
		}
	}
	if !(pos["contracts"] < pos["db"] && pos["db"] < pos["app"]) {
		t.Fatalf("order wrong: %v", batches)
	}
}

func TestOrderDeterministic(t *testing.T) {
	b := bp(
		Module{Name: "z", Mode: ModeConsume, DependsOn: []string{"a"}},
		Module{Name: "a", Mode: ModeConsume},
		Module{Name: "m", Mode: ModeConsume, DependsOn: []string{"a"}},
		Module{Name: "b", Mode: ModeConsume},
	)
	first, err := orderModules(b)
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	for i := 0; i < 30; i++ {
		again, err := orderModules(b)
		if err != nil {
			t.Fatalf("order: %v", err)
		}
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("non-deterministic order: %v != %v", first, again)
		}
	}
}

func TestOrderUndeclaredCycleErrors(t *testing.T) {
	b := bp(
		Module{Name: "x", Mode: ModeConsume, DependsOn: []string{"y"}},
		Module{Name: "y", Mode: ModeConsume, DependsOn: []string{"x"}},
	)
	_, err := orderModules(b)
	if err == nil {
		t.Fatal("expected undeclared-cycle error")
	}
}

func TestOrderDeclaredCycleBatches(t *testing.T) {
	b := bp(
		Module{Name: "billing", Mode: ModeConsume, DependsOn: []string{"membership"}},
		Module{Name: "membership", Mode: ModeConsume, DependsOn: []string{"billing"}},
		Module{Name: "root", Mode: ModeConsume, DependsOn: []string{"billing", "membership"}},
	)
	b.CycleBreak = []string{"billing->membership"}
	batches, err := orderModules(b)
	if err != nil {
		t.Fatalf("order with declared cycle: %v", err)
	}
	// billing + membership form one atomic batch placed before root.
	foundCluster := false
	for _, batch := range batches {
		if len(batch) == 2 {
			foundCluster = true
			if !reflect.DeepEqual(batch, []string{"billing", "membership"}) {
				t.Fatalf("cluster batch = %v", batch)
			}
		}
	}
	if !foundCluster {
		t.Fatalf("expected an SCC batch of size 2, got %v", batches)
	}
}

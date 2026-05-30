package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// TestComputePlanHashForRevision_StableAcrossChecksumAndRevision verifies the
// canonical plan hash is invariant under (a) re-rendering with a new
// metadata.checksum and (b) re-persisting under a different revision key.
// This is the core data-model.md §3.1 invariant the M5.a writer relies on.
func TestComputePlanHashForRevision_StableAcrossChecksumAndRevision(t *testing.T) {
	base := &model.Plan{
		Metadata: model.PlanMetadata{
			Name:      "p",
			Namespace: "default",
		},
		Jobs: []model.PlanJob{{ID: "j1", Component: "c", Environment: "dev"}},
	}
	hash1, err := computePlanHashForRevision(base)
	if err != nil {
		t.Fatalf("hash1: %v", err)
	}
	if !strings.HasPrefix(hash1, "sha256:") {
		t.Fatalf("hash1 missing sha256: prefix: %q", hash1)
	}

	// (a) mutate checksum — must not change hash.
	base.Metadata.Checksum = "sha256-deadbeef"
	hash2, err := computePlanHashForRevision(base)
	if err != nil {
		t.Fatalf("hash2: %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("checksum mutation changed plan hash:\nbefore=%s\nafter=%s", hash1, hash2)
	}

	// (b) embed metadata.revision — must not change hash.
	base.Metadata.Revision = &model.PlanRevisionMeta{Key: "rev-x", PlanHash: hash1}
	hash3, err := computePlanHashForRevision(base)
	if err != nil {
		t.Fatalf("hash3: %v", err)
	}
	if hash1 != hash3 {
		t.Errorf("revision-meta embedding changed plan hash:\nbefore=%s\nafter=%s", hash1, hash3)
	}

	// Material mutation MUST change the hash.
	base.Jobs = append(base.Jobs, model.PlanJob{ID: "j2"})
	hash4, _ := computePlanHashForRevision(base)
	if hash1 == hash4 {
		t.Errorf("adding a job did not change plan hash: %s", hash1)
	}
}

func TestComputePlanHashForRevision_NilPlan(t *testing.T) {
	if _, err := computePlanHashForRevision(nil); err == nil {
		t.Fatal("expected error for nil plan")
	}
}

// TestCanonicalPlanJSON_RoundTrips verifies the persisted plan.json bytes
// re-marshal cleanly so downstream readers (resolver, manifest writer) get a
// JSON document, not a raw struct dump.
func TestCanonicalPlanJSON_RoundTrips(t *testing.T) {
	in := &model.Plan{
		Metadata: model.PlanMetadata{
			Name: "p",
			Revision: &model.PlanRevisionMeta{
				Key:      "rev-manual-abcdef0-pdeadbee1",
				PlanHash: "sha256:" + strings.Repeat("a", 64),
			},
		},
	}
	b, err := canonicalPlanJSON(in)
	if err != nil {
		t.Fatalf("canonicalPlanJSON: %v", err)
	}
	var out model.Plan
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v\nbytes=%s", err, b)
	}
	if out.Metadata.Revision == nil ||
		out.Metadata.Revision.Key != in.Metadata.Revision.Key ||
		out.Metadata.Revision.PlanHash != in.Metadata.Revision.PlanHash {
		t.Errorf("revision metadata did not survive round-trip: %#v", out.Metadata.Revision)
	}
}

func TestCanonicalPlanJSON_NilPlan(t *testing.T) {
	if _, err := canonicalPlanJSON(nil); err == nil {
		t.Fatal("expected error for nil plan")
	}
}

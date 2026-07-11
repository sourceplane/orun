package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/worklens"
)

// sealedBriefFixture mirrors what the cloud approve mutator seals (WH4):
// canonical bytes whose sha256 is the id. The fixture is built with the Go
// canonicalizer purely for convenience — verification never re-canonicalizes,
// it hashes the bytes as received.
func sealedBriefFixture(t *testing.T) (string, []byte, worklens.EpicSnapshot) {
	t.Helper()
	snap := worklens.EpicSnapshot{
		Kind:       "EpicSnapshot",
		APIVersion: worklens.APIVersion,
		Spec: worklens.Spec{
			APIVersion: worklens.APIVersion, Kind: worklens.KindSpec,
			Key: "demo-epic", Workspace: "org-1", Title: "Demo Epic", DocRef: "sha256:doc",
			Initiative: "ai-native-work",
			CreatedBy:  worklens.Actor{Type: worklens.ActorUser, ID: "usr_1"},
		},
		Milestones: []worklens.Milestone{
			{Key: "M1", Title: "Foundation", Goal: "lay it", DoneWhen: []string{"tests green"}, Ordinal: 0},
			{Key: "M2", Title: "Surface", Ordinal: 1},
		},
		Tasks: []worklens.Task{{
			APIVersion: worklens.APIVersion, Kind: worklens.KindTask,
			Key: "WKS-1", Workspace: "org-1", Spec: "demo-epic", Milestone: "M1", Title: "seed",
			Contract:  &worklens.Contract{Goal: "g", Affects: []string{"a/b/c"}, DoneWhen: []string{"d"}, Gates: []string{"tests"}},
			CreatedBy: worklens.Actor{Type: worklens.ActorAgent, ID: "sp_1"},
		}},
		LadderHash: "sha256:ladder",
		Approval:   worklens.EpicSnapshotApproval{Revision: "sha256:doc", By: worklens.Actor{Type: worklens.ActorUser, ID: "usr_rahul"}, At: "2026-07-11T10:00:00Z"},
		CoordSeq:   42,
		ObsSeq:     7,
	}
	canonical, err := worklens.CanonicalJSON(snap)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), canonical, snap
}

func TestVerifySealedBytes(t *testing.T) {
	id, canonical, _ := sealedBriefFixture(t)
	if err := worklens.VerifySealedBytes(id, canonical); err != nil {
		t.Fatalf("valid brief rejected: %v", err)
	}
	// A flipped byte must fail — content addressing is the trust chain.
	tampered := append([]byte(nil), canonical...)
	tampered[len(tampered)/2] ^= 1
	if err := worklens.VerifySealedBytes(id, tampered); err == nil {
		t.Fatal("tampered brief accepted")
	}
	// A brief smuggling fold output must fail (invariant 1).
	hot := []byte(`{"kind":"EpicSnapshot","rung":"done"}`)
	sum := sha256.Sum256(hot)
	if err := worklens.VerifySealedBytes("sha256:"+hex.EncodeToString(sum[:]), hot); err == nil {
		t.Fatal("hot-state brief accepted")
	}
}

func TestEpicSnapshotParsesCloudShape(t *testing.T) {
	// The cloud seals with the TS canonicalizer; Go only ever PARSES the
	// bytes. This pins the wire shape both sides share.
	cloudBytes := []byte(`{"apiVersion":"orun.io/v1","approval":{"at":"2026-07-11T10:00:01Z","by":{"id":"usr_rahul","type":"user","via":"console"},"revision":"sha256:abc"},"catalog":"sha256:cat","coordSeq":5,"kind":"EpicSnapshot","ladderHash":"sha256:lh","milestones":[{"key":"M1","ordinal":0,"title":"One"}],"obsSeq":2,"spec":{"apiVersion":"orun.io/v1","createdAt":"2026-07-11T10:00:00Z","createdBy":{"id":"usr_rahul","type":"user"},"key":"det-epic","kind":"Spec","title":"Det","workspace":"org-1"},"tasks":[]}`)
	var snap worklens.EpicSnapshot
	if err := json.Unmarshal(cloudBytes, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Kind != "EpicSnapshot" || snap.Spec.Key != "det-epic" || snap.LadderHash != "sha256:lh" {
		t.Fatalf("parsed = %+v", snap)
	}
	if snap.Approval.By.ID != "usr_rahul" || snap.Approval.Revision != "sha256:abc" {
		t.Fatalf("approval = %+v", snap.Approval)
	}
	if len(snap.Milestones) != 1 || snap.Milestones[0].Key != "M1" {
		t.Fatalf("milestones = %+v", snap.Milestones)
	}
}

func TestRenderEpicBriefIsAgentReadable(t *testing.T) {
	id, _, snap := sealedBriefFixture(t)
	brief := renderEpicBrief(&snap, id)
	for _, want := range []string{
		"# Demo Epic — frozen epic brief",
		id,
		"## M1 — Foundation",
		"**Goal:** lay it",
		"### WKS-1 — seed",
		"**Gates:** tests",
		"approved by `usr_rahul`",
		"read-only by\nconstruction",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q", want)
		}
	}
	if strings.Contains(brief, "in_review") || strings.Contains(brief, `"rung"`) {
		t.Error("brief leaked fold output")
	}
}

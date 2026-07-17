package workflowbackend

import (
	"strings"
	"testing"
)

const inspectFixture = `apiVersion: torkflow/v1
kind: Workflow
metadata: { name: n }
spec:
  outputs:
    email: "{{ Steps.Get.user.email }}"
    id: "{{ Steps.Get.user.id }}"
  steps:
    - name: Get
      actionRef: chat.getUser
      connection: chat-main
    - name: Notify
      actionRef: chat.postMessage
      connection: chat-main
    - name: Log
      actionRef: core.stdout
`

func TestInspectWorkflow(t *testing.T) {
	insp, err := InspectWorkflow([]byte(inspectFixture))
	if err != nil {
		t.Fatalf("InspectWorkflow: %v", err)
	}
	if len(insp.Connections) != 1 || insp.Connections[0] != "chat-main" {
		t.Fatalf("connections: %v", insp.Connections)
	}
	if len(insp.Outputs) != 2 || insp.Outputs[0] != "email" || insp.Outputs[1] != "id" {
		t.Fatalf("outputs: %v", insp.Outputs)
	}
}

func TestValidateGrant(t *testing.T) {
	declared := []string{"chat-main", "vcs-app"}
	full := map[string]map[string]string{
		"chat-main": {"token": "secret://a/b/c/CHAT"},
		"vcs-app":   {"token": "secret://a/b/c/VCS"},
	}
	if err := ValidateGrant("step s", declared, full); err != nil {
		t.Fatalf("complete grant should validate: %v", err)
	}
	// Missing: error prints a paste block (S-8).
	err := ValidateGrant("step s", declared, map[string]map[string]string{"chat-main": full["chat-main"]})
	if err == nil || !strings.Contains(err.Error(), "connections:") || !strings.Contains(err.Error(), "vcs-app") {
		t.Fatalf("missing grant should print the block to paste: %v", err)
	}
	// Stale: grant names an undeclared connection.
	if err := ValidateGrant("step s", []string{"chat-main"}, full); err == nil {
		t.Fatalf("stale grant must fail")
	}
	// No declarations, no grant: fine.
	if err := ValidateGrant("step s", nil, nil); err != nil {
		t.Fatalf("empty grant over no declarations: %v", err)
	}
}

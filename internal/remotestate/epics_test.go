package remotestate

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMilestoneCreateRequestPositions(t *testing.T) {
	cases := []struct {
		req  MilestoneCreateRequest
		want string
	}{
		{MilestoneCreateRequest{Name: "n", After: "mls_A"}, `{"after":"mls_A","name":"n"}`},
		{MilestoneCreateRequest{Name: "n", First: true}, `{"after":null,"name":"n"}`},
		{MilestoneCreateRequest{Name: "n", ExitCriteria: []string{"a"}, TargetDate: "2026-01-01"}, `{"exitCriteria":["a"],"name":"n","targetDate":"2026-01-01"}`},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.req)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != c.want {
			t.Errorf("%+v → %s, want %s", c.req, b, c.want)
		}
	}
}

func TestExistingEpicOf(t *testing.T) {
	holder := &APIError{Status: http.StatusConflict, Code: "conflict", Details: json.RawMessage(`{"existing":{"id":"epc_X","slug":"s"}}`)}
	if got := ExistingEpicOf(holder); got == nil || got.ID != "epc_X" {
		t.Fatalf("holder not read: %+v", got)
	}
	for _, bad := range []error{
		&APIError{Status: http.StatusConflict, Code: "conflict"},
		&APIError{Status: http.StatusConflict, Details: json.RawMessage(`{"existing":{}}`)},
		&APIError{Status: http.StatusUnprocessableEntity, Details: json.RawMessage(`{"existing":{"id":"epc_X"}}`)},
		json.Unmarshal([]byte("{"), &struct{}{}),
	} {
		if ExistingEpicOf(bad) != nil {
			t.Errorf("%v must not read as a holder", bad)
		}
	}
}


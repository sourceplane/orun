package model

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// saas-work-gitops-teardown: a `work:` section still carried by a repository
// is read (so the intent loads and plans as before) and ignored — nothing
// resolves a layout from it, and it never reaches the intent's JSON form.
func TestRetiredWorkSectionLoadsAndIsIgnored(t *testing.T) {
	var in Intent
	src := "apiVersion: orun.io/v1\nkind: Intent\nmetadata:\n  name: t\nwork:\n  epics: work/epics\n  tasks: work/tasks\n  sync: on-merge\n"
	if err := yaml.Unmarshal([]byte(src), &in); err != nil {
		t.Fatalf("an intent with a work: section must still load: %v", err)
	}
	if in.Work == nil {
		t.Fatal("the section is read so that `orun plan` can say it is retired")
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"work"`) {
		t.Fatalf("the retired section leaks into the intent's JSON: %s", b)
	}
	var none Intent
	if err := yaml.Unmarshal([]byte("apiVersion: orun.io/v1\nkind: Intent\nmetadata:\n  name: t\n"), &none); err != nil || none.Work != nil {
		t.Fatalf("no section, no notice: %v %v", err, none.Work)
	}
}

package integrationscli

// ICL1/ICL2: descriptor-declared arg parsing — positionals, enum validation,
// repeat, kv — and the bind-map resolution execution relies on.

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/configsurface"
	"github.com/spf13/cobra"
)

// bindVerb builds the leaf for v, executes it with args, and returns the
// captured invocation (or the execution error).
func bindVerb(t *testing.T, v configsurface.CliVerb, args ...string) (*Invocation, error) {
	t.Helper()
	var got *Invocation
	deps := Deps{Exec: func(cmd *cobra.Command, inv *Invocation) error {
		got = inv
		return nil
	}}
	root := &cobra.Command{Use: "orun", SilenceUsage: true, SilenceErrors: true}
	leaf := buildLeafCommand("prov", VerbSpec{Verb: v}, deps)
	root.AddCommand(leaf)
	root.SetArgs(append([]string{leaf.Name()}, args...))
	err := root.Execute()
	return got, err
}

func TestBindArgsPositionalAndBind(t *testing.T) {
	v := configsurface.CliVerb{
		Path: []string{"get"},
		Args: []configsurface.CliArg{
			{Name: "connection", Kind: "positional", Type: "string", Required: true},
		},
		Invoke: configsurface.CliInvoke{Plane: "integrations", Op: "integrations.getConnection",
			Bind: map[string]string{"connection": "connectionId"}},
	}
	inv, err := bindVerb(t, v, "int_123")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if inv.Values["connection"].Str != "int_123" || !inv.Values["connection"].Set {
		t.Errorf("values = %+v", inv.Values)
	}
	if inv.FieldString("connectionId") != "int_123" {
		t.Errorf("FieldString(connectionId) = %q", inv.FieldString("connectionId"))
	}
	// Missing required positional fails with the SP5 dialect.
	if _, err := bindVerb(t, v); err == nil || !strings.Contains(err.Error(), "missing <CONNECTION>") {
		t.Errorf("expected missing-positional error, got: %v", err)
	}
	// Extra positional is rejected.
	if _, err := bindVerb(t, v, "a", "b"); err == nil || !strings.Contains(err.Error(), `unexpected argument "b"`) {
		t.Errorf("expected unexpected-argument error, got: %v", err)
	}
}

func TestBindArgsEnumValidation(t *testing.T) {
	v := configsurface.CliVerb{
		Path: []string{"list"},
		Args: []configsurface.CliArg{
			{Name: "state", Kind: "flag", Type: "enum", Enum: []string{"active", "revoked"}},
		},
		Invoke: configsurface.CliInvoke{Op: "integrations.listConnections"},
	}
	inv, err := bindVerb(t, v, "--state", "active")
	if err != nil || inv.Values["state"].Str != "active" {
		t.Fatalf("enum accept: inv=%+v err=%v", inv, err)
	}
	_, err = bindVerb(t, v, "--state", "pending")
	if err == nil {
		t.Fatal("expected enum rejection")
	}
	for _, want := range []string{"--state must be one of: active, revoked", `got "pending"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%s", want, err)
		}
	}
	// Unset optional enum is fine.
	if _, err := bindVerb(t, v); err != nil {
		t.Errorf("unset optional enum must pass, got: %v", err)
	}
}

func TestBindArgsRepeatFlag(t *testing.T) {
	v := configsurface.CliVerb{
		Path: []string{"list"},
		Args: []configsurface.CliArg{
			{Name: "tag", Kind: "flag", Type: "string", Repeat: true},
		},
		Invoke: configsurface.CliInvoke{Op: "integrations.listConnections"},
	}
	inv, err := bindVerb(t, v, "--tag", "a", "--tag", "b")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := inv.Values["tag"]
	if got.Kind != "list" || len(got.List) != 2 || got.List[1] != "b" {
		t.Errorf("repeat value = %+v", got)
	}
}

func TestBindArgsKV(t *testing.T) {
	v := configsurface.CliVerb{
		Path: []string{"create"},
		Args: []configsurface.CliArg{
			{Name: "param", Kind: "flag", Type: "kv"},
		},
		Invoke: configsurface.CliInvoke{Op: "integrations.listConnections"},
	}
	inv, err := bindVerb(t, v, "--param", "zoneIds=z1,z2", "--param", "token=a=b")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	kv := inv.Values["param"].KV
	if kv["zoneIds"] != "z1,z2" || kv["token"] != "a=b" {
		t.Errorf("kv = %v", kv)
	}
	if _, err := bindVerb(t, v, "--param", "novalue"); err == nil ||
		!strings.Contains(err.Error(), "--param must be key=value") {
		t.Errorf("expected kv shape error, got: %v", err)
	}
	if _, err := bindVerb(t, v, "--param", "a=1", "--param", "a=2"); err == nil ||
		!strings.Contains(err.Error(), "more than once") {
		t.Errorf("expected duplicate-key error, got: %v", err)
	}
}

func TestBindArgsIntBoolAndJSONYes(t *testing.T) {
	v := configsurface.CliVerb{
		Path: []string{"revoke"},
		Args: []configsurface.CliArg{
			{Name: "limit", Kind: "flag", Type: "int"},
			{Name: "force", Kind: "flag", Type: "bool"},
		},
		Invoke: configsurface.CliInvoke{Op: "integrations.revokeConnection"},
	}
	inv, err := bindVerb(t, v, "--limit", "5", "--force", "--json", "--yes")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if inv.Values["limit"].Int != 5 || !inv.Values["force"].Bool {
		t.Errorf("values = %+v", inv.Values)
	}
	if !inv.JSON || !inv.Yes {
		t.Errorf("json/yes = %v/%v", inv.JSON, inv.Yes)
	}
}

func TestBindArgsRequiredFlagEnforced(t *testing.T) {
	v := configsurface.CliVerb{
		Path: []string{"list"},
		Args: []configsurface.CliArg{
			{Name: "connection", Kind: "flag", Type: "string", Required: true},
		},
		Invoke: configsurface.CliInvoke{Op: "integrations.listMinted",
			Bind: map[string]string{"connection": "connectionId"}},
	}
	if _, err := bindVerb(t, v); err == nil || !strings.Contains(err.Error(), "connection") {
		t.Errorf("expected required-flag error, got: %v", err)
	}
}

func TestFieldStringFallbacks(t *testing.T) {
	inv := &Invocation{Values: map[string]ArgValue{
		"connection": {Str: "int_9", Set: true},
	}}
	// No bind map: the conventional short name resolves connectionId.
	if got := inv.FieldString("connectionId"); got != "int_9" {
		t.Errorf("short-name fallback = %q", got)
	}
	inv = &Invocation{
		Bind:   map[string]string{"conn": "connectionId"},
		Values: map[string]ArgValue{"conn": {Str: "int_7", Set: true}},
	}
	if got := inv.FieldString("connectionId"); got != "int_7" {
		t.Errorf("bind-map resolution = %q", got)
	}
	if got := inv.FieldString("credentialId"); got != "" {
		t.Errorf("unbound field = %q", got)
	}
}

package integrationscli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/sourceplane/orun/internal/configsurface"
)

// Plane is the typed client surface the allowlisted ops call through — the
// existing config/integrations planes, implemented by *configsurface.Client.
// Descriptors can only select from the ops below; they can never name a URL,
// header, or local exec (locked decision 2).
type Plane interface {
	ListIntegrationConnections(ctx context.Context, org, provider string) ([]configsurface.IntegrationConnection, error)
	GetIntegrationConnection(ctx context.Context, org, connectionID string) (*configsurface.IntegrationConnection, error)
	RevokeIntegrationConnection(ctx context.Context, org, connectionID string) error
	ListProviderScopeTemplates(ctx context.Context, org, provider string) ([]configsurface.ScopeTemplate, error)
	ListMintedCredentials(ctx context.Context, org, connectionID string) ([]configsurface.MintedCredential, error)
	RevokeMintedCredential(ctx context.Context, org, connectionID, credentialID string) error
	ListProviderSandboxes(ctx context.Context, org, provider string) ([]configsurface.IntegrationSandbox, error)
	ListSecrets(ctx context.Context, scope configsurface.Scope, chain bool) ([]configsurface.SecretMeta, json.RawMessage, error)
}

// ExecEnv carries the resolved runtime for one invocation: the authed plane
// client, the org scope, and the caller's streams. Built by cmd/orun at run
// time (auth + tenancy resolution stay there).
type ExecEnv struct {
	Client Plane
	Org    string
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
	// StaleNote, when set, is printed once to Stderr before execution — the
	// soft-TTL staleness hint (design.md §4).
	StaleNote string
}

// opSpec is one allowlist entry: whether the op mutates (confirm-gated) and
// how it executes.
type opSpec struct {
	confirm bool
	label   func(inv *Invocation) string
	run     func(ctx context.Context, env *ExecEnv, inv *Invocation) error
}

// ops is THE invoke allowlist (design.md §3 — the security boundary). Adding
// an op is a reviewed code change; descriptors can only select from it.
var ops = map[string]opSpec{
	"config.createBrokeredSecret":  {run: runSecretCreateRedirect},
	"config.createRotatedSecret":   {run: runSecretCreateRedirect},
	"config.listSecretsByProvider": {run: invokeListSecretsByProvider},
	"integrations.listConnections": {run: invokeListConnections},
	"integrations.getConnection":   {run: invokeGetConnection},
	"integrations.revokeConnection": {
		confirm: true,
		label:   func(inv *Invocation) string { return "revoke connection " + inv.FieldString("connectionId") },
		run:     invokeRevokeConnection,
	},
	"integrations.connectionHealth": {run: invokeConnectionHealth},
	"integrations.listTemplates":    {run: invokeListTemplates},
	"integrations.listMinted":       {run: invokeListMinted},
	"integrations.revokeMinted": {
		confirm: true,
		label:   func(inv *Invocation) string { return "revoke minted credential " + inv.FieldString("credentialId") },
		run:     invokeRevokeMinted,
	},
	"integrations.listSandboxes": {run: invokeListSandboxes},
}

// OpKnown reports whether op is in the compiled-in allowlist. A served verb
// with an unknown op renders but fails at run time with the update hint.
func OpKnown(op string) bool {
	_, ok := ops[op]
	return ok
}

// opConfirms reports whether op is a confirm-gated mutation (gets --yes).
func opConfirms(op string) bool {
	return ops[op].confirm
}

// ExecuteInvocation runs one bound invocation through the allowlist: staleness
// note, confirmation gate for mutations, then the typed plane call.
func ExecuteInvocation(ctx context.Context, env *ExecEnv, inv *Invocation) error {
	spec, ok := ops[inv.Op]
	if !ok {
		return fmt.Errorf("operation %q is not supported by this orun build; upgrade orun", inv.Op)
	}
	if env.StaleNote != "" {
		fmt.Fprintln(env.Stderr, env.StaleNote)
	}
	if spec.confirm && !inv.Yes {
		label := inv.Op
		if spec.label != nil {
			label = spec.label(inv)
		}
		ok, err := confirm(env, label)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("aborted (pass --yes to skip the confirmation)")
		}
	}
	return spec.run(ctx, env, inv)
}

// confirm prompts on stderr and reads one line from stdin; only an explicit
// y/yes proceeds.
func confirm(env *ExecEnv, label string) (bool, error) {
	fmt.Fprintf(env.Stderr, "%s? [y/N]: ", label)
	line, err := bufio.NewReader(env.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// runSecretCreateRedirect covers the secret-authoring ops when reached outside
// the renderer's SP5 delegation (which is the only supported path — byte-
// identical UX is the no-regression bar).
func runSecretCreateRedirect(_ context.Context, _ *ExecEnv, inv *Invocation) error {
	return fmt.Errorf("secret authoring runs through `orun integrations %s secret create <KEY> …`", inv.Provider)
}

// requireField extracts a non-empty bound string field or fails naming it.
func requireField(inv *Invocation, field, human string) (string, error) {
	v := inv.FieldString(field)
	if strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("missing %s: the verb did not bind a %s", human, field)
	}
	return v, nil
}

// ── integrations-plane ops ───────────────────────────────────────────────────

func invokeListConnections(ctx context.Context, env *ExecEnv, inv *Invocation) error {
	conns, err := env.Client.ListIntegrationConnections(ctx, env.Org, inv.Provider)
	if err != nil {
		return err
	}
	if inv.JSON {
		return emitJSON(env.Stdout, conns)
	}
	if len(conns) == 0 {
		fmt.Fprintf(env.Stdout, "No %s connections in workspace %q.\n", inv.Provider, env.Org)
		return nil
	}
	rows := make([][]string, 0, len(conns))
	for _, c := range conns {
		rows = append(rows, []string{
			c.ID,
			orDash(c.DisplayName),
			orDash(defaultString(c.Status, "active")),
			orDash(c.CreatedAt),
			orDash(c.LastUsedAt),
		})
	}
	fmt.Fprint(env.Stdout, renderColumns([]string{"ID", "NAME", "STATUS", "CREATED", "LAST USED"}, rows))
	return nil
}

func invokeGetConnection(ctx context.Context, env *ExecEnv, inv *Invocation) error {
	id, err := requireField(inv, "connectionId", "<CONNECTION>")
	if err != nil {
		return err
	}
	conn, err := env.Client.GetIntegrationConnection(ctx, env.Org, id)
	if err != nil {
		return err
	}
	if inv.JSON {
		return emitJSON(env.Stdout, conn)
	}
	pairs := [][2]string{
		{"id", conn.ID},
		{"provider", conn.Provider},
		{"name", orDash(conn.DisplayName)},
		{"status", orDash(defaultString(conn.Status, "active"))},
		{"health", connectionHealthCell(*conn)},
		{"auth", orDash(conn.AuthKind)},
		{"created", orDash(conn.CreatedAt)},
		{"last used", orDash(conn.LastUsedAt)},
		{"expires", orDash(conn.ExpiresAt)},
	}
	for _, p := range pairs {
		fmt.Fprintf(env.Stdout, "%-10s %s\n", p[0]+":", p[1])
	}
	return nil
}

func invokeRevokeConnection(ctx context.Context, env *ExecEnv, inv *Invocation) error {
	id, err := requireField(inv, "connectionId", "<CONNECTION>")
	if err != nil {
		return err
	}
	if err := env.Client.RevokeIntegrationConnection(ctx, env.Org, id); err != nil {
		return err
	}
	if inv.JSON {
		return emitJSON(env.Stdout, map[string]interface{}{"connection": id, "revoked": true})
	}
	fmt.Fprintf(env.Stdout, "✓ revoked connection %s (secrets bound to it are now orphaned; see `orun secrets list`)\n", id)
	return nil
}

// invokeConnectionHealth derives per-connection health from the connections
// read: the IR0 fixtures pin no dedicated health endpoint, so health is the
// server-projected `health` field when present, else mapped from `status`
// (active → ok). Execution stays server-validated either way.
func invokeConnectionHealth(ctx context.Context, env *ExecEnv, inv *Invocation) error {
	conns, err := env.Client.ListIntegrationConnections(ctx, env.Org, inv.Provider)
	if err != nil {
		return err
	}
	if inv.JSON {
		type healthRow struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Health string `json:"health"`
		}
		rows := make([]healthRow, 0, len(conns))
		for _, c := range conns {
			rows = append(rows, healthRow{ID: c.ID, Status: defaultString(c.Status, "active"), Health: connectionHealthCell(c)})
		}
		return emitJSON(env.Stdout, rows)
	}
	if len(conns) == 0 {
		fmt.Fprintf(env.Stdout, "No %s connections in workspace %q.\n", inv.Provider, env.Org)
		return nil
	}
	rows := make([][]string, 0, len(conns))
	for _, c := range conns {
		rows = append(rows, []string{
			c.ID,
			orDash(c.DisplayName),
			orDash(defaultString(c.Status, "active")),
			connectionHealthCell(c),
		})
	}
	fmt.Fprint(env.Stdout, renderColumns([]string{"ID", "NAME", "STATUS", "HEALTH"}, rows))
	return nil
}

// connectionHealthCell maps a connection to its health cell: the projected
// health when the server serves one, otherwise derived from status.
func connectionHealthCell(c configsurface.IntegrationConnection) string {
	if c.Health != "" {
		return c.Health
	}
	switch c.Status {
	case "", "active":
		return "ok"
	default:
		return c.Status
	}
}

func invokeListTemplates(ctx context.Context, env *ExecEnv, inv *Invocation) error {
	templates, err := env.Client.ListProviderScopeTemplates(ctx, env.Org, inv.Provider)
	if err != nil {
		return err
	}
	if inv.JSON {
		return emitJSON(env.Stdout, templates)
	}
	if len(templates) == 0 {
		fmt.Fprintf(env.Stdout, "No scope templates declared by %s.\n", inv.Provider)
		return nil
	}
	rows := make([][]string, 0, len(templates))
	for _, t := range templates {
		rows = append(rows, []string{
			t.ID,
			strconv.Itoa(t.Version),
			orDash(t.DisplayName),
			orDash(strings.Join(t.Params, ",")),
			strconv.Itoa(t.MaxTTLSeconds) + "s",
			orDash(defaultString(t.Status, "active")),
		})
	}
	fmt.Fprint(env.Stdout, renderColumns([]string{"ID", "VERSION", "NAME", "PARAMS", "MAX TTL", "STATUS"}, rows))
	return nil
}

func invokeListMinted(ctx context.Context, env *ExecEnv, inv *Invocation) error {
	connID, err := requireField(inv, "connectionId", "--connection <int_…>")
	if err != nil {
		return err
	}
	creds, err := env.Client.ListMintedCredentials(ctx, env.Org, connID)
	if err != nil {
		return err
	}
	if inv.JSON {
		return emitJSON(env.Stdout, creds)
	}
	if len(creds) == 0 {
		fmt.Fprintf(env.Stdout, "No minted credentials for connection %s.\n", connID)
		return nil
	}
	rows := make([][]string, 0, len(creds))
	for _, c := range creds {
		rows = append(rows, []string{
			c.ID,
			orDash(c.Template),
			orDash(defaultString(c.Status, "active")),
			orDash(c.MintedAt),
			orDash(c.ExpiresAt),
			orDash(c.LastUsedAt),
		})
	}
	fmt.Fprint(env.Stdout, renderColumns([]string{"ID", "TEMPLATE", "STATUS", "MINTED", "EXPIRES", "LAST USED"}, rows))
	return nil
}

func invokeRevokeMinted(ctx context.Context, env *ExecEnv, inv *Invocation) error {
	connID, err := requireField(inv, "connectionId", "--connection <int_…>")
	if err != nil {
		return err
	}
	credID, err := requireField(inv, "credentialId", "<CREDENTIAL>")
	if err != nil {
		return err
	}
	if err := env.Client.RevokeMintedCredential(ctx, env.Org, connID, credID); err != nil {
		return err
	}
	if inv.JSON {
		return emitJSON(env.Stdout, map[string]interface{}{"credential": credID, "connection": connID, "revoked": true})
	}
	fmt.Fprintf(env.Stdout, "✓ revoked minted credential %s (connection %s)\n", credID, connID)
	return nil
}

func invokeListSandboxes(ctx context.Context, env *ExecEnv, inv *Invocation) error {
	boxes, err := env.Client.ListProviderSandboxes(ctx, env.Org, inv.Provider)
	if err != nil {
		return err
	}
	if inv.JSON {
		return emitJSON(env.Stdout, boxes)
	}
	if len(boxes) == 0 {
		fmt.Fprintf(env.Stdout, "No %s sandboxes in workspace %q.\n", inv.Provider, env.Org)
		return nil
	}
	rows := make([][]string, 0, len(boxes))
	for _, b := range boxes {
		rows = append(rows, []string{
			b.ID,
			orDash(b.Name),
			orDash(defaultString(b.Status, "active")),
			orDash(b.CreatedAt),
			orDash(b.LastActiveAt),
		})
	}
	fmt.Fprint(env.Stdout, renderColumns([]string{"ID", "NAME", "STATUS", "CREATED", "LAST ACTIVE"}, rows))
	return nil
}

// ── config-plane ops ─────────────────────────────────────────────────────────

// invokeListSecretsByProvider lists workspace-scope secret metadata filtered
// to rows whose rotation producer names the provider. Brokered rows carry no
// provider in their metadata projection, so only provider-rotated rows filter
// positively — the substrate-wide view stays `orun secrets list`.
func invokeListSecretsByProvider(ctx context.Context, env *ExecEnv, inv *Invocation) error {
	items, _, err := env.Client.ListSecrets(ctx, configsurface.Scope{Kind: configsurface.ScopeWorkspace, Org: env.Org}, false)
	if err != nil {
		return err
	}
	var filtered []configsurface.SecretMeta
	for _, m := range items {
		if m.Rotation != nil && m.Rotation.Provider == inv.Provider {
			filtered = append(filtered, m)
		}
	}
	if inv.JSON {
		if filtered == nil {
			filtered = []configsurface.SecretMeta{}
		}
		return emitJSON(env.Stdout, filtered)
	}
	if len(filtered) == 0 {
		fmt.Fprintf(env.Stdout, "No %s-rotated secrets in workspace %q (see `orun secrets list` for all secrets).\n", inv.Provider, env.Org)
		return nil
	}
	rows := make([][]string, 0, len(filtered))
	for _, m := range filtered {
		rows = append(rows, []string{
			m.SecretKey,
			orDash(m.EffectiveScope()),
			orDash(m.Rotation.Template),
			orDash(defaultString(m.Status, "active")),
		})
	}
	fmt.Fprint(env.Stdout, renderColumns([]string{"KEY", "SCOPE", "TEMPLATE", "STATUS"}, rows))
	return nil
}

// emitJSON writes v as indented JSON — metadata only; no secret value is ever
// routed here (the Plane interface has no value-returning method).
func emitJSON(w io.Writer, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	fmt.Fprintln(w, string(data))
	return nil
}

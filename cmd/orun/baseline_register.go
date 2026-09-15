package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/remotestate"
)

// `orun baseline register` — put an account's own baseline in the registry
// (orun-bootstrap-engine BE-O7b, over orun-cloud BR3).
//
// # Why the CLI and not only the console
//
// A baseline is a repository, and the person who has one is standing in it.
// Registering it is three facts they already hold — where it lives, which tag
// is published, how long a build takes — and a verb that takes them where they
// are is the difference between "register it" and "open a console, find the
// form, retype what is on your screen".
//
// # The pair that must be a pair
//
// `--brief` and `--umbrella` are the SHELL LAYER, and a baseline either has one
// or it does not. Passing neither registers a BLUEPRINT-DRIVEN baseline — the
// shape this epic made canonical, and the one this binary's own `baseline new
// --local` builds — where the build document declares every phase and orun runs
// it. Passing one of the two is refused, here and at the door: a brief with no
// umbrella names a runbook with nothing to run.
//
// The refusal is here AS WELL as at the door because the door's is one round
// trip away and this one costs nothing. The door's is the boundary; this is the
// courtesy.
func newBaselineRegisterCommand() *cobra.Command {
	var (
		workspace    string
		backendURL   string
		name         string
		summary      string
		sourceRepo   string
		tag          string
		minutes      int
		visibility   string
		stack        []string
		requires     []string
		brief        string
		umbrella     string
		manifestPath string
	)
	cmd := &cobra.Command{
		Use:   "register <id>",
		Short: "Register a baseline this account owns",
		Long: `Register one of this account's own baselines.

Pass neither --brief nor --umbrella for a BLUEPRINT-DRIVEN baseline: one whose
build document declares every phase, with no shell layer to read. Pass both for
a baseline that has one. Passing one of the two is refused.

Visibility is private unless --visibility unlisted is given. A PUBLIC baseline
is granted by Orunbase and cannot be set here: it is a repository an agent
clones into a stranger's workspace, so it is not an account's to publish.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := registerRequest(args[0], registerFlags{
				name:         name,
				summary:      summary,
				sourceRepo:   sourceRepo,
				tag:          tag,
				minutes:      minutes,
				visibility:   visibility,
				stack:        stack,
				requires:     requires,
				brief:        brief,
				umbrella:     umbrella,
				manifestPath: manifestPath,
				briefSet:     cmd.Flags().Changed("brief"),
				umbrellaSet:  cmd.Flags().Changed("umbrella"),
			})
			if err != nil {
				return err
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			res, err := client.RegisterBaseline(cmd.Context(), client.Scope().OrgID, *req)
			if err != nil {
				return fmt.Errorf("orun baseline register: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "registered %s (%s)\n", res.Baseline.ID, res.Baseline.Visibility)
			fmt.Fprintf(cmd.OutOrStdout(), "  source  %s@%s\n", res.Baseline.SourceRepo, res.Baseline.Tag)
			return nil
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace id (ws_…/org_…) or slug")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "platform base URL")
	cmd.Flags().StringVar(&name, "name", "", "display name (required)")
	cmd.Flags().StringVar(&summary, "summary", "", "one paragraph a reader sees")
	cmd.Flags().StringVar(&sourceRepo, "source-repo", "", "owner/name of the baseline's repository (required)")
	cmd.Flags().StringVar(&tag, "tag", "", "the published tag (required)")
	cmd.Flags().IntVar(&minutes, "expected-minutes", 0, "how long a build takes, measured (required)")
	cmd.Flags().StringVar(&visibility, "visibility", "private", "private | unlisted")
	cmd.Flags().StringSliceVar(&stack, "stack", nil, "display chips, e.g. --stack cloudflare,d1")
	cmd.Flags().StringSliceVar(&requires, "requires", nil, "providers that must be connected first")
	cmd.Flags().StringVar(&brief, "brief", "", "the agent brief's path, for a baseline with a shell layer")
	cmd.Flags().StringVar(&umbrella, "umbrella", "", "the umbrella workflow's path; declare it with --brief or not at all")
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "the build contract's path inside the source repo")
	return cmd
}

type registerFlags struct {
	name         string
	summary      string
	sourceRepo   string
	tag          string
	minutes      int
	visibility   string
	stack        []string
	requires     []string
	brief        string
	umbrella     string
	manifestPath string
	briefSet     bool
	umbrellaSet  bool
}

// registerRequest turns the flags into what the door takes, refusing the
// shapes it would refuse anyway — one round trip earlier, and in the
// vocabulary the caller typed rather than the one the API uses.
func registerRequest(id string, f registerFlags) (*remotestate.RegisterBaselineRequest, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("orun baseline register: an id is required")
	}
	missing := []string{}
	if strings.TrimSpace(f.name) == "" {
		missing = append(missing, "--name")
	}
	if strings.TrimSpace(f.sourceRepo) == "" {
		missing = append(missing, "--source-repo")
	}
	if strings.TrimSpace(f.tag) == "" {
		missing = append(missing, "--tag")
	}
	if f.minutes <= 0 {
		missing = append(missing, "--expected-minutes")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("orun baseline register: %s %s required",
			strings.Join(missing, ", "), pluralIs(len(missing)))
	}
	// PUBLIC is not an account's to grant, and saying so here rather than
	// letting the door say it means the message names the flag the caller
	// typed. It is still refused at the door, which is the boundary.
	switch strings.TrimSpace(f.visibility) {
	case "", "private", "unlisted":
	case "public":
		return nil, fmt.Errorf(
			"orun baseline register: --visibility public is granted by Orunbase, not set here.\n" +
				"A public baseline is a repository an agent clones into a stranger's workspace, so it is\n" +
				"not an account's to publish. Register it unlisted and ask for it to be listed.")
	default:
		return nil, fmt.Errorf("orun baseline register: --visibility must be private or unlisted")
	}

	// DECLARE BOTH OR NEITHER. `Changed` rather than emptiness, so an explicit
	// `--brief ""` is the same statement as omitting it and an explicit
	// `--brief x --umbrella ""` is still half a shell layer.
	brief, umbrella := strings.TrimSpace(f.brief), strings.TrimSpace(f.umbrella)
	if (brief != "") != (umbrella != "") {
		return nil, fmt.Errorf(
			"orun baseline register: --brief and --umbrella are a pair — declare both, or neither.\n" +
				"A baseline with a brief is run through its umbrella; one with neither is blueprint-driven,\n" +
				"and its build document declares every phase.")
	}

	req := &remotestate.RegisterBaselineRequest{
		ID:              id,
		Name:            strings.TrimSpace(f.name),
		Summary:         strings.TrimSpace(f.summary),
		Stack:           f.stack,
		SourceRepo:      strings.TrimSpace(f.sourceRepo),
		Tag:             strings.TrimSpace(f.tag),
		Requires:        f.requires,
		ExpectedMinutes: f.minutes,
		ManifestPath:    strings.TrimSpace(f.manifestPath),
	}
	if v := strings.TrimSpace(f.visibility); v != "" && v != "private" {
		req.Visibility = v
	}
	// Sent only when there IS a shell layer. An explicit `""` pair would be
	// the same statement to the door, but sending fields nobody set makes a
	// request that reads as a claim about paths the caller never mentioned.
	if brief != "" {
		req.BriefPath = &brief
		req.UmbrellaPath = &umbrella
	}
	return req, nil
}

func pluralIs(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

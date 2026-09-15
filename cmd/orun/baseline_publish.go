package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// `orun baseline publish` — move a registered baseline to a tag
// (orun-bootstrap-engine BE-O7b, over orun-cloud BE-K4c).
//
// # The order this verb exists to remove
//
// A baseline's version lives in two repositories: the git tag in its own
// source, and the pin in the platform's registry. They have to move in that
// order — what a build first reads is fetched from the source repo AT THE
// REGISTRY'S TAG, so a pin moved before the tag is pushed means every build of
// that baseline 404s until somebody notices. The registry's own catalogue file
// warns about this in a comment, which is the only place the rule lived.
//
// One verb cannot get the order wrong: push the tag, then run this. If the tag
// is not there, the registry does not move.
//
// # Nothing is decided here
//
// The door proves the tag — that it IS a tag and not a branch, that the files a
// build enters through resolve at it, that the build contract parses with the
// platform's own parser — and this reports what it said. A client that formed
// its own opinion would be a second answer to a question the platform already
// answers, and the two would drift the first time the contract changed.
//
// That is also why the refusal is printed verbatim rather than summarised: the
// door's message names the file and the line, and a CLI that replaced it with
// "publish failed" would throw away the only part worth reading.
func newBaselinePublishCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
	)
	cmd := &cobra.Command{
		Use:   "publish <id> <tag>",
		Short: "Move a registered baseline to a tag, once the platform has proven it",
		Long: `Move one of this account's registered baselines to a tag.

Push the git tag in the baseline's own repository FIRST. The platform then
proves the tag before the registry moves: that it is a tag rather than a
branch, that the files a build reads first are in it, and that the build
contract parses. A tag that fails any of those leaves the registry untouched
and the reason is printed.

An Orunbase-maintained baseline is not published this way — its catalogue is
a file in orun-cloud and moves by pull request. The refusal says so.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, tag, err := parsePublishArgs(args)
			if err != nil {
				return err
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			res, err := client.PublishBaseline(cmd.Context(), client.Scope().OrgID, id, tag)
			if err != nil {
				// Verbatim: the door's message names the file, the line and
				// what to do, and "publish failed" would throw away the only
				// part worth reading.
				return fmt.Errorf("orun baseline publish: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is now published at %s\n", res.Baseline.ID, res.PublishedTag)
			fmt.Fprintf(cmd.OutOrStdout(), "  source  %s@%s\n", res.Baseline.SourceRepo, res.PublishedTag)
			return nil
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace id (ws_…/org_…) or slug")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "platform base URL")
	return cmd
}

// parsePublishArgs splits `<id> <tag>`, and refuses the one shape that would
// otherwise be read two ways.
//
// `id@tag` is the READ vocabulary — `show`, `check` and `new` all take it, and
// there the tag says WHICH VERSION TO LOOK AT. Here the tag is what CHANGES,
// so `orun baseline publish cirrus@baseline-v5 baseline-v6` has two tags in it
// and no reading of it is obviously right. Refused rather than guessed: the
// wrong guess moves a registry pin.
func parsePublishArgs(args []string) (id, tag string, err error) {
	id = strings.TrimSpace(args[0])
	tag = strings.TrimSpace(args[1])
	if id == "" || tag == "" {
		return "", "", fmt.Errorf("orun baseline publish: an id and a tag are both required")
	}
	if strings.Contains(id, "@") {
		return "", "", fmt.Errorf(
			"orun baseline publish: pass the id and the tag as two arguments (%q looks like an id@tag address;\n"+
				"publishing is the one verb where the tag is what CHANGES rather than what to read)", id)
	}
	return id, tag, nil
}

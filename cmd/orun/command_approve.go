package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/approval"
)

var (
	approveReject bool
	approveBy     string
)

// approveCmd resolves a paused workflow approval gate (orun-workflows-v2 §9).
// The verdict — who, when, what — seals into .orun/approvals as a run fact.
var approveCmd = &cobra.Command{
	Use:   "approve [jobID stepID]",
	Short: "Approve (or reject) a paused workflow step, or list pending approvals",
	Long: `Resolve a workflow step paused on an approval gate.

  orun approve                      list pending approvals
  orun approve <jobID> <stepID>     approve
  orun approve <jobID> <stepID> --reject
`,
	Args: cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		workspace, err := os.Getwd()
		if err != nil {
			return err
		}
		if len(args) < 2 {
			pending, perr := approval.Pending(workspace)
			if perr != nil {
				return perr
			}
			if len(pending) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no pending approvals")
				return nil
			}
			for _, req := range pending {
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s — %s\n", req.RequestedAt.Format("15:04:05"), req.JobID, req.StepID, req.Prompt)
			}
			return nil
		}
		by := approveBy
		if by == "" {
			by = os.Getenv("USER")
		}
		if err := approval.Decide(workspace, args[0], args[1], !approveReject, by); err != nil {
			return err
		}
		verdict := "approved"
		if approveReject {
			verdict = "rejected"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s / %s\n", verdict, args[0], args[1])
		return nil
	},
}

func registerApproveCommand(root *cobra.Command) {
	root.AddCommand(approveCmd)
	approveCmd.Flags().BoolVar(&approveReject, "reject", false, "Reject instead of approving")
	approveCmd.Flags().StringVar(&approveBy, "by", "", "Who is deciding (defaults to $USER)")
}

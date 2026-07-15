package scaffold

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runHooks executes the declared postInstantiate hooks (design §12) AFTER
// placement and OUTSIDE the template sandbox. v1 is a minimal audited executor:
// an explicit argv (no shell), run in the output directory, logged, opt-in per
// instantiation (Options.RunHooks). orun names no ecosystem — it runs whatever
// argv the baseline declared (invariant 8).
func runHooks(hooks []Hook, outDir string) ([]string, error) {
	var ran []string
	for _, h := range hooks {
		if len(h.Run) == 0 {
			return ran, fmt.Errorf("hook %q: empty run argv", h.ID)
		}
		// No shell: exec the argv directly, so nothing is interpreted.
		cmd := exec.Command(h.Run[0], h.Run[1:]...) //nolint:gosec // declared argv, no shell, opt-in
		cmd.Dir = outDir
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return ran, fmt.Errorf("hook %q (%s): %w", h.ID, strings.Join(h.Run, " "), err)
		}
		ran = append(ran, h.ID)
	}
	return ran, nil
}

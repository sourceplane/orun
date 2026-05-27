package runbundle

import (
	"fmt"
	"regexp"
	"strings"
)

// artifactNameRe matches valid artifact names and captures components.
var artifactNameRe = regexp.MustCompile(`^orun\.v1\.([a-zA-Z0-9_-]+)\.(plan|job)\.(.+)\.(.+)$`)

// safeExecIDRe rejects exec IDs containing characters unsafe for artifact naming.
var safeExecIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ArtifactName constructs the GitHub artifact name for a shard.
// Format: orun.v1.<exec-id>.<role>.<suffix>.<status>
func ArtifactName(execID string, role ShardRole, suffix, status string) string {
	return fmt.Sprintf("orun.v1.%s.%s.%s.%s", execID, role, suffix, status)
}

// ParsedShardName holds the components extracted from an artifact name.
type ParsedShardName struct {
	ExecID string
	Role   ShardRole
	Suffix string
	Status string
}

// ParseShardName parses an artifact name into its components.
// Returns nil if the name doesn't match the expected format.
func ParseShardName(name string) *ParsedShardName {
	matches := artifactNameRe.FindStringSubmatch(name)
	if matches == nil {
		return nil
	}
	return &ParsedShardName{
		ExecID: matches[1],
		Role:   ShardRole(matches[2]),
		Suffix: matches[3],
		Status: matches[4],
	}
}

// ExecID constructs the execution ID for GitHub Actions runs.
// Format: gh-{run_id}-{attempt}-{plan_short_sha}
func ExecID(runID, runAttempt, planShortSHA string) string {
	return fmt.Sprintf("gh-%s-%s-%s", runID, runAttempt, planShortSHA)
}

// ValidExecID checks whether an execID is safe for use in artifact names.
func ValidExecID(execID string) bool {
	return safeExecIDRe.MatchString(execID)
}

// IsOrunArtifact checks whether an artifact name belongs to the Orun naming scheme.
func IsOrunArtifact(name string) bool {
	return strings.HasPrefix(name, "orun.v1.")
}
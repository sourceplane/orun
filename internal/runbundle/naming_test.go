package runbundle

import "testing"

func TestArtifactName(t *testing.T) {
	tests := []struct {
		name   string
		execID string
		role   ShardRole
		suffix string
		status string
		want   string
	}{
		{
			name:   "plan shard",
			execID: "gh-26185145757-1-a1b2c3d4",
			role:   ShardRolePlan,
			suffix: "a1b2c3d4",
			status: "created",
			want:   "orun.v1.gh-26185145757-1-a1b2c3d4.plan.a1b2c3d4.created",
		},
		{
			name:   "job shard completed",
			execID: "gh-26185145757-1-a1b2c3d4",
			role:   ShardRoleJob,
			suffix: "7f6a9c21d4e8b012",
			status: "completed",
			want:   "orun.v1.gh-26185145757-1-a1b2c3d4.job.7f6a9c21d4e8b012.completed",
		},
		{
			name:   "job shard failed",
			execID: "gh-26185145757-1-a1b2c3d4",
			role:   ShardRoleJob,
			suffix: "7f6a9c21d4e8b012",
			status: "failed",
			want:   "orun.v1.gh-26185145757-1-a1b2c3d4.job.7f6a9c21d4e8b012.failed",
		},
		{
			name:   "simple names",
			execID: "test-1-abc",
			role:   ShardRolePlan,
			suffix: "abc",
			status: "created",
			want:   "orun.v1.test-1-abc.plan.abc.created",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ArtifactName(tt.execID, tt.role, tt.suffix, tt.status)
			if got != tt.want {
				t.Errorf("ArtifactName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseShardName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  *ParsedShardName
	}{
		{
			name:  "plan shard",
			input: "orun.v1.gh-26185145757-1-a1b2c3d4.plan.a1b2c3d4.created",
			want: &ParsedShardName{
				ExecID: "gh-26185145757-1-a1b2c3d4",
				Role:   ShardRolePlan,
				Suffix: "a1b2c3d4",
				Status: "created",
			},
		},
		{
			name:  "job shard completed",
			input: "orun.v1.gh-26185145757-1-a1b2c3d4.job.7f6a9c21d4e8b012.completed",
			want: &ParsedShardName{
				ExecID: "gh-26185145757-1-a1b2c3d4",
				Role:   ShardRoleJob,
				Suffix: "7f6a9c21d4e8b012",
				Status: "completed",
			},
		},
		{
			name:  "job shard failed",
			input: "orun.v1.gh-26185145757-1-a1b2c3d4.job.7f6a9c21d4e8b012.failed",
			want: &ParsedShardName{
				ExecID: "gh-26185145757-1-a1b2c3d4",
				Role:   ShardRoleJob,
				Suffix: "7f6a9c21d4e8b012",
				Status: "failed",
			},
		},
		{
			name:  "non-orun name",
			input: "some-other-artifact",
			want:  nil,
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "wrong prefix",
			input: "other.v1.exec.plan.suffix.status",
			want:  nil,
		},
		{
			name:  "missing fields",
			input: "orun.v1.exec-id.plan",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseShardName(tt.input)
			if tt.want == nil {
				if got != nil {
					t.Errorf("ParseShardName() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ParseShardName() = nil, want %+v", tt.want)
			}
			if got.ExecID != tt.want.ExecID {
				t.Errorf("ExecID: got %q, want %q", got.ExecID, tt.want.ExecID)
			}
			if got.Role != tt.want.Role {
				t.Errorf("Role: got %q, want %q", got.Role, tt.want.Role)
			}
			if got.Suffix != tt.want.Suffix {
				t.Errorf("Suffix: got %q, want %q", got.Suffix, tt.want.Suffix)
			}
			if got.Status != tt.want.Status {
				t.Errorf("Status: got %q, want %q", got.Status, tt.want.Status)
			}
		})
	}
}

func TestExecID(t *testing.T) {
	tests := []struct {
		name         string
		runID        string
		runAttempt   string
		planShortSHA string
		want         string
	}{
		{
			name:         "typical values",
			runID:        "26185145757",
			runAttempt:   "1",
			planShortSHA: "a1b2c3d4",
			want:         "gh-26185145757-1-a1b2c3d4",
		},
		{
			name:         "with underscores",
			runID:        "26185145757",
			runAttempt:   "1",
			planShortSHA: "a1b2_c3d4",
			want:         "gh-26185145757-1-a1b2_c3d4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExecID(tt.runID, tt.runAttempt, tt.planShortSHA)
			if got != tt.want {
				t.Errorf("ExecID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidExecID(t *testing.T) {
	tests := []struct {
		name   string
		execID string
		valid  bool
	}{
		{"valid typical", "gh-26185145757-1-a1b2c3d4", true},
		{"valid with underscores", "gh-26185145757-1-a1b2_c3d4", true},
		{"valid with dots", "gh-26185145757-1-a1b2.c3d4", false},
		{"invalid with spaces", "gh 26185145757 1 a1b2c3d4", false},
		{"invalid with special chars", "gh-26185145757-1-a1b2@c3d4", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidExecID(tt.execID)
			if got != tt.valid {
				t.Errorf("ValidExecID(%q) = %v, want %v", tt.execID, got, tt.valid)
			}
		})
	}
}

func TestIsOrunArtifact(t *testing.T) {
	tests := []struct {
		name string
		input string
		want bool
	}{
		{"orun artifact", "orun.v1.exec.plan.suffix.status", true},
		{"orun artifact with more dots", "orun.v1.gh-123-1-abc.job.uid.completed", true},
		{"other artifact", "some-other-artifact", false},
		{"similar prefix", "orun.other", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsOrunArtifact(tt.input)
			if got != tt.want {
				t.Errorf("IsOrunArtifact(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseShardNameRoundTrip(t *testing.T) {
	// Verify that ArtifactName + ParseShardName is a round trip
	execID := "gh-98765-2-x1y2z3"
	role := ShardRoleJob
	suffix := "abc123def456"
	status := "completed"

	name := ArtifactName(execID, role, suffix, status)
	parsed := ParseShardName(name)

	if parsed == nil {
		t.Fatal("ParseShardName returned nil for valid name")
	}
	if parsed.ExecID != execID {
		t.Errorf("ExecID: got %q, want %q", parsed.ExecID, execID)
	}
	if parsed.Role != role {
		t.Errorf("Role: got %q, want %q", parsed.Role, role)
	}
	if parsed.Suffix != suffix {
		t.Errorf("Suffix: got %q, want %q", parsed.Suffix, suffix)
	}
	if parsed.Status != status {
		t.Errorf("Status: got %q, want %q", parsed.Status, status)
	}
}
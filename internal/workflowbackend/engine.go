package workflowbackend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// EngineEnv names the environment variable that points at the pinned torkflow
// engine binary when EngineOptions.Bin is empty.
const EngineEnv = "ORUN_TORKFLOW_ENGINE"

// EngineBackendArgs is the default argv orun passes the engine before streaming
// the JSON Request on stdin: the engine runs its "backend" mode, reading a
// Request on stdin and writing a Result on stdout (design §5).
var EngineBackendArgs = []string{"backend"}

// Engine runs a digest-pinned workflow engine. Digest lets a plan's source list
// or a provenance lock pin exactly which engine executed a workflow (design §5);
// Invoke runs one workflow over the JSON contract (design §5/§6).
type Engine interface {
	// Digest is the content digest of the engine artifact ("sha256:<hex>").
	Digest() string
	// Invoke runs one workflow request. It returns an error only for
	// infrastructure failures (the engine could not run, or produced invalid
	// output). A workflow that ran and failed returns a Result whose Status is
	// not StatusSuccess, with a nil error — the caller decides what to do.
	Invoke(ctx context.Context, req Request) (Result, error)
}

// EngineOptions configures engine resolution.
type EngineOptions struct {
	// Bin is the path to the pinned torkflow engine binary. When empty,
	// ResolveEngine falls back to the ORUN_TORKFLOW_ENGINE environment variable.
	Bin string
	// Args are passed to the engine before the JSON request is streamed on
	// stdin. When nil, EngineBackendArgs is used.
	Args []string
}

// ResolveEngine locates the pinned engine binary and computes its content
// digest, returning a SubprocessEngine ready to Invoke. A missing or
// unconfigured engine is a clear pre-flight error, not a mid-step crash (S-4).
func ResolveEngine(opts EngineOptions) (*SubprocessEngine, error) {
	bin := opts.Bin
	if bin == "" {
		bin = os.Getenv(EngineEnv)
	}
	if bin == "" {
		return nil, fmt.Errorf("workflow engine not configured: set EngineOptions.Bin or %s", EngineEnv)
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		return nil, fmt.Errorf("workflow engine %q not readable: %w", bin, err)
	}
	args := opts.Args
	if args == nil {
		args = EngineBackendArgs
	}
	sum := sha256.Sum256(data)
	return &SubprocessEngine{
		Bin:    bin,
		Args:   args,
		digest: fmt.Sprintf("sha256:%x", sum),
	}, nil
}

// SubprocessEngine invokes the pinned engine as a child process over the JSON
// contract — the same process boundary torkflow uses for its own providers
// (internal/executor/binary.go in torkflow). No cross-module Go import is
// required (design §5, S-5).
type SubprocessEngine struct {
	Bin    string
	Args   []string
	digest string
}

// Digest returns the pinned engine's content digest.
func (e *SubprocessEngine) Digest() string { return e.digest }

// Invoke streams req to the engine on stdin and decodes its Result from stdout.
func (e *SubprocessEngine) Invoke(ctx context.Context, req Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return Result{}, fmt.Errorf("marshal workflow request: %w", err)
	}

	cmd := exec.CommandContext(ctx, e.Bin, e.Args...) //nolint:gosec // pinned engine path, no shell
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		// The engine may still emit a structured Result (Status: failed) on
		// stdout with a non-zero exit — prefer it so the caller sees the
		// workflow-level failure rather than an opaque exec error.
		if res, ok := decodeResult(stdout.Bytes()); ok {
			return res, nil
		}
		msg := bytes.TrimSpace(stderr.Bytes())
		if len(msg) == 0 {
			msg = bytes.TrimSpace(stdout.Bytes())
		}
		return Result{}, fmt.Errorf("workflow engine failed: %w (%s)", runErr, msg)
	}

	res, ok := decodeResult(stdout.Bytes())
	if !ok {
		return Result{}, fmt.Errorf("workflow engine produced invalid output: %q", stdout.String())
	}
	return res, nil
}

// decodeResult parses engine stdout into a Result, reporting whether the bytes
// were a well-formed non-empty Result document.
func decodeResult(b []byte) (Result, bool) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return Result{}, false
	}
	var res Result
	if err := json.Unmarshal(b, &res); err != nil {
		return Result{}, false
	}
	return res, true
}

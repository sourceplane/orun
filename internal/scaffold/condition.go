package scaffold

import (
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
)

// Phase conditions (orun-bootstrap-engine BE-O3).
//
// CEL, for the reason orun-workflows-v3 already settled on it: a condition must
// be side-effect-free and terminating, which is the property CEL was built for.
// "Whatever a template engine accepts" is not a contract.
//
// The environment is deliberately NARROWER than the workflow engine's: a phase
// condition sees `inputs` and nothing else. It cannot see another phase's
// result, because a phase must be answerable alone — a condition depending on
// what a previous phase produced could not be evaluated by a resumed run in a
// container that never ran it.
func conditionEnv() (*cel.Env, error) {
	return cel.NewEnv(
		ext.Strings(),
		cel.Variable("inputs", cel.MapType(cel.StringType, cel.DynType)),
	)
}

// compileCondition checks a phase condition at PARSE time, so a typo in a
// `when:` is a blueprint error rather than a phase that silently never runs —
// which is the worse failure, because nothing appears to be wrong.
func compileCondition(src, where string) error {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	env, err := conditionEnv()
	if err != nil {
		return err
	}
	ast, iss := env.Compile(src)
	if iss != nil && iss.Err() != nil {
		return fmt.Errorf("%s: %v", where, iss.Err())
	}
	if !ast.OutputType().IsAssignableType(cel.BoolType) {
		return fmt.Errorf("%s: condition must evaluate to true or false, not %s", where, ast.OutputType())
	}
	return nil
}

// evalCondition evaluates a phase condition against the collected inputs. An
// empty condition is true.
func evalCondition(src string, inputs map[string]any) (bool, error) {
	if strings.TrimSpace(src) == "" {
		return true, nil
	}
	env, err := conditionEnv()
	if err != nil {
		return false, err
	}
	ast, iss := env.Compile(src)
	if iss != nil && iss.Err() != nil {
		return false, iss.Err()
	}
	prg, err := env.Program(ast)
	if err != nil {
		return false, err
	}
	if inputs == nil {
		inputs = map[string]any{}
	}
	out, _, err := prg.Eval(map[string]any{"inputs": inputs})
	if err != nil {
		return false, fmt.Errorf("evaluating %q: %w", src, err)
	}
	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("condition %q produced %T, not a boolean", src, out.Value())
	}
	return b, nil
}

# GitHub Actions Example

This example shows a component that installs Helm with `azure/setup-helm` and then uses the `helm` binary from later `run:` steps.

Run the commands from `examples/gha-actions`:

```bash
cd examples/gha-actions
```

Generate a plan:

```bash
go run ../../cmd/liteci plan \
  --intent intent.yaml \
  --config-dir compositions \
  --output /tmp/liteci-gha-actions-plan.json
```

Execute the plan:

```bash
go run ../../cmd/liteci run \
  --plan /tmp/liteci-gha-actions-plan.json \
  --execute
```

`liteci run` auto-selects GitHub Actions compatibility mode because the compiled plan contains a `use:` step. The run succeeds when `azure/setup-helm` provisions Helm and the following shell step can execute `helm version --short`.
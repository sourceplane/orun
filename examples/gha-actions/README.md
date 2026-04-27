# GitHub Actions Example

This example shows a component that installs Helm with `azure/setup-helm` and then uses the `helm` binary from later `run:` steps.

Run the commands from `examples/gha-actions`:

```bash
cd examples/gha-actions
```

Generate a plan:

```bash
go run ../../cmd/gluon plan \
  --intent intent.yaml \
  --output /tmp/gluon-gha-actions-plan.json
```

Inspect the packaged example composition:

```bash
go run ../../cmd/gluon compositions --intent intent.yaml
```

Run the plan:

```bash
go run ../../cmd/gluon run \
  --plan /tmp/gluon-gha-actions-plan.json
```

`gluon run` auto-selects GitHub Actions compatibility mode because the compiled plan contains a `use:` step. The run succeeds when `azure/setup-helm` provisions Helm and the following shell step can execute `helm version --short`.

Planning also writes `examples/gha-actions/.gluon/compositions.lock.yaml`, so the example records exactly which composition source was resolved.

Successful runs use the compact `run` output by default. Add `--verbose` if you want the full GitHub Actions-compatible step logs inline.

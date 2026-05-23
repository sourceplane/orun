# PR 10: Workflow Template + CI Updates + Website Docs

**Phase:** 10 (from implementation plan)
**Size:** Medium — ~6 files changed/created

## Goal

Document the GitHub artifact workflow, update CI to use the new artifact flow, and add website documentation.

## Files to create/change

### `docs/examples/github-artifacts-workflow.yaml` (create)
- Template workflow YAML showing the no-upload-step pattern
- Uses `--artifact github`, `--github-output`, and env-based activation

### `docs/github-artifacts.md` (create)
- Usage guide explaining the artifact system
- Three levels of remote inspection
- Partial hydration explanation
- Security considerations

### `.github/workflows/orun-default-workflow.yaml` (change)
- Replace `actions/upload-artifact@v4` and `actions/download-artifact@v4` with Orun-native artifact flow
- Add `ORUN_ARTIFACT_BACKEND=github` and `ORUN_ARTIFACT_UPLOAD=true` env vars
- Add `actions: write` permission for artifact upload
- Use `--artifact github` and `--github-output` flags

### `.github/workflows/release-oci.yaml` (change)
- Ensure release workflow also enables artifacts if applicable

### Website docs

#### `website/docs/cli/orun-plan.md` (change)
- Document `--artifact` and `--github-output` flags
- Add CI usage example

#### `website/docs/cli/orun-run.md` (change)
- Document `--artifact` flag
- Document defer/finally upload semantics
- Add CI matrix example with artifact flow

#### `website/docs/concepts/execution-model.md` (change)
- Add "CI artifacts" section explaining shard-based evidence

## Dependencies
- PR 7 (plan integration)
- PR 8 (run integration)
- PR 9 (CLI github subcommands)
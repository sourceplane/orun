# orun-bootstrap-engine — Risks and open questions

Status: Decisions taken, with their reasons, and the questions still open.
The baseline-side decisions live in cirrus `specs/epics/saas-bootstrap-engine/`;
the console-side in orun-cloud's. These are the binary's.

## Decisions taken

| Decision | Why |
|---|---|
| **Typed actions (`uses:` + `with:`) rather than `run: ["orun", …]` argv.** | Five differences, all real: a bad parameter becomes a parse-time error naming the line instead of an exit code mid-bootstrap; no binary need be on `PATH`; hook outputs become addressable (argv hooks have none — `hookRunner` sends stdout to stderr and discards it); failures are typed; and a closed set is a genuine audit boundary for steps that already run **outside** the template sandbox. |
| **`run:` survives as the ecosystem escape.** | Invariant 8 — orun's core names no ecosystem. An *action* is orun's own surface; *argv* is how a blueprint reaches `node`. Removing `run:` would force ecosystem specifics into the binary, which is the thing `orun-scaffolding` was careful not to do. |
| **The action set is closed and compiled in, not a plugin directory.** | `orun-workflows-v3` already rejected "a filesystem store of loose executables as the extension model". A closed set is what makes the registry auditable and what makes the reciprocal test (§7) able to say which baselines a change breaks. If an OCI action-package seam is ever wanted, it is v3's design to extend, not a second mechanism. |
| **Phase state is derived; a stored file is a cache and must be safe to delete.** | `.orun/*` is gitignored in cirrus and in every product it scaffolds, so the per-phase provenance the flows archive has never survived a container — and the paced bootstrap works anyway. Deriving is what makes "a different session, months later, a fresh container" possible. A ledger would quietly become the source of truth and break the property nobody would notice losing until a customer did. |
| **BE-O2 (derivation) lands before BE-O4 (the cache).** | So the cache cannot become load-bearing by accident. If `.orun/run.state` is ever required for correctness, that is a bug, and the ordering is what lets a test assert it. |
| **BE-O8 (deleting `Hook.Workflow`) lands after BE-O5 (the full action set).** | Invert them and a baseline has no way to express a bootstrap in between. This is a safety property of the sequence, not a preference. |
| **Provenance keeps recording no runtime outcome.** | `provenance.go`'s rule — *"Reference + digest only — never the hook's runtime outcome"* — is right for a scaffold. Run state goes in a separate file with a separate lifetime, so the reviewable artifact does not churn on every retry. |
| **Inputs on resume are a conflict, not an override.** | `InputsHash` already exists. A product half-branded one way and half another is silent and expensive; re-inputting is `upgrade`'s operation. |
| **Narration is rendered through the constrained funcmap, and may not set state.** | Narration is prose a *baseline* authored. Rendering it with the same sandbox rules as module templates (no file/exec/net) keeps it data. Forbidding it from setting state keeps the engine the only thing that can say a phase is done. |
| **`orun baseline new --local` does not re-implement the paid gate, the grant or the runner.** | It does not need them: the operator is already the principal. The privileged path stays behind one door in orun-cloud. A CLI that re-derived entitlement would be a second answer to a question the platform already answers. |

## Open questions

| # | Question | Leaning |
|---|---|---|
| 1 | **Does `CycleBreak` actually replace a baseline's strip/restore two-pass?** The field describes *"module pairs whose edge is a deferred feedback edge in a declared binding cycle"*, which is the shape of cirrus's phase-04 dance — but that script defers *binding content in a rendered config*, and the field may defer *placement order*. | Verify in BE-O1 and record the answer. If they differ, the baseline keeps its `run:` hook and a follow-on milestone adds content-level deferral. Do not let this block anything. |
| 2 | **Where does `poll:`/`until:` live, given `orun-workflows-v3` also specifies it?** | Both, with one implementation. The wait primitive is a package; `kind: Workflow` gets it as a step verb and the phase engine gets it as an `await` hook result. Two call sites, one timer/backoff/cancellation implementation. Duplicating it would be exactly the vocabulary debt v3's rev 2 rejected. |
| 3 | **Should `--progress json` be a stable public contract?** A baseline's end-to-end CI will assert against it, which makes it an interface. | Yes, and version it in the envelope (`schema: "bootstrap-event/v1"`). A test-only format that four repos depend on is a public contract whether or not it is called one. |
| 4 | **How much history does `orun baseline status` need?** Deriving "landed" from git log implies a deep enough clone. | Derive from the task plane first (it is authoritative and needs no history) and use git only to confirm placement, which a shallow clone answers. Record the depth requirement if one turns out to be unavoidable. |
| 5 | **Does the lease belong here or in orun-cloud?** | orun-cloud, keyed (workspace, product repo). The binary asks for it and reports the holder; it does not own it. A lease held in a container dies with the container, which is the opposite of what it is for. |

## Risks

| Risk | Mitigation |
|---|---|
| **The action registry becomes a plugin system by accretion.** Every baseline that needs something new asks for an action. | Eight actions is the budget for the whole cluster. Treat a ninth request as a design question — is this genuinely generic, or baseline policy that belongs in a `run:` hook? Reviewed at BE-O5, not deferred. |
| **Two parsers of the baseline manifest** — this binary's and orun-cloud's — drift, reintroducing at a new seam the exact failure the cluster exists to end. | The schema is vendored with a parity test, the `orun-mcp` UM0–UM6 pattern. Locked, not a guideline. |
| **`internal/scaffold` grows a dependency on the platform**, turning a pure, sandboxed package into a networked one. | Actions live in `internal/actions` and are injected; the engine calls an interface. A unit test constructs the engine with a recording registry and no network, which is also what a baseline's phase-simulation tier uses. |
| **The event stream becomes a firehose** and the publish path becomes the bottleneck of an hour-long build. | `detail` is batched and capped per phase; `narration` is one line per transition. Caps are set from the first real end-to-end measurement rather than guessed — no bootstrap has ever been measured end to end, which is itself the point of the baseline-side tier 3. |
| **`orun baseline` drifts from the platform's routes** as orun-cloud evolves them. | The verbs map 1:1 onto existing routes and add no logic. A contract test against the vendored route list, same discipline as the manifest. |

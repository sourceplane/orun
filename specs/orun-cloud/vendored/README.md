# Vendored wire contract

This directory holds a **verbatim** copy of the normative State API contract
owned by the platform repo:

    orun-cloud/specs/epics/saas-orun-platform/state-api-contract.md

The platform repo owns the normative copy; this repo vendors it so that the
`internal/remotestate` client can be developed and reviewed against a stable,
checked-in version of the seam. Neither repo may break the contract
unilaterally (see `specs/orun-cloud/README.md`).

## Drift guard

`CHECKSUM` records the sha256 of `state-api-contract.md`. The Go test
`TestVendoredContractChecksum` in
`internal/remotestate/contract_vendor_test.go` recomputes the digest of the
vendored file and fails the build if it no longer matches `CHECKSUM`.

This is an **in-repo integrity guard**: it catches an accidental or
unreviewed edit to the vendored copy. A true cross-repo live diff against the
platform repo's source needs orun-cloud access at CI time, which is not
guaranteed in this repo's CI; the guard here is the portable equivalent. If a
cross-repo fetch/vendor mechanism is later added to this repo, fold this guard
into it.

## Re-vendor procedure

When the platform repo changes the contract (additively or with a contract
version bump per the platform's change-control), re-vendor here:

1. Copy the new source verbatim:

       cp ../../../../orun-cloud/specs/epics/saas-orun-platform/state-api-contract.md \
          specs/orun-cloud/vendored/state-api-contract.md

   (adjust the source path to wherever the platform repo is checked out).

2. Recompute and record the new checksum in `CHECKSUM`:

       sha256sum specs/orun-cloud/vendored/state-api-contract.md
       # paste "<sha256>  state-api-contract.md" into CHECKSUM (replacing the old line)

3. Update `internal/remotestate` (and tests) for any contract change, run
   `go test ./...`, and commit the re-vendor together with the client change
   so the diff documents the contract delta that motivated it.

If `TestVendoredContractChecksum` fails unexpectedly, it means the vendored
file changed without the checksum being updated — either revert the edit, or
**re-vendor from orun-cloud or renegotiate the contract**, then update
`CHECKSUM`.

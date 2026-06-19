# Vendored wire contract

This directory holds a **verbatim** copy of the normative Coordination API
contract owned by the platform repo:

    orun-cloud/specs/epics/saas-orun-backend-merge/coordination-api.md

The platform repo owns the normative copy; this repo vendors it so the
`internal/remotestate` client can be developed and reviewed against a stable,
checked-in version of the seam. Neither repo may break the contract unilaterally
(see `../README.md`).

## Drift guard

`CHECKSUM` records the sha256 of `coordination-api.md`. A Go test
(`TestVendoredCoordinationChecksum`, to land with NC0) recomputes the digest of
the vendored file and fails the build if it no longer matches `CHECKSUM` — the
same in-repo integrity guard used by `specs/orun-cloud/vendored/`.

## Re-vendor procedure

When the platform repo changes the contract (additively or with a contract
version bump per its change-control), re-vendor here:

1. Copy the new source verbatim:

       cp ../../../../orun-cloud/specs/epics/saas-orun-backend-merge/coordination-api.md \
          specs/orun-native-coordination/vendored/coordination-api.md

2. Recompute and record the checksum:

       sha256sum specs/orun-native-coordination/vendored/coordination-api.md
       # paste "<sha256>  coordination-api.md" into CHECKSUM

3. Update `internal/remotestate` (and tests) for the contract change, run
   `go test ./...`, and commit the re-vendor together with the client change so
   the diff documents the contract delta.

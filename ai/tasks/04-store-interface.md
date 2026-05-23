# PR 4: Generic Store Interface + In-Memory Implementation

**Phase:** 4 (from implementation plan)
**Size:** Small — ~3 files

## Goal

Define the `Store` interface (Upload, List, Download) and provide an in-memory implementation for unit testing.

## Files to create

### `internal/artifactstore/store.go`
- `Store` interface with `Upload`, `List`, `Download`
- `RemoteShard`, `ListOptions`, `UploadResult`, `DownloadedShard` types

### `internal/artifactstore/memory/memory.go`
- `InMemoryStore` implementing `Store`
- Backed by an in-memory map
- Useful for unit tests

### `internal/artifactstore/store_test.go`
- Round-trip with in-memory store

## Dependencies
- PR 1 (runbundle types)
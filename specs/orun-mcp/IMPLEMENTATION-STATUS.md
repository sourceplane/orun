# orun-mcp — Implementation Status (as-built)

| ID | Milestone | Status | As-built |
|----|-----------|--------|----------|
| UM0 | Provider composition (`internal/mcpserve`) | ✅ Done | The stdio JSON-RPC 2.0 loop (16 MiB scanner, initialize/ping/tools handling, notification silence, -32700/-32601/-32602 codes) moved verbatim from `internal/workmcp` into `internal/mcpserve`, which now composes `ToolProvider`s: merged `tools/list` in provider order, `tools/call` routed to the owning provider, unknown tool keeps the historical isError-result shape, duplicate tool names across providers fail loudly at serve start. serverInfo → `{name: "orun", version: <binary version>}` (was `orun-work`/`"1"`). `workmcp` keeps `WorkAPI` + the 9 tools and implements `ToolProvider`; `cmd/orun/mcp.go` serves the composite (work provider only until UM1). The WP-3/WP-10 forbidden-name sweep (`mcpserve.ForbiddenNameFragments`: status/pin/lifecycle) now runs over the composed roster in both suites; `./internal/mcpserve` + `./internal/workmcp` wired into `make test-state-redesign` (the PR CI gate) |
| UM1 | Native platform reads + manifest parity | 🗓️ Planned | — |
| UM2 | Platform writes + rails | 🗓️ Planned | — |
| UM3 | Docs + release | 🗓️ Planned | — |

---
title: orun preview
description: Run the repository's declared live preview — the dev server of the component intent.yaml names, after the components it needs — detached, with state and logs under .orun/preview/.
---

`orun preview` runs the repository's **live preview**: the dev server of the
component `intent.yaml` names in `preview.component`, after the previews of
the components it `needs`. The servers run detached, so the command returns
once they answer, and a coding session's `orun agent serve` keeps the same
preview running while the agent works (see [below](#in-a-coding-session)).

## Usage

```bash
orun preview [start]       # start it and wait until it answers
orun preview status        # is it still up?
orun preview stop          # stop every process of it
orun preview logs [component] [-f]
```

`orun preview` alone is `orun preview start`. Each command finds the
repository the way the others do: the nearest `intent.yaml` up from the
working directory, or `--intent`.

## Declaring a preview

Each component that can run itself says how in its `component.yaml`:

```yaml
spec:
  preview:
    run: ["pnpm", "--filter", "web", "dev", "--host", "0.0.0.0", "--port", "4173"]
    port: 4173
    needs: [api-edge]   # components whose own previews start first
    path: /             # the page the preview opens; default /
    env:                # optional: plain, non-secret env for the server
      VITE_API_URL: http://127.0.0.1:8787
```

The intent names the one to show:

```yaml
preview:
  component: web
```

| Field | Meaning |
| --- | --- |
| `run` | The server's argv, run from the component's directory. Not a shell line: `["pnpm", "dev"]`, not `"pnpm dev"`. |
| `port` | Where it listens: an integer from 1024 to 65535. It must answer on `127.0.0.1`, so bind `0.0.0.0` or `127.0.0.1`, not only `localhost` (which may be IPv6 only). |
| `needs` | Components whose previews start first, each of which declares one. No cycles. |
| `path` | The page the preview opens. Starts with `/`; default `/`. |
| `env` | Extra variables for the server. Never put a secret here: it is in the repository. |

[`orun validate`](./orun-validate.md) checks every declaration: `run`, `port`
and `path`, that each of `needs` exists and declares a preview, that there is
no cycle, and that `preview.component` names a component that declares one.
`orun preview` refuses a bad declaration with the same messages. The catalog
knows the block, so it raises no `component.field.unknown` warning; a bad port
or path there is a `component.spec.preview.invalid` warning.

## `start`

`start` runs the components in dependency order, `needs` first. Each one:

- runs from its component's directory, detached in a session of its own;
- gets the checkout's environment **without its secrets**: `ORUN_SESSION_TOKEN`,
  `ORUN_TOKEN`, `ORUN_TOKEN_FILE`, `GITHUB_TOKEN`, every `ANTHROPIC_*`,
  `OPENROUTER_*` and `GH_*` variable, and any name ending in `_TOKEN`, `_KEY`,
  `_SECRET`, `_PASSWORD` or `_CREDENTIALS` are removed;
- gets `PORT` set to its port (unless its `env` sets `PORT`), then its `env`;
- logs to `.orun/preview/<component>.log`.

`start` waits for each port to accept connections on `127.0.0.1` before
starting the next, up to `--timeout` each (default `2m`), then writes
`.orun/preview/state.json`:

```json
{
  "component": "web",
  "port": 4173,
  "path": "/",
  "state": "ready",
  "processes": [
    { "component": "api-edge", "pid": 4120, "port": 8787 },
    { "component": "web", "pid": 4131, "port": 4173 }
  ],
  "startedAt": "2026-11-04T09:12:03Z"
}
```

`state` is `starting`, `ready`, `failed` or `stopped`; a failed preview also
has a one-line `detail`, for example
`web exited (exit status 1): Error: Cannot find module 'vite'`. If a server
exits before its port answers, or the timeout passes, or a port is already
taken by something else, `start` stops what it started, records `failed` and
exits `1`.

`start` is idempotent: a preview already running healthy for the same
declaration is left alone. One that died, or that runs another declaration,
is stopped and started again.

| Flag | Meaning |
| --- | --- |
| `--timeout <duration>` | How long to wait for each component's port (default `2m0s`). |
| `--json` | Print the state as JSON. |

## `status`, `stop`, `logs`

`status` reads the state and re-checks it: a process that has exited makes it
`failed` (with the last line it logged), and one that is alive but no longer
answers makes it `starting`. `--json` prints the state.

`stop` ends every process group of the preview, the shown component first
(`SIGTERM`, then `SIGKILL` after five seconds), and records `stopped`.

`logs` prints a component's log, the shown component's by default; `-f`
keeps printing as the server writes, across restarts.

## In a coding session

`orun agent serve` keeps the preview running for a grounded coding session
(not for a baseline build). It re-reads the checkout's declaration every 30
seconds and whenever a turn ends, so a session that starts before its
repository has code starts the preview once the code lands. It restarts a
server that exits, with backoff, and gives up after three failed starts in a
row until the declaration changes or the next turn ends. A preview someone
stopped by hand stays stopped until the next turn ends. Each change is a
`preview_changed` session event:

```json
{ "state": "ready", "port": 4173, "component": "web", "path": "/" }
```

`starting` when it launches, `ready` once the port answers, `failed` with a
`detail` when it cannot come up, and `stopped` when the session ends. The
platform serves that port of the box as the session's preview. The dev
server's own reload picks up the agent's edits. `ORUN_PREVIEW=off` turns
this off.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success, including `status` and `stop` with no preview started. |
| `1` | No preview declared, a bad declaration, or a preview that did not come up. |

## Related

- [`orun agent serve`](./orun-agent.md#serve) — the session that keeps it running
- [`orun validate`](./orun-validate.md) — checks the declarations
- [Configuration](../reference/configuration.md#live-preview) — `spec.preview` and `preview`

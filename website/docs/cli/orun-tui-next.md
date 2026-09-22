---
title: orun tui-next
description: The next-generation cockpit (preview) — Home, Agents, Activity, Catalog, and Events over the same state store as the console, stream-driven and frame-stable.
---

`orun tui-next` launches **cockpit v2**: a ground-up rebuild of the
terminal cockpit as the terminal head of orun cloud. It shows the same
surfaces the console shows — **Home · Agents · Activity · Catalog · Events**
— over the same state store, driven by streams (a watch on `.orun` refs,
step-level run events, the agent attach protocol, cloud feeds when signed
in) rather than polling, and renders into fixed-size regions so frames are
stable by construction. It is local-first: every surface reads the
workspace's object graph, and gains its cloud lane when you are
authenticated. Being online is a status, not a mode.

```bash
orun tui-next          # the cockpit v2
orun tui --next        # the same thing
orun agent --next      # opened on the Agents surface
ORUN_TUI=next orun     # opt in for bare orun, orun tui, and orun agent
```

The command takes no flags of its own beyond the global `-i, --intent`,
`-c, --config-dir`, and `--all`.

## Preview status

Cockpit v2 shipped as the default once and was **rolled back to opt-in**
while it soaks. [`orun tui`](./orun-tui.md) remains the default cockpit;
v2 is reached only through the spellings above. It becomes the default
after the remaining cloud lanes land and a release has soaked with the flip
in place. Expect the surfaces to be complete for local work and the cloud
lanes to be the part still moving.

## Surfaces

| Key | Surface | What it shows |
|---|---|---|
| `1` | Home | The workspace at a glance |
| `2` | Agents | Sessions — attach to a live one, start a new one |
| `3` | Activity | Runs, their steps, and their logs (what the v1 run dashboard, log explorer, and history showed) |
| `4` | Catalog | Components and the compose flow that generates a plan |
| `5` | Events | The event stream |

A design-system gallery is registered as a further surface for development.

## Keys

The interaction grammar is Claude Code's: calm chrome, one escape hatch,
and `esc` that always goes back.

| Key | Effect |
|---|---|
| `1`–`5` | Switch surface |
| `:` or `ctrl+k` | Command palette — every action lives there |
| `?` | Help, generated from the command registry |
| `,` | Settings |
| `esc` | Back or dismiss; never quits |
| `ctrl+c` twice | Quit |

On the Agents surface, `enter` attaches to the selected session and `n`
starts a new one. Inside a session the conversation is composer-first:

| Key | Effect |
|---|---|
| `enter` | Send a steer |
| `esc` | Interrupt the current turn |
| `ctrl+d` | Detach; the session keeps running |
| `ctrl+y` / `ctrl+n` | Approve / deny a pending approval |
| `ctrl+o` | Expand tool cards |

These are the same actions the line-mode head exposes as `/approve`,
`/deny`, `/interrupt`, and `/detach` (see
[`orun agent attach`](./orun-agent.md#sessions-ps-attach-kill)); both heads
speak one attach protocol.

## Related

- [`orun tui`](./orun-tui.md) — the current default cockpit
- [Cockpit overview](../cockpit/overview.md)
- [`orun agent`](./orun-agent.md)

# Software factory

Point an AI agent at a product idea, walk away, come back to a live product.

That is the short version. The longer version is that `orun` already knows
how to build a whole SaaS from a baseline, how to record work as epics and
tasks, and how to land every change with its provenance attached. What was
missing was something that drives all of it from a one-paragraph brief. The
[`software-factory` skill](skills/software-factory/SKILL.md) is that
something. It works with Claude Code today and with any agent that reads a
`SKILL.md`.

## What you get

Give the skill an idea and a few facts (a name, a domain, a GitHub org, a
Cloudflare token). It will:

1. **Write the epic first.** A real one: thesis, design, milestone plan with
   "done when" criteria, risks, and an as-built status file, from the
   templates in [`skills/software-factory/templates/`](skills/software-factory/templates/).
   It shows you the milestone table and waits. That is the only time it stops.
2. **Create the workspace** on Orunbase, connect Cloudflare, and confirm the
   baseline is buildable.
3. **Bootstrap the baseline.** `cirrus` by default: twelve Workers behind one
   edge API, a Next.js console, D1 and KV, migrations, and CI, live on `stage`
   and `prod` in about an hour. Every phase lands as a pull request.
4. **Make the work visible.** The epic and its milestones go into the task
   plane; the design documents are pushed with `orun spec push`; the
   baseline's own bootstrap epic is already there.
5. **Build the milestones.** One task, one `orun/<KEY>-<slug>` branch, one
   pull request landed with `orun pr land`, per change. The commit trailer,
   the branch grammar, and the task contract are checked before the PR
   exists, not after.
6. **Report** what is live, what landed, what it fixed along the way, and
   what is left.

When something breaks, it fixes it and keeps going. It keeps a notes file so
you can see what happened.

## Before you start

You need an [Orunbase](https://orunbase.com) account (free is fine; that is
the hosted control plane `orun` talks to for workspaces, secrets, and the
baseline registry), the `orun` CLI at v2.58.15 or later, `gh` logged in to an
org where you can create repositories, and a Cloudflare account on the
Workers Paid plan with an API token that can write Workers, KV, and D1.

```bash
curl -fsSL https://raw.githubusercontent.com/sourceplane/orun/main/install.sh | sh
orun auth login
```

Install the Orunbase GitHub App on your org from the console's Integrations
page. The skill checks all of this before it spends any money, and tells you
exactly what is missing if something is.

## Run it

Install the skill where your agent looks for skills. For Claude Code that is
either your user directory or the repository you are working in:

```bash
git clone https://github.com/sourceplane/orun.git
cp -r orun/skills/software-factory ~/.claude/skills/        # or ./.claude/skills/ in a project
```

Then give it the brief:

```text
/software-factory

Build "Acme Cloud": a scheduling tool for small clinics. Practices sign up,
add staff, and publish a booking page; patients book without an account.
First features: practice + staff management, a public booking page, email
confirmations. Repo acme under github.com/acme, domain acme.dev,
Cloudflare subdomain acme-7be. Use cirrus. I'll paste the Cloudflare token
when you ask.
```

The agent will ask for the token on its own terms (it reads it from a file
it deletes afterwards, never from the chat or a command line), show you the
milestone table, and go. Expect the first checkpoint within a few minutes and
a live product within about an hour and a half.

## Doing it by hand

Everything the skill does is plain CLI. If you would rather drive, or you
want to understand what it is doing, the walkthrough is on the docs site:
[Create a workspace and build it from a baseline](https://orun-docs.pages.dev/examples/bootstrap-a-product-from-a-baseline),
with the [`orun baseline`](https://orun-docs.pages.dev/cli/orun-baseline),
[`orun task`](https://orun-docs.pages.dev/cli/orun-task), and
[`orun pr`](https://orun-docs.pages.dev/cli/orun-pr) references. The skill
file itself is readable top to bottom and doubles as the checklist.

## Things we learned the hard way

These are in the skill's fix table too, but they are worth knowing before
you hit them.

- The baseline's hooks call a bare `orun`. A shell alias does not count. Put
  the binary on `PATH`.
- `orun workspace use` does not see a workspace you created a moment ago
  until the next `orun auth login`. `ORUN_WORKSPACE=ws_…` in the environment
  works immediately.
- The Cloudflare token needs D1 Write, not just Workers. You find out in
  phase 3, which is late.
- `orun spec push` reads what is committed at `HEAD`, not your working tree.
- A reused `Idempotency-Key` replays the earlier response, including a
  failure.

## Contributing

The skill lives at [`skills/software-factory/`](skills/software-factory/).
If a run fails on something the fix table does not cover, open a PR that
adds the row; that is how every row got there. Larger changes to the flow
follow [CONTRIBUTING.md](CONTRIBUTING.md).

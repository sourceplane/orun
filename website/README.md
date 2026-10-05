# orun documentation site

This directory is the Docusaurus source for the orun documentation, published
at <https://orun-docs.pages.dev>. Content lives under `docs/`; the sidebar is
`sidebars.js`; site settings are `docusaurus.config.js`.

The docs are organised around orun's line, **platform discipline as code**, and
its four pillars: 1 · Declare, 2 · Package & evolve, 3 · Ground agents, and
4 · Verify → plan → execute. The sidebar groups pages by pillar; file paths, and
so URLs, do not move when a page changes group. The design of this structure is
the epic in [`specs/orun-docs-discipline/`](../specs/orun-docs-discipline/).

## Local development

```bash
cd website
npm ci
npm run docs:start
```

## Production build

```bash
cd website
npm ci
npm run docs:build     # writes docs-build/; broken links fail the build
npm run docs:serve
```

## Deploy

Deployment is a manual Cloudflare Pages publish; there is no CI workflow for
the site yet.

```bash
cd website
npm ci
npm run docs:build
wrangler login
wrangler pages deploy docs-build --project-name orun-docs
```

## Conventions

- One page per CLI command under `docs/cli/`, named `orun-<command>.md`, and
  listed in `sidebars.js` under its pillar.
- Open a concept page with why it matters for platform discipline, then the
  mechanics. State only what the code does: when a declared rule is not
  enforced, say so and link `concepts/standards.md`.
- Frontmatter carries `title` and a one-sentence `description`. The title is
  the H1; do not repeat it in the body.
- Write "Orunbase" for the hosted control plane and `orun` for the engine.
- Add a page under `docs/release-notes/` for every release with
  user-visible changes and put it first in the release-notes sidebar list. The
  `Releases` navbar link points at the GitHub releases page and needs no update.
- `.docs-last-version` is being retired in favour of the release notes; if you
  touch it, keep its version current.

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the documentation policy.

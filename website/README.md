# orun documentation site

This directory is the Docusaurus source for the orun documentation, published
at <https://orun-docs.pages.dev>. Content lives under `docs/`; the sidebar is
`sidebars.js`; site settings are `docusaurus.config.js`.

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
  listed in `sidebars.js`.
- Frontmatter carries `title` and a one-sentence `description`. The title is
  the H1; do not repeat it in the body.
- Write "Orunbase" for the hosted control plane and `orun` for the engine.
- Add a page under `docs/release-notes/` for every minor release and put it
  first in the release-notes sidebar list; update the `Releases` navbar link
  in `docusaurus.config.js`.
- `.docs-last-version` records the CLI version the docs were last checked
  against.

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the documentation policy.

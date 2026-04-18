---
title: Deploying docs
---

The `arx` documentation site is a docs-only Docusaurus project under `website/`. The production target is Cloudflare Pages.

## Local validation

Install dependencies and run the local dev server:

```bash
cd website
npm install
npm run docs:start
```

Create a production build:

```bash
cd website
npm ci
npm run docs:build
npm run docs:serve
```

The static site output is written to `website/docs-build/`.

## Manual Cloudflare Pages deploy

```bash
cd website
npm ci
npm run docs:build
wrangler login
wrangler pages deploy docs-build --project-name arx-docs
```

Replace `arx-docs` if your Cloudflare Pages project name differs.

## Deployment notes

- the site routes docs at the root path
- broken links fail the build
- `docs-build/` is generated output, not source of truth
- update `docusaurus.config.js` if the public site URL changes
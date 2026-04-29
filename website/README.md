# orun website

This directory contains the Docusaurus-based documentation site for `orun`.

## Local development

```bash
cd website
npm install
npm run docs:start
```

## Static build

```bash
cd website
npm ci
npm run docs:build
npm run docs:serve
```

## Manual Cloudflare Pages deploy

```bash
cd website
npm ci
npm run docs:build
wrangler login
wrangler pages deploy docs-build --project-name orun-docs
```

Replace `orun-docs` with the actual Cloudflare Pages project name if it differs.
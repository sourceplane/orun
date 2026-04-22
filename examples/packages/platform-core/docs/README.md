# platform-core

This package exports the packaged `charts`, `helm`, `helmCommon`, and `terraform` compositions used by the repository quick start.

Build a portable archive with:

```bash
gluon compositions package build \
  --root examples/packages/platform-core \
  --output dist/platform-core-1.0.0.tgz
```

Then point an intent at the directory, archive, or OCI reference through `intent.compositions.sources`.

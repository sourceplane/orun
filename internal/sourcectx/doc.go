// Package sourcectx is the source-context resolver layer that produces a
// SourceSnapshot for the workspace at refresh time. C0 ships only the
// pure type+key+hash skeleton; the git/FS-backed Resolver lands in C1.
//
// The split keeps the catalogmodel "pure data" boundary clean while giving
// downstream packages (catalogresolve, catalogstore) a stable type to import
// from now.
//
// References:
//   - specs/archive/orun-component-catalog/identity-and-keys.md §2 (SourceSnapshot
//     key construction), §7 (dirtyHash inputs), §8 (catalogInputHash).
//   - specs/archive/orun-component-catalog/data-model.md §1 (SourceSnapshot shape).
package sourcectx

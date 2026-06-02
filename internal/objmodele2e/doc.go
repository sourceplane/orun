// Package objmodele2e holds the end-to-end integration test for the
// orun-object-model (specs/orun-object-model test-plan.md §4, §6): a full
// plan → seal → index → fsck → push → pull → gc walk across every layer, plus
// the dedup / disk-win assertion that proves content addressing + sharing beats
// copying. It has no non-test API.
package objmodele2e

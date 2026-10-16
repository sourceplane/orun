package main

import (
	"fmt"
	"sort"
	"strings"

	compositionpkg "github.com/sourceplane/orun/internal/composition"
	"github.com/sourceplane/orun/internal/loader"
	"github.com/sourceplane/orun/internal/model"
)

func pullCompositions() error {
	return resolveAndCacheCompositions(true)
}

func lockCompositions(writeIntent, check bool) error {
	if check {
		return checkCompositionLock()
	}
	registry, err := resolveAndCacheCompositionsRegistry(true)
	if err != nil {
		return err
	}
	if !writeIntent {
		return nil
	}

	changes, err := compositionpkg.PinIntentDigests(intentFile, registry.Sources)
	if err != nil {
		return err
	}
	pinned := 0
	for _, change := range changes {
		if change.Changed() {
			pinned++
		}
	}
	if pinned == 0 {
		fmt.Printf("\n✓ %s already pins every composition source\n", intentFile)
		return nil
	}
	fmt.Printf("\n✓ Pinned %d composition source digest(s) in %s:\n", pinned, intentFile)
	for _, change := range changes {
		if !change.Changed() {
			continue
		}
		if change.OldDigest == "" {
			fmt.Printf("  - %s: %s\n", change.Name, change.NewDigest)
		} else {
			fmt.Printf("  - %s: %s -> %s\n", change.Name, change.OldDigest, change.NewDigest)
		}
	}
	return nil
}

// checkCompositionLock resolves sources without writing anything and fails when
// an unpinned source no longer matches the recorded lock. Pinned sources are
// already enforced by resolution itself.
func checkCompositionLock() error {
	lock, err := compositionpkg.ReadLockFile(intentFile)
	if err != nil {
		return err
	}

	intent, err := loader.LoadIntent(intentFile)
	if err != nil {
		return fmt.Errorf("failed to load intent: %w", err)
	}
	registry, err := loader.LoadCompositionsForIntent(intent, intentFile, configDir)
	if err != nil {
		return fmt.Errorf("failed to resolve compositions: %w", err)
	}

	drift, unlocked := compositionpkg.CheckLockDrift(intent.Compositions.Sources, registry.Sources, lock)
	for _, name := range unlocked {
		fmt.Printf("⚠ composition source %s is unpinned and not in the lock\n", name)
	}
	if len(drift) == 0 {
		fmt.Println("✓ Composition sources match the lock")
		return nil
	}
	for _, d := range drift {
		fmt.Printf("✗ composition source %s drifted: locked %s, resolves to %s\n", d.Name, d.LockedDigest, d.Digest)
	}
	return fmt.Errorf("%d composition source(s) drifted from %s; run 'orun compositions lock' to accept, or pin with --write-intent", len(drift), loaderPathForLock(intentFile))
}

func resolveAndCacheCompositions(writeLock bool) error {
	_, err := resolveAndCacheCompositionsRegistry(writeLock)
	return err
}

func resolveAndCacheCompositionsRegistry(writeLock bool) (*loader.CompositionRegistry, error) {
	fmt.Println("□ Loading intent...")
	intent, _, err := loadResolvedIntentFile(intentFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load intent: %w", err)
	}

	fmt.Println("□ Resolving compositions...")
	registry, err := loader.LoadCompositionsForIntent(intent, intentFile, configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve compositions: %w", err)
	}

	if writeLock {
		if err := loader.WriteCompositionLockFile(intentFile, registry.Sources); err != nil {
			return nil, err
		}
		fmt.Printf("✓ Wrote composition lock: %s\n", loaderPathForLock(intentFile))
	}

	printResolvedSourceSummary(registry.Sources)
	return registry, nil
}

func buildCompositionPackage() error {
	if strings.TrimSpace(compositionPackageRoot) == "" {
		return fmt.Errorf("--root is required")
	}
	if strings.TrimSpace(compositionPackageOutput) == "" {
		return fmt.Errorf("--output is required")
	}

	fmt.Println("□ Building composition package archive...")
	if err := compositionpkg.BuildPackageArchive(compositionPackageRoot, compositionPackageOutput); err != nil {
		return err
	}
	fmt.Printf("✓ Package archive written: %s\n", compositionPackageOutput)
	return nil
}

func pushCompositionPackage(archivePath, ref string) error {
	fmt.Println("□ Publishing composition package...")
	if err := compositionpkg.PushArchiveFile(archivePath, ref); err != nil {
		return err
	}
	fmt.Printf("✓ Package published: %s\n", ref)
	return nil
}

func loaderPathForLock(intentPath string) string {
	return compositionpkg.LockFilePath(intentPath)
}

func printResolvedSourceSummary(sources []model.ResolvedCompositionSource) {
	if len(sources) == 0 {
		return
	}

	sorted := append([]model.ResolvedCompositionSource(nil), sources...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	fmt.Println("\nResolved sources:")
	for _, source := range sorted {
		location := source.Ref
		if location == "" {
			location = source.Path
		}
		fmt.Printf("  - %s (%s) %s\n", source.Name, source.Kind, location)
		fmt.Printf("    digest: %s\n", source.ResolvedDigest)
		if len(source.Exports) > 0 {
			fmt.Printf("    exports: %s\n", strings.Join(source.Exports, ", "))
		}
	}
}

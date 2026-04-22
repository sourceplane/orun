package main

import (
	"fmt"
	"sort"
	"strings"

	compositionpkg "github.com/sourceplane/gluon/internal/composition"
	"github.com/sourceplane/gluon/internal/loader"
	"github.com/sourceplane/gluon/internal/model"
)

func pullCompositions() error {
	return resolveAndCacheCompositions(true)
}

func lockCompositions() error {
	return resolveAndCacheCompositions(true)
}

func resolveAndCacheCompositions(writeLock bool) error {
	fmt.Println("□ Loading intent...")
	intent, _, err := loadResolvedIntentFile(intentFile)
	if err != nil {
		return fmt.Errorf("failed to load intent: %w", err)
	}

	fmt.Println("□ Resolving compositions...")
	registry, err := loader.LoadCompositionsForIntent(intent, intentFile, configDir)
	if err != nil {
		return fmt.Errorf("failed to resolve compositions: %w", err)
	}

	if writeLock {
		if err := loader.WriteCompositionLockFile(intentFile, registry.Sources); err != nil {
			return err
		}
		fmt.Printf("✓ Wrote composition lock: %s\n", loaderPathForLock(intentFile))
	}

	printResolvedSourceSummary(registry.Sources)
	return nil
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
	if err := compositionpkg.PushPackageArchive(archivePath, ref); err != nil {
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

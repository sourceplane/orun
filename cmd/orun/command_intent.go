package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/sourceplane/orun/internal/loader"
	"github.com/sourceplane/orun/internal/preset"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	intentRenderOutput string
)

var intentCmd = &cobra.Command{
	Use:   "intent",
	Short: "Inspect and render the effective intent",
}

var intentExplainCmd = &cobra.Command{
	Use:   "explain",
	Short: "Show what each preset contributes to the effective intent",
	RunE: func(cmd *cobra.Command, args []string) error {
		return explainIntent()
	},
}

var intentRenderCmd = &cobra.Command{
	Use:   "render",
	Short: "Render the fully-merged effective intent to stdout or a file",
	RunE: func(cmd *cobra.Command, args []string) error {
		return renderIntent()
	},
}

func registerIntentCommand(parent *cobra.Command) {
	intentRenderCmd.Flags().StringVarP(&intentRenderOutput, "output", "o", "", "Output file path (defaults to stdout)")
	intentCmd.AddCommand(intentExplainCmd)
	intentCmd.AddCommand(intentRenderCmd)
	parent.AddCommand(intentCmd)
}

func explainIntent() error {
	intent, _, err := loadResolvedIntentFile(intentFile)
	if err != nil {
		return fmt.Errorf("failed to load intent: %w", err)
	}

	if len(intent.Extends) == 0 {
		fmt.Println("No presets applied. The intent is used as-is.")
		return nil
	}

	compositionRegistry, err := loader.LoadCompositionsForIntent(intent, intentFile, configDir)
	if err != nil {
		return fmt.Errorf("failed to resolve compositions: %w", err)
	}

	if err := preset.ValidateExtendsRefs(intent); err != nil {
		return err
	}

	resolvedPresets, err := preset.LoadPresetsForIntent(intent, compositionRegistry.SourceRoots)
	if err != nil {
		return fmt.Errorf("failed to load intent presets: %w", err)
	}

	mergeResult, err := preset.MergePresets(intent, resolvedPresets)
	if err != nil {
		return fmt.Errorf("failed to merge intent presets: %w", err)
	}

	color := ui.ColorEnabledForWriter(os.Stdout)

	fmt.Printf("\n%s\n\n", ui.Bold(color, "Intent Presets"))

	for _, rp := range resolvedPresets {
		fmt.Printf("  %s %s:%s\n", ui.Cyan(color, "●"), rp.Provenance.Source, rp.Provenance.Preset)
		fmt.Printf("    %s\n", ui.Dim(color, rp.Preset.Metadata.Name))
	}

	fmt.Printf("\n%s\n\n", ui.Bold(color, "Contributions"))

	// Group provenance by preset
	byPreset := make(map[string][]string)
	for field, provs := range mergeResult.Provenance {
		for _, p := range provs {
			key := p.Source + ":" + p.Preset
			byPreset[key] = append(byPreset[key], field)
		}
	}

	presetKeys := make([]string, 0, len(byPreset))
	for k := range byPreset {
		presetKeys = append(presetKeys, k)
	}
	sort.Strings(presetKeys)

	for _, key := range presetKeys {
		fields := byPreset[key]
		sort.Strings(fields)
		fmt.Printf("  %s\n", ui.Bold(color, key))
		for _, field := range fields {
			fmt.Printf("    %s %s\n", ui.Dim(color, "├─"), field)
		}
		fmt.Println()
	}

	return nil
}

func renderIntent() error {
	intent, _, err := loadResolvedIntentFile(intentFile)
	if err != nil {
		return fmt.Errorf("failed to load intent: %w", err)
	}

	if len(intent.Extends) > 0 {
		compositionRegistry, err := loader.LoadCompositionsForIntent(intent, intentFile, configDir)
		if err != nil {
			return fmt.Errorf("failed to resolve compositions: %w", err)
		}

		if err := preset.ValidateExtendsRefs(intent); err != nil {
			return err
		}

		resolvedPresets, err := preset.LoadPresetsForIntent(intent, compositionRegistry.SourceRoots)
		if err != nil {
			return fmt.Errorf("failed to load intent presets: %w", err)
		}

		mergeResult, err := preset.MergePresets(intent, resolvedPresets)
		if err != nil {
			return fmt.Errorf("failed to merge intent presets: %w", err)
		}
		intent = mergeResult.Intent
	}

	data, err := yaml.Marshal(intent)
	if err != nil {
		return fmt.Errorf("failed to marshal effective intent: %w", err)
	}

	header := "# Effective intent (presets merged)\n"
	if len(intent.Extends) == 0 {
		header = ""
	}

	output := header + string(data)

	if intentRenderOutput != "" {
		if err := os.WriteFile(intentRenderOutput, []byte(output), 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
		fmt.Printf("Effective intent written to %s\n", intentRenderOutput)
	} else {
		fmt.Print(output)
	}

	return nil
}


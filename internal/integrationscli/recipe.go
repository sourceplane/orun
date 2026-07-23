package integrationscli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewRecipeCommand builds the `recipe` native extension for a provider: it
// prints the connect token recipe (intro, permission items, links) from the
// CACHED descriptor, for air-gapped connect prep — no network, no auth. The
// provider name is injected by the cmd/orun wiring; this package stays
// catalog-free.
func NewRecipeCommand(provider string, load func() *CachedRegistry) *cobra.Command {
	return &cobra.Command{
		Use:   "recipe",
		Short: "Print the " + provider + " connect token recipe from the cached registry (air-gapped prep)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cache := load()
			if cache == nil {
				return fmt.Errorf("no cached integration registry; run `orun integrations sync` first (the recipe is served by the registry)")
			}
			d := cache.Descriptor(provider)
			if d == nil {
				return fmt.Errorf("provider %q is not in the cached registry; run `orun integrations sync` to refresh", provider)
			}
			recipe := d.ConnectRecipe()
			if recipe == nil {
				return fmt.Errorf("provider %q declares no connect recipe in the cached registry", provider)
			}
			out := cmd.OutOrStdout()
			if recipe.Intro != "" {
				fmt.Fprintln(out, recipe.Intro)
			}
			if len(recipe.Items) > 0 {
				fmt.Fprintln(out)
				fmt.Fprintln(out, "token permissions:")
				for _, item := range recipe.Items {
					if item.Why != "" {
						fmt.Fprintf(out, "  - %s: %s\n", item.Name, item.Why)
					} else {
						fmt.Fprintf(out, "  - %s\n", item.Name)
					}
				}
			}
			if len(recipe.Links) > 0 {
				fmt.Fprintln(out)
				fmt.Fprintln(out, "links:")
				for _, link := range recipe.Links {
					fmt.Fprintf(out, "  - %s: %s\n", link.Label, link.URL)
				}
			}
			return nil
		},
	}
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"

	"github.com/sourceplane/orun/internal/scaffold"
)

// `orun baseline new --local` — build a registered baseline from its own
// source, on this machine (orun-bootstrap-engine BE-O7b).
//
// # What this verb is
//
// Three facts, composed: the registry says WHERE a baseline lives (repo) and
// WHICH COMMIT is published (tag); its manifest says WHICH DOCUMENT inside
// that tree is the build; and `orun new` places that document's phases. Before
// this, an operator held all three and had to join them by hand — clone at the
// tag, know the manifest's path, read a key out of it, then invoke `orun new`
// with the right file. Every step of that is a fact the platform already
// publishes, which is why the join belongs here.
//
// # Why the manifest and not a convention
//
// `blueprint.yaml` is what every registered row but one names, and
// `stratus-coolify` is that one: two rows over a single tree, each with its own
// manifest. A convention would serve the Azure contract to a Coolify build —
// a substitution that has actually happened on the platform side, with the
// agent brief. So the path comes from the row (orun-cloud BE-K1f) and the
// build document comes from the manifest at that path (BE-K1e).

// buildDocument reads `spec.bootstrap.blueprint` out of a baseline manifest and
// returns it as a repo-relative path.
//
// This deliberately reads ONE key rather than parsing the manifest, because
// this binary is not the manifest's reader — orun-cloud's parser is, and a
// second implementation of a contract is a second thing to keep true. What it
// does duplicate is the SAFETY rule, because a path that escapes the checkout
// is not a contract question: this value names a file this process is about to
// read and place from, so it is validated here whatever anyone else did.
func buildDocument(manifest []byte, manifestPath string) (string, error) {
	var doc struct {
		Spec struct {
			Bootstrap struct {
				Blueprint string `yaml:"blueprint"`
			} `yaml:"bootstrap"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(manifest, &doc); err != nil {
		return "", fmt.Errorf("reading %s: %w", manifestPath, err)
	}
	rel := strings.TrimSpace(doc.Spec.Bootstrap.Blueprint)
	if rel == "" {
		return "", fmt.Errorf(
			"%s declares no spec.bootstrap.blueprint, so nothing names the document to place.\n"+
				"A baseline with a shell layer is run through its umbrella and is not buildable this way;\n"+
				"a blueprint-driven one must name its build document (orun-cloud BE-K1e)", manifestPath)
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("%s: spec.bootstrap.blueprint %q must be repo-relative", manifestPath, rel)
	}
	// Cleaned first, so `a/../../etc/passwd` is caught as the `..` it resolves
	// to rather than passing a prefix check on its literal text.
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s: spec.bootstrap.blueprint %q must not escape the repo", manifestPath, rel)
	}
	return clean, nil
}

// fetchBaselineSource clones a baseline's repo at the registry's pinned tag.
//
// A seam, not indirection: it is the one step of this verb that reaches the
// network, so a test can drive everything around it without one. The real
// implementation is orun's own git source resolver, so a baseline fetched here
// and a `kind: git` source fetched during placement come down the same code
// path — shallow, tag-or-branch, pinned.
var fetchBaselineSource = func(repo, ref, workDir string) (string, error) {
	return scaffold.FetchGitRef(repo, ref, workDir, "baseline")
}

// readBuildDocument joins a checkout to the build document inside it.
func readBuildDocument(checkout, manifestPath string) (string, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return "", fmt.Errorf(
			"the registry row names no manifestPath.\n" +
				"A platform older than orun-cloud BE-K1f does not serve it, and guessing the\n" +
				"convention is how a two-manifest baseline gets built from the wrong contract")
	}
	manifestAbs := filepath.Join(checkout, filepath.Clean(manifestPath))
	body, err := os.ReadFile(manifestAbs) //nolint:gosec // path is cleaned and joined under the checkout
	if err != nil {
		return "", fmt.Errorf("reading the manifest %s at the pinned tag: %w", manifestPath, err)
	}
	rel, err := buildDocument(body, manifestPath)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(checkout, rel)
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf(
			"%s names %s as the build document, and it is not in the tree at this tag: %w",
			manifestPath, rel, err)
	}
	return abs, nil
}

// newBaselineNewCommand builds a registered baseline from its own source.
//
// Only `--local` exists today, and it is required rather than defaulted: the
// other shape — a platform-run build — is a different operation with different
// failure modes and a different place to watch it, and a flag that silently
// picks one is a flag that surprises somebody at the worst moment. When
// `--via-platform` lands it will be the other explicit half.
func newBaselineNewCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		local      bool
		out        string
		sets       []string
		valuesFile string
		runHooks   bool
		resume     bool
		phase      string
		until      string
		progress   string
		keepWork   bool
	)
	cmd := &cobra.Command{
		Use:   "new <id[@tag]> --local --out <dir>",
		Short: "Build a registered baseline from its own source, on this machine",
		Long: `Build a registered baseline into a directory on this machine.

The registry says where the baseline lives and which commit is published; its
manifest says which document inside that tree is the build; and this places
that document's phases. Every one of those is a fact the platform publishes,
so joining them by hand — clone at the tag, find the manifest, read a key out
of it, invoke ` + "`orun new`" + ` — is work nobody should repeat.

The tag is the registry's, not yours: addressing ` + "`id@tag`" + ` that the registry has
since moved past RESOLVES to what it publishes now, and says so.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !local {
				return exitErr(2, "orun baseline new: --local is required.\n"+
					"A platform-run build (--via-platform) is not in this release: it needs a repo link id\n"+
					"the CLI cannot resolve yet. Until then this verb builds here, and says so.")
			}
			if strings.TrimSpace(out) == "" {
				return exitErr(2, "orun baseline new: --out is required — it is where the product is placed")
			}
			ctx := cmd.Context()
			view, err := resolveBlueprint(ctx, backendURL, workspace, args[0])
			if err != nil {
				return err
			}
			b := view.Blueprint

			// READINESS IS A GATE, not a warning. A bootstrap that starts
			// without its providers does not fail at the door; it fails thirty
			// minutes in, having created a repo and half a product, and the
			// operator reads a Cloudflare error instead of "you never connected
			// Cloudflare". `baseline check` exists to ask this in advance, and
			// this asks it again because the answer can change in between.
			if !b.Readiness.IntegrationsReady {
				var missing []string
				for _, p := range b.Readiness.Integrations {
					if !p.Connected {
						missing = append(missing, p.Provider)
					}
				}
				return exitErr(1, "orun baseline new: %s cannot be built by this workspace yet — %s %s not connected.\n"+
					"Connect in the console, then re-run. Nothing has been written.",
					b.ID, strings.Join(missing, ", "), pluralize(len(missing), "is", "are"))
			}

			if view.PinnedTagStale {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"you asked for %s; the registry now publishes %s, and that is what this builds\n",
					view.PinnedTag, b.Tag)
			}

			work, err := os.MkdirTemp("", "orun-baseline-")
			if err != nil {
				return fmt.Errorf("orun baseline new: %w", err)
			}
			if keepWork {
				fmt.Fprintf(cmd.ErrOrStderr(), "baseline checkout kept at %s\n", work)
			} else {
				defer func() { _ = os.RemoveAll(work) }()
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "fetching %s@%s\n", b.SourceRepo, b.Tag)
			checkout, err := fetchBaselineSource(b.SourceRepo, b.Tag, work)
			if err != nil {
				return fmt.Errorf("orun baseline new: %w", err)
			}
			doc, err := readBuildDocument(checkout, b.ManifestPath)
			if err != nil {
				return fmt.Errorf("orun baseline new: %w", err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "building %s\n", filepath.Base(doc))

			// The SAME pipeline `orun new` runs. Not a parallel one: a second
			// path to place a blueprint is a second path to get placement
			// wrong, and every gate `orun new` carries — the two-parser output
			// check, the phase barrier, the requirement gate — is one this
			// build needs exactly as much.
			scaffoldBlueprint = doc
			scaffoldOut = out
			scaffoldSet = sets
			scaffoldValuesFile = valuesFile
			scaffoldRunHooks = runHooks
			scaffoldResume = resume
			scaffoldPhase = phase
			scaffoldUntil = until
			scaffoldProgress = progress
			scaffoldStatus = false
			scaffoldJSON = false
			return runScaffoldNew(ctx)
		},
	}
	cmd.Flags().BoolVar(&local, "local", false, "Build here, from a checkout of the baseline at the registry's tag (required)")
	cmd.Flags().StringVar(&out, "out", "", "Output directory for the product (required)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace to resolve the baseline for")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "Backend URL")
	cmd.Flags().StringArrayVar(&sets, "set", nil, "Set a blueprint input as key=value (repeatable)")
	cmd.Flags().StringVar(&valuesFile, "values", "", "Path to a YAML values file feeding blueprint inputs")
	cmd.Flags().BoolVar(&runHooks, "run-hooks", false, "Execute the phases' hooks — the difference between placing files and bootstrapping a product")
	cmd.Flags().BoolVar(&resume, "resume", false, "Place every phase not already derived as done")
	cmd.Flags().StringVar(&phase, "phase", "", "Place only this phase")
	cmd.Flags().StringVar(&until, "until", "", "Place every phase through this one")
	cmd.Flags().StringVar(&progress, "progress", "auto", "Progress rendering: auto | plain | verbose | json")
	cmd.Flags().BoolVar(&keepWork, "keep-checkout", false, "Keep the baseline checkout instead of removing it (debugging)")
	return cmd
}

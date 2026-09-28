package main

// changed_inputs_test.go locks spec.inputs (OR2) end to end: component.yaml →
// resolved catalog → the --changed engine. Root files owned by no component
// (the lockfile, turbo.json, a tooling tree) select the components that list
// them in spec.inputs, and the selection rides dependsOn input:true edges.

import "testing"

func TestChangedInputs_RootFilesSelectDeclaringComponents(t *testing.T) {
	root := writeWorkspace(t, map[string]string{
		"intent.yaml": `apiVersion: orun.io/v1alpha1
kind: Intent
metadata:
  name: inputs
catalog:
  namespace: ns
`,
		"apps/web/component.yaml": `apiVersion: orun.io/v1alpha1
kind: Component
metadata:
  name: web
spec:
  type: app
  inputs:
    - pnpm-lock.yaml
    - turbo.json
`,
		"packages/lint/component.yaml": `apiVersion: orun.io/v1alpha1
kind: Component
metadata:
  name: lint
spec:
  type: library
  inputs:
    - tooling/eslint/**
`,
		"apps/console/component.yaml": `apiVersion: orun.io/v1alpha1
kind: Component
metadata:
  name: console
spec:
  type: app
  dependsOn:
    - component: lint
      input: true
`,
		"apps/api/component.yaml": `apiVersion: orun.io/v1alpha1
kind: Component
metadata:
  name: api
spec:
  type: app
`,
		"apps/web/page.tsx":         "export {}\n",
		"apps/api/main.go":          "package main\n",
		"pnpm-lock.yaml":            "lockfileVersion: 9\n",
		"turbo.json":                "{}\n",
		"tooling/eslint/rules/x.js": "module.exports = {}\n",
		"tooling/prettier/index.js": "module.exports = {}\n",
	})
	expectSelection(t, root, []string{"pnpm-lock.yaml"}, []string{"web"})
	expectSelection(t, root, []string{"turbo.json"}, []string{"web"})
	// ** matches nested files; console rides its input edge onto lint.
	expectSelection(t, root, []string{"tooling/eslint/rules/x.js"}, []string{"console", "lint"})
	// Non-matching root files still select nothing.
	expectSelection(t, root, []string{"tooling/prettier/index.js"}, nil)
	// Path ownership is unchanged.
	expectSelection(t, root, []string{"apps/api/main.go"}, []string{"api"})
}

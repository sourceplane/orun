package executor

import (
	"fmt"
	"sort"
	"strings"
)

type factory func() Executor

var factories = map[string]factory{
	"docker": func() Executor {
		return &dockerExecutor{pulledImages: map[string]struct{}{}}
	},
	"github-actions": func() Executor {
		return &githubActionsExecutor{delegate: &localExecutor{}}
	},
	"local": func() Executor {
		return &localExecutor{}
	},
}

func Get(name string) (Executor, error) {
	normalized := NormalizeRunnerName(name)
	build, ok := factories[normalized]
	if !ok {
		return nil, fmt.Errorf("unsupported runner %q (supported: %s)", name, strings.Join(Supported(), ", "))
	}
	return build(), nil
}

func Supported() []string {
	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

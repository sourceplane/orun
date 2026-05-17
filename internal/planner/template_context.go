package planner

import "github.com/sourceplane/orun/internal/model"

// TemplateContext gathers all data needed to build the template dot map.
type TemplateContext struct {
	CompInst *model.ComponentInstance
	JobID    string
	JobName  string
}

// Build returns the map[string]interface{} used as dot in text/template.Execute.
func (tc *TemplateContext) Build() map[string]interface{} {
	ctx := make(map[string]interface{})

	// .orun namespace
	ctx["orun"] = map[string]interface{}{
		"component": map[string]interface{}{
			"name":   tc.CompInst.ComponentName,
			"type":   tc.CompInst.Type,
			"domain": tc.CompInst.Domain,
		},
		"environment": map[string]interface{}{
			"name": tc.CompInst.Environment,
		},
		"composition": map[string]interface{}{
			"type": tc.CompInst.Type,
		},
		"profile": map[string]interface{}{
			"name": tc.CompInst.ProfileName,
		},
		"job": map[string]interface{}{
			"id":   tc.JobID,
			"name": tc.JobName,
		},
	}

	// .parameters namespace
	params := make(map[string]interface{}, len(tc.CompInst.Parameters))
	for k, v := range tc.CompInst.Parameters {
		params[k] = v
	}
	ctx["parameters"] = params

	// .env namespace
	env := make(map[string]interface{}, len(tc.CompInst.Env))
	for k, v := range tc.CompInst.Env {
		env[k] = v
	}
	ctx["env"] = env

	return ctx
}

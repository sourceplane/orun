package actions

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// orun.http/probe@v1 — assert that declared URLs answer.
//
// This is the first action in the registry, and it is deliberately the
// smallest one that is genuinely useful: a baseline's verify step is "GET these
// URLs and expect this status", which needs nothing from the platform, no
// credential, and no code extracted from a cobra command. Shipping it first
// proves the whole mechanism — declaration, parse-time validation, resolution,
// outputs — against an action that actually runs.
//
// Redirects are NOT followed. A verify step that accepts a 301 to somewhere
// else is asserting that something answered, not that the right thing did.
func init() {
	register(Spec{
		ID:      "orun.http/probe@v1",
		Summary: "GET each URL and require the expected status",
		Params: []Param{
			{
				Name:        "urls",
				Type:        ParamStringList,
				Required:    true,
				Description: "absolute URLs to probe",
			},
			{
				Name:        "expectStatus",
				Type:        ParamInt,
				Default:     200,
				Description: "the status every URL must answer with",
			},
			{
				Name:        "timeoutSeconds",
				Type:        ParamInt,
				Default:     10,
				Description: "per-request timeout",
			},
		},
		Outputs: []string{"checked", "failures"},
	}, runHTTPProbe)
}

func runHTTPProbe(ctx context.Context, in Input) (Result, error) {
	urls := StringListParam(in, "urls")
	if len(urls) == 0 {
		return Result{}, fmt.Errorf("orun.http/probe@v1: urls is empty — nothing to verify")
	}
	want := IntParam(in, "expectStatus")
	timeout := time.Duration(IntParam(in, "timeoutSeconds")) * time.Second

	client := &http.Client{
		Timeout: timeout,
		// Do not follow redirects: see the type comment.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	var failures []string
	for _, raw := range urls {
		url := strings.TrimSpace(raw)
		if url == "" {
			continue
		}
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			failures = append(failures, fmt.Sprintf("%s: not an absolute http(s) URL", url))
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", url, err))
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", url, err))
			continue
		}
		// The body is never read — only the status is asserted — but it must be
		// closed or the connection leaks for the life of the process.
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			failures = append(failures, fmt.Sprintf("%s: got %d, want %d", url, resp.StatusCode, want))
		}
	}

	sort.Strings(failures)
	res := Result{Outputs: map[string]string{
		"checked":  fmt.Sprint(len(urls)),
		"failures": fmt.Sprint(len(failures)),
	}}
	if len(failures) > 0 {
		return res, fmt.Errorf("orun.http/probe@v1: %d of %d URL(s) did not answer as expected:\n  %s",
			len(failures), len(urls), strings.Join(failures, "\n  "))
	}
	return res, nil
}

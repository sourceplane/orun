package actions

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
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
		Doing:   "Checking that the deployed addresses answer.",
		Params: []Param{
			{
				Name:        "urls",
				Type:        ParamStringList,
				Description: "absolute URLs to probe",
			},
			{
				Name:        "targets",
				Type:        ParamObjectList,
				Description: "URLs to probe with what a reader calls them: {url, env, label, pin}; env defaults to the one the host names, pin to false for a health check",
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
		// urls is every address checked and failedUrls the ones that did not
		// answer as expected, one per line, in the order they were declared.
		Outputs: []string{"checked", "failures", "urls", "failedUrls"},
		Did:     probeDid,
		Facts:   probeFacts,
		Check: func(with map[string]any) error {
			if with["urls"] == nil && with["targets"] == nil {
				return fmt.Errorf("requires parameter \"urls\" or \"targets\" (the addresses to probe)")
			}
			return nil
		},
	}, runHTTPProbe)
}

// ProbeTarget is one address a probe checks, with what a reader calls it.
type ProbeTarget struct {
	URL   string
	Env   string
	Label string
	// Pin overrides whether a reader keeps it in view; nil decides by the
	// address (a health check is not pinned).
	Pin *bool
}

// ProbeTargets are a probe's addresses in declared order — `urls` first, then
// `targets` — each once.
func ProbeTargets(params map[string]any) []ProbeTarget {
	var out []ProbeTarget
	seen := map[string]bool{}
	add := func(t ProbeTarget) {
		t.URL = strings.TrimSpace(t.URL)
		if t.URL == "" || seen[t.URL] {
			return
		}
		seen[t.URL] = true
		out = append(out, t)
	}
	if items, ok := params["urls"].([]any); ok {
		for _, item := range items {
			if s, ok := item.(string); ok {
				add(ProbeTarget{URL: s})
			}
		}
	}
	if items, ok := params["targets"].([]any); ok {
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t := ProbeTarget{}
			t.URL, _ = m["url"].(string)
			t.Env, _ = m["env"].(string)
			t.Label, _ = m["label"].(string)
			t.Env, t.Label = strings.TrimSpace(t.Env), strings.TrimSpace(t.Label)
			switch pin := m["pin"].(type) {
			case bool:
				t.Pin = &pin
			case string:
				if b, err := strconv.ParseBool(strings.TrimSpace(pin)); err == nil {
					t.Pin = &b
				}
			}
			add(t)
		}
	}
	return out
}

func runHTTPProbe(ctx context.Context, in Input) (Result, error) {
	var urls []string
	for _, t := range ProbeTargets(in.Params) {
		urls = append(urls, t.URL)
	}
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

	var failures, failedURLs []string
	fail := func(url, format string, args ...any) {
		failures = append(failures, url+": "+fmt.Sprintf(format, args...))
		failedURLs = append(failedURLs, url)
	}
	for _, url := range urls {
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			fail(url, "not an absolute http(s) URL")
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			fail(url, "%v", err)
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			fail(url, "%v", err)
			continue
		}
		// The body is never read — only the status is asserted — but it must be
		// closed or the connection leaks for the life of the process.
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			fail(url, "got %d, want %d", resp.StatusCode, want)
		}
	}

	sort.Strings(failures)
	res := Result{Outputs: map[string]string{
		"checked":    fmt.Sprint(len(urls)),
		"failures":   fmt.Sprint(len(failures)),
		"urls":       strings.Join(urls, "\n"),
		"failedUrls": strings.Join(failedURLs, "\n"),
	}}
	if len(failures) > 0 {
		return res, fmt.Errorf("orun.http/probe@v1: %d of %d URL(s) did not answer as expected:\n  %s",
			len(failures), len(urls), strings.Join(failures, "\n  "))
	}
	return res, nil
}

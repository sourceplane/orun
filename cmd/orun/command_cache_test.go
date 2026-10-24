package main

import (
	"testing"
	"time"
)

func TestParseAgeAndSize(t *testing.T) {
	for in, want := range map[string]time.Duration{"30d": 30 * 24 * time.Hour, "12h": 12 * time.Hour, "90m": 90 * time.Minute, "1.5d": 36 * time.Hour} {
		got, err := parseAge(in)
		if err != nil || got != want {
			t.Fatalf("parseAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "soon", "-1d"} {
		if _, err := parseAge(bad); err == nil {
			t.Fatalf("parseAge(%q) accepted", bad)
		}
	}
	for in, want := range map[string]int64{"10G": 10 << 30, "500M": 500 << 20, "1.5G": 3 << 29, "64K": 64 << 10, "2GiB": 2 << 30, "123": 123} {
		got, err := parseSize(in)
		if err != nil || got != want {
			t.Fatalf("parseSize(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := parseSize("lots"); err == nil {
		t.Fatal("parseSize(lots) accepted")
	}
	if got := formatSize(1536); got != "1.5 KB" {
		t.Fatalf("formatSize(1536) = %q", got)
	}
	if got := formatSize(10 << 30); got != "10.0 GB" {
		t.Fatalf("formatSize(10G) = %q", got)
	}
}

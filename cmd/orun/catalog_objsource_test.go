package main

import "testing"

func TestObjCatalogRef(t *testing.T) {
	id := "sha256:" + repeatByte('a', 64)
	cases := []struct {
		name     string
		source   string
		snapshot string
		want     string
		wantErr  bool
	}{
		{"default", "", "", "catalogs/current", false},
		{"current", "current", "", "catalogs/current", false},
		{"latest maps to current", "latest", "", "catalogs/current", false},
		{"main", "main", "", "catalogs/main", false},
		{"branch", "branches/feature-x", "", "catalogs/branches/feature-x", false},
		{"pr", "prs/42", "", "catalogs/prs/42", false},
		{"bare id", id, "", id, false},
		{"snapshot pin wins", "main", id, id, false},
		{"empty branch invalid", "branches/", "", "", true},
		{"garbage invalid", "not-a-selector", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := objCatalogRef(tc.source, tc.snapshot)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("objCatalogRef(%q,%q) = %q, want %q", tc.source, tc.snapshot, got, tc.want)
			}
		})
	}
}

func repeatByte(b byte, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return string(out)
}

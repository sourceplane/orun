package views

import "testing"

func TestViewportWindow(t *testing.T) {
	cases := []struct {
		name               string
		cursor, total, max int
		wantStart, wantEnd int
	}{
		{"all rows fit", 0, 5, 10, 0, 5},
		{"cursor at top", 0, 100, 10, 0, 10},
		{"cursor inside window", 9, 100, 10, 0, 10},
		{"cursor scrolled", 10, 100, 10, 1, 11},
		{"cursor at bottom", 99, 100, 10, 90, 100},
		{"empty list", 0, 0, 10, 0, 0},
		{"stale cursor beyond total", 9, 3, 10, 0, 3},
		{"stale cursor beyond total, scrolled", 50, 3, 10, 3, 3},
		{"zero max clamps to one", 0, 5, 0, 0, 1},
	}
	for _, tc := range cases {
		start, end := viewportWindow(tc.cursor, tc.total, tc.max)
		if start != tc.wantStart || end != tc.wantEnd {
			t.Errorf("%s: viewportWindow(%d, %d, %d) = (%d, %d), want (%d, %d)",
				tc.name, tc.cursor, tc.total, tc.max, start, end, tc.wantStart, tc.wantEnd)
		}
		if start > end {
			t.Errorf("%s: start %d > end %d", tc.name, start, end)
		}
	}
}

package handlers

import "testing"

func TestValidGoal(t *testing.T) {
	cases := []struct {
		month, kind string
		target      int64
		ok          bool
	}{
		{"2026-11", "sales", 3_000_000, true},
		{"2026-11", "left", 1, true},
		{"2026-13", "sales", 100, false},
		{"2026-11", "clientes", 100, false},
		{"2026-11", "sales", 0, false},
		{"11-2026", "sales", 100, false},
	}
	for _, c := range cases {
		if got := ValidGoal(c.month, c.kind, c.target); got != c.ok {
			t.Fatalf("ValidGoal(%q,%q,%d) = %v", c.month, c.kind, c.target, got)
		}
	}
}

package main

import "testing"

func TestLabelOpenWhileClosed(t *testing.T) {
	tests := []struct {
		name    string
		label   string
		allowed string
		want    bool
	}{
		{"empty allow-list refuses", "global-gathering", "", false},
		{"listed label passes", "global-gathering", "global-gathering", true},
		{"several labels", "rightscon", "global-gathering rightscon", true},
		{"other label refused", "miniapp", "global-gathering", false},
		{"bot unknown label refused", "1_DaChtoJeEtoTakoe", "global-gathering", false},
		{"empty label refused even if listed", "", "global-gathering ", false},
		{"prefix is not a match", "global", "global-gathering", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := labelOpenWhileClosed(tt.label, tt.allowed); got != tt.want {
				t.Fatalf("labelOpenWhileClosed(%q, %q) = %v, want %v", tt.label, tt.allowed, got, tt.want)
			}
		})
	}
}

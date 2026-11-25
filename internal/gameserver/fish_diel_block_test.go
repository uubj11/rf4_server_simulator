package gameserver

import (
	"encoding/json"
	"os"
	"testing"
)

func TestFishDielBlockTableAndWeight(t *testing.T) {
	blocks := fishDielBlocked()
	if len(blocks) == 0 {
		t.Fatal("fish_diel_block table is empty")
	}
	raw, err := os.ReadFile("assets/fish_ecology_defs.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		FishKey string `json:"fish_key"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	defs := make(map[string]bool, len(rows))
	for _, row := range rows {
		defs[row.FishKey] = true
	}
	for key, halves := range blocks {
		if !defs[key] {
			t.Fatalf("diel block key not found in ecology defs: %s", key)
		}
		for half := range halves {
			if half != "day" && half != "night" {
				t.Fatalf("invalid half for %s: %s", key, half)
			}
		}
	}
	profile := fishEcologyProfile{FishKey: "burbot", DielCurve: "[1,1,1,1]"}
	if got := fishDielWeight(profile, 8); got > fishDielBlockedCap+1e-9 {
		t.Fatalf("blocked day weight = %g, want <= %g", got, fishDielBlockedCap)
	}
	if got := fishDielWeight(profile, 22); got < 0.99 {
		t.Fatalf("active night weight = %g, want 1", got)
	}
	profile = fishEcologyProfile{FishKey: "a.sleeper", DielCurve: "[1,1,1,1]"}
	if got := fishDielWeight(profile, 22); got > fishDielBlockedCap+1e-9 {
		t.Fatalf("blocked night weight = %g, want <= %g", got, fishDielBlockedCap)
	}
	if got := fishDielWeight(profile, 8); got < 0.99 {
		t.Fatalf("active day weight = %g, want 1", got)
	}
}

package admin

import (
	"encoding/json"
	"io/fs"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/logging"
	"rf4server/internal/store"
)

func TestEventRewardPresetEmbeddedAndValid(t *testing.T) {
	db, err := store.OpenWithStatic(filepath.Join(t.TempDir(), "player.db"), "../../server_data/static.sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(config.Admin{JWTSecret: "test"}, db, logging.Discard(), WebFS())
	for _, tc := range []struct {
		file       string
		branches   []string
		lastStages []int
	}{
		{"anniversary-2024.json", []string{"11", "12"}, []int{7, 7}},
		{"anniversary-2025.json", []string{"15", "16"}, []int{7, 7}},
		{"anniversary-2026.json", []string{"19", "20"}, []int{7, 7}},
		{"newyear-2025.json", []string{"13", "14"}, []int{7, 10}},
		{"newyear-2026.json", []string{"17", "18"}, []int{7, 10}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := fs.ReadFile(WebFS(), "event-reward-presets/"+tc.file)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateEventStageGifts(string(raw)); err != nil {
				t.Fatal(err)
			}
			if err := srv.validateEventStageGiftItems(t.Context(), string(raw)); err != nil {
				t.Fatal(err)
			}
			var preset map[string]map[int]json.RawMessage
			if err := json.Unmarshal(raw, &preset); err != nil {
				t.Fatal(err)
			}
			if len(preset) != 2 {
				t.Fatalf("expected two branches, got %d", len(preset))
			}
			for i, branch := range tc.branches {
				if len(preset[branch]) != tc.lastStages[i]+1 {
					t.Fatalf("branch %s must have stages 1..%d and completion", branch, tc.lastStages[i])
				}
				for stage := 0; stage <= tc.lastStages[i]; stage++ {
					if len(preset[branch][stage]) == 0 {
						t.Fatalf("missing %s/%d", branch, stage)
					}
				}
			}
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/event-reward-presets/"+tc.file, nil))
			if w.Code != 200 || w.Body.String() != string(raw) {
				t.Fatalf("embedded preset not served: %d", w.Code)
			}
		})
	}
	var n int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM tunables WHERE key='event_stage_gifts'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("reading preset must not activate it: count=%d err=%v", n, err)
	}
}

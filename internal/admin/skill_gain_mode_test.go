package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/logging"
)

func TestSkillGainModeSetIsAtomicAndPreservesCustomValues(t *testing.T) {
	db := testDB(t)
	if err := db.EnsureAdmin(context.Background(), "admin", "admin123"); err != nil {
		t.Fatal(err)
	}
	s := NewServer(config.Admin{JWTSecret: "skill-gain-mode-test"}, db, logging.Discard(), WebFS())
	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		return w
	}
	login := call(http.MethodPost, "/api/login", "", `{"username":"admin","password":"admin123"}`)
	var auth struct {
		Token string `json:"token"`
	}
	if login.Code != http.StatusOK || json.Unmarshal(login.Body.Bytes(), &auth) != nil || auth.Token == "" {
		t.Fatalf("login: %d %s", login.Code, login.Body)
	}
	postMode := func(mode string) int {
		return call(http.MethodPost, "/api/skill_gain_mode", auth.Token, `{"mode":"`+mode+`"}`).Code
	}

	if code := postMode("fixed100"); code != http.StatusOK {
		t.Fatalf("fixed100 code=%d", code)
	}
	values, err := db.TunableList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"skill_gain_pct_1", "skill_gain_pct_2", "skill_gain_pct_3", "skill_gain_pct_11", "skill_gain_pct_4", "skill_gain_pct_6", "skill_gain_pct_5", "skill_gain_pct_8"} {
		if values[key] != "100" {
			t.Fatalf("%s=%q, want 100", key, values[key])
		}
	}
	if values["skill_gain_mode"] != "fixed100" {
		t.Fatalf("mode=%q", values["skill_gain_mode"])
	}

	if code := call(http.MethodPost, "/api/tunables", auth.Token, `{"key":"skill_gain_pct_2","value":"37"}`).Code; code != http.StatusOK {
		t.Fatalf("set custom value code=%d", code)
	}
	if code := postMode("custom"); code != http.StatusOK {
		t.Fatalf("custom code=%d", code)
	}
	values, _ = db.TunableList(context.Background())
	if values["skill_gain_pct_2"] != "37" || values["skill_gain_pct_1"] != "100" || values["skill_gain_mode"] != "custom" {
		t.Fatalf("custom should preserve values: %+v", values)
	}

	if code := postMode("native"); code != http.StatusOK {
		t.Fatalf("native code=%d", code)
	}
	values, _ = db.TunableList(context.Background())
	for _, key := range []string{"skill_gain_pct_1", "skill_gain_pct_2", "skill_gain_pct_3", "skill_gain_pct_11", "skill_gain_pct_4", "skill_gain_pct_6", "skill_gain_pct_5", "skill_gain_pct_8"} {
		if values[key] != "0" {
			t.Fatalf("%s=%q, want 0", key, values[key])
		}
	}
	if values["skill_gain_mode"] != "native" {
		t.Fatalf("mode=%q", values["skill_gain_mode"])
	}

	if code := postMode("bogus"); code != http.StatusBadRequest {
		t.Fatalf("invalid mode code=%d, want 400", code)
	}
	for _, value := range []string{"101", "-1", "NaN"} {
		if code := call(http.MethodPost, "/api/tunables", auth.Token, `{"key":"skill_gain_pct_1","value":"`+value+`"}`).Code; code != http.StatusBadRequest {
			t.Fatalf("skill_gain_pct value %q code=%d, want 400", value, code)
		}
	}
}

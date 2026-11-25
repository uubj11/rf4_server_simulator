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

func TestEmptyAdminListsEncodeAsArrays(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.EnsureAdmin(ctx, "admin", "admin123"); err != nil {
		t.Fatal(err)
	}
	s := NewServer(config.Admin{JWTSecret: "empty-lists-test"}, db, logging.Discard(), WebFS())
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
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body)
	}
	var auth struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &auth); err != nil || auth.Token == "" {
		t.Fatalf("decode login: %v %s", err, login.Body)
	}

	players := call(http.MethodGet, "/api/players", auth.Token, "")
	if players.Code != http.StatusOK || strings.TrimSpace(players.Body.String()) != "[]" {
		t.Fatalf("empty players must encode as []: %d %s", players.Code, players.Body)
	}

	if _, err := db.SQL.Exec(`DELETE FROM audit_log`); err != nil {
		t.Fatal(err)
	}
	audit := call(http.MethodGet, "/api/audit", auth.Token, "")
	if audit.Code != http.StatusOK || strings.TrimSpace(audit.Body.String()) != "[]" {
		t.Fatalf("empty audit must encode as []: %d %s", audit.Code, audit.Body)
	}

	if _, err := db.EnsureProfile(ctx, 1000001); err != nil {
		t.Fatal(err)
	}
	noMatch := call(http.MethodGet, "/api/players?q=definitely-no-such-player", auth.Token, "")
	if noMatch.Code != http.StatusOK || strings.TrimSpace(noMatch.Body.String()) != "[]" {
		t.Fatalf("no-match players must encode as []: %d %s", noMatch.Code, noMatch.Body)
	}
}

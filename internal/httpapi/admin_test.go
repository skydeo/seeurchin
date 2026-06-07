package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/enderu/seeurchin/internal/auth"
	"github.com/enderu/seeurchin/internal/config"
	"github.com/enderu/seeurchin/internal/jellyfin"
	"github.com/enderu/seeurchin/internal/poll"
	"github.com/enderu/seeurchin/internal/store"
)

func newAdminTestServer(t *testing.T, adminToken string) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	resolver := fakeResolver{items: map[string]*poll.ResolvedItem{
		"m1": {ID: "m1", Title: "Dune", Type: "Movie", Year: 2021},
		"m2": {ID: "m2", Title: "Arrival", Type: "Movie", Year: 2016},
	}}
	cfg := config.Config{BaseURL: "http://example.test", SessionSecret: []byte("test-secret-test-secret"), AdminToken: adminToken}
	svc := poll.NewService(st, resolver, 0)
	srv := NewServer(cfg, svc, st, jellyfin.New("http://unused", "k"), nil, auth.NewSessions(cfg.SessionSecret), NewHub())
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts
}

func TestAdminDisabledReturns404(t *testing.T) {
	ts := newAdminTestServer(t, "")
	c := newClient(t)
	for _, path := range []string{"/api/admin/session", "/api/admin/polls"} {
		if code := do(t, c, http.MethodGet, ts.URL+path, nil, nil); code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}
	if code := do(t, c, http.MethodPost, ts.URL+"/api/admin/login", map[string]string{"token": "x"}, nil); code != http.StatusNotFound {
		t.Errorf("login when disabled = %d, want 404", code)
	}
}

func TestAdminDashboardFlow(t *testing.T) {
	ts := newAdminTestServer(t, "sekret")
	admin := newClient(t)
	host := newClient(t)

	// Build a closed poll with a winner via the normal participant API.
	var created pollView
	if code := do(t, host, http.MethodPost, ts.URL+"/api/polls", map[string]any{
		"title": "Movie Night", "host_name": "Alice", "library_scope": "movie",
		"voting_method": "approval",
		"voting_config": json.RawMessage(`{"votes_per_user":3,"max_votes_per_option":1,"allow_self_vote":true}`),
		"allow_guests":  true,
	}, &created); code != http.StatusCreated {
		t.Fatalf("create = %d", code)
	}
	pc := created.Code
	for _, id := range []string{"m1", "m2"} {
		do(t, host, http.MethodPost, ts.URL+"/api/polls/"+pc+"/nominations", map[string]string{"item_id": id}, nil)
	}
	var r2 pollView
	do(t, host, http.MethodPost, ts.URL+"/api/polls/"+pc+"/advance", nil, &r2)
	m1 := nomIDByItem(r2, "m1")
	do(t, host, http.MethodPost, ts.URL+"/api/polls/"+pc+"/votes", map[string]any{"selections": map[string]int{m1: 1}}, nil)
	do(t, host, http.MethodPost, ts.URL+"/api/polls/"+pc+"/advance", nil, nil) // -> closed

	// Gating before login.
	var sess struct {
		Authenticated bool `json:"authenticated"`
	}
	if code := do(t, admin, http.MethodGet, ts.URL+"/api/admin/session", nil, &sess); code != http.StatusOK || sess.Authenticated {
		t.Fatalf("session pre-login = %d auth=%v", code, sess.Authenticated)
	}
	if code := do(t, admin, http.MethodGet, ts.URL+"/api/admin/polls", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("polls pre-login = %d, want 401", code)
	}
	if code := do(t, admin, http.MethodPost, ts.URL+"/api/admin/login", map[string]string{"token": "wrong"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401", code)
	}

	// Login, then the session reports authenticated.
	if code := do(t, admin, http.MethodPost, ts.URL+"/api/admin/login", map[string]string{"token": "sekret"}, nil); code != http.StatusOK {
		t.Fatalf("login = %d", code)
	}
	if code := do(t, admin, http.MethodGet, ts.URL+"/api/admin/session", nil, &sess); code != http.StatusOK || !sess.Authenticated {
		t.Fatalf("session post-login = %d auth=%v", code, sess.Authenticated)
	}

	// History shows the closed poll with winner + counts.
	var list []adminPollView
	if code := do(t, admin, http.MethodGet, ts.URL+"/api/admin/polls", nil, &list); code != http.StatusOK {
		t.Fatalf("polls = %d", code)
	}
	var row *adminPollView
	for i := range list {
		if list[i].Code == pc {
			row = &list[i]
		}
	}
	if row == nil {
		t.Fatal("closed poll missing from history")
	}
	if row.Status != "closed" || row.WinnerTitle != "Dune" {
		t.Fatalf("row = %+v, want closed/Dune", *row)
	}
	if row.ParticipantCount != 1 || row.NominationCount != 2 || row.VoterCount != 1 {
		t.Fatalf("counts = %+v, want 1/2/1", *row)
	}

	// Force-advance a fresh round-1 poll without being its host.
	var p2 pollView
	do(t, host, http.MethodPost, ts.URL+"/api/polls", map[string]any{
		"title": "Other", "host_name": "Bob", "library_scope": "movie", "voting_method": "approval", "allow_guests": true,
	}, &p2)
	for _, id := range []string{"m1", "m2"} {
		do(t, host, http.MethodPost, ts.URL+"/api/polls/"+p2.Code+"/nominations", map[string]string{"item_id": id}, nil)
	}
	var adv adminPollView
	if code := do(t, admin, http.MethodPost, ts.URL+"/api/admin/polls/"+p2.Code+"/advance", nil, &adv); code != http.StatusOK {
		t.Fatalf("admin advance = %d", code)
	}
	if adv.Status != "round2" {
		t.Fatalf("advanced status = %q, want round2", adv.Status)
	}

	// Delete it; it's then gone.
	if code := do(t, admin, http.MethodDelete, ts.URL+"/api/admin/polls/"+p2.Code, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", code)
	}
	if code := do(t, admin, http.MethodGet, ts.URL+"/api/admin/polls/"+p2.Code, nil, nil); code != http.StatusNotFound {
		t.Fatalf("get deleted = %d, want 404", code)
	}

	// Logout drops access.
	do(t, admin, http.MethodPost, ts.URL+"/api/admin/logout", nil, nil)
	if code := do(t, admin, http.MethodGet, ts.URL+"/api/admin/polls", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("polls post-logout = %d, want 401", code)
	}
}

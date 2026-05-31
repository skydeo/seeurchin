package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/enderu/seeurchin/internal/auth"
	"github.com/enderu/seeurchin/internal/config"
	"github.com/enderu/seeurchin/internal/jellyfin"
	"github.com/enderu/seeurchin/internal/poll"
	"github.com/enderu/seeurchin/internal/store"
)

type fakeResolver struct{ items map[string]*poll.ResolvedItem }

func (f fakeResolver) GetItem(_ context.Context, id string) (*poll.ResolvedItem, error) {
	return f.items[id], nil
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	resolver := fakeResolver{items: map[string]*poll.ResolvedItem{
		"m1": {ID: "m1", Title: "Dune", Type: "Movie", Year: 2021},
		"m2": {ID: "m2", Title: "Arrival", Type: "Movie", Year: 2016},
		"m3": {ID: "m3", Title: "Sicario", Type: "Movie", Year: 2015},
	}}
	cfg := config.Config{BaseURL: "http://example.test", SessionSecret: []byte("test-secret-test-secret")}
	svc := poll.NewService(st, resolver, 0)
	srv := NewServer(cfg, svc, st, jellyfin.New("http://unused", "k"), auth.NewSessions(cfg.SessionSecret), NewHub())

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts
}

func newClient(t *testing.T) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

// do performs a JSON request and decodes the response into out (if non-nil).
func do(t *testing.T, c *http.Client, method, url string, body any, out any) int {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s %s: %v", method, url, err)
		}
	}
	return resp.StatusCode
}

func nomIDByItem(v pollView, itemID string) string {
	for _, n := range v.Nominations {
		if n.ItemID == itemID {
			return n.ID
		}
	}
	return ""
}

func TestFullPollFlow(t *testing.T) {
	ts := newTestServer(t)
	host := newClient(t)
	guest := newClient(t)

	// Host creates a poll (approval voting: 3 votes, 1 per option, self-vote ok).
	var created pollView
	code := do(t, host, http.MethodPost, ts.URL+"/api/polls", map[string]any{
		"title":         "Friday Night",
		"host_name":     "Alice",
		"library_scope": "movie",
		"voting_method": "approval",
		"voting_config": json.RawMessage(`{"votes_per_user":3,"max_votes_per_option":1,"allow_self_vote":true}`),
		"allow_guests":  true,
	}, &created)
	if code != http.StatusCreated {
		t.Fatalf("create poll status = %d", code)
	}
	if created.Me == nil || !created.Me.IsHost {
		t.Fatal("creator should be host")
	}
	pollCode := created.Code

	// Host nominates two movies.
	for _, id := range []string{"m1", "m2"} {
		if c := do(t, host, http.MethodPost, ts.URL+"/api/polls/"+pollCode+"/nominations", map[string]string{"item_id": id}, nil); c != http.StatusOK {
			t.Fatalf("nominate %s status = %d", id, c)
		}
	}

	// Guest joins and nominates a third.
	if c := do(t, guest, http.MethodPost, ts.URL+"/api/polls/"+pollCode+"/join", map[string]string{"display_name": "Bob"}, nil); c != http.StatusOK {
		t.Fatalf("join status = %d", c)
	}
	if c := do(t, guest, http.MethodPost, ts.URL+"/api/polls/"+pollCode+"/nominations", map[string]string{"item_id": "m3"}, nil); c != http.StatusOK {
		t.Fatalf("guest nominate status = %d", c)
	}

	// Unknown item is rejected.
	if c := do(t, host, http.MethodPost, ts.URL+"/api/polls/"+pollCode+"/nominations", map[string]string{"item_id": "nope"}, nil); c != http.StatusBadRequest {
		t.Fatalf("unknown item status = %d, want 400", c)
	}

	// Host advances to round 2.
	var afterAdvance pollView
	if c := do(t, host, http.MethodPost, ts.URL+"/api/polls/"+pollCode+"/advance", nil, &afterAdvance); c != http.StatusOK {
		t.Fatalf("advance status = %d", c)
	}
	if afterAdvance.Status != "round2" {
		t.Fatalf("status = %q, want round2", afterAdvance.Status)
	}
	if len(afterAdvance.Nominations) != 3 {
		t.Fatalf("expected 3 nominations, got %d", len(afterAdvance.Nominations))
	}

	m1 := nomIDByItem(afterAdvance, "m1")
	m3 := nomIDByItem(afterAdvance, "m3")

	// Both vote: m1 gets 2 votes, m3 gets 1 -> m1 wins.
	if c := do(t, host, http.MethodPost, ts.URL+"/api/polls/"+pollCode+"/votes", map[string]any{"selections": map[string]int{m1: 1, m3: 1}}, nil); c != http.StatusOK {
		t.Fatalf("host vote status = %d", c)
	}
	if c := do(t, guest, http.MethodPost, ts.URL+"/api/polls/"+pollCode+"/votes", map[string]any{"selections": map[string]int{m1: 1}}, nil); c != http.StatusOK {
		t.Fatalf("guest vote status = %d", c)
	}

	// Over-budget ballot rejected (4 selections, budget 3).
	m2 := nomIDByItem(afterAdvance, "m2")
	if c := do(t, host, http.MethodPost, ts.URL+"/api/polls/"+pollCode+"/votes", map[string]any{"selections": map[string]int{m1: 2, m2: 2}}, nil); c != http.StatusBadRequest {
		t.Fatalf("over-budget vote status = %d, want 400", c)
	}

	// Host closes the poll.
	if c := do(t, host, http.MethodPost, ts.URL+"/api/polls/"+pollCode+"/advance", nil, nil); c != http.StatusOK {
		t.Fatalf("close status = %d", c)
	}

	// Results: m1 wins with score 2.
	var results resultsView
	if c := do(t, host, http.MethodGet, ts.URL+"/api/polls/"+pollCode+"/results", nil, &results); c != http.StatusOK {
		t.Fatalf("results status = %d", c)
	}
	if len(results.Winners) != 1 || results.Winners[0].Title != "Dune" {
		t.Fatalf("winners = %+v, want [Dune]", results.Winners)
	}
	if results.Ranked[0].Score != 2 {
		t.Fatalf("top score = %v, want 2", results.Ranked[0].Score)
	}
}

func TestGuestRejectedWhenDisallowed(t *testing.T) {
	ts := newTestServer(t)
	host := newClient(t)
	guest := newClient(t)

	var created pollView
	do(t, host, http.MethodPost, ts.URL+"/api/polls", map[string]any{
		"title": "Private", "host_name": "Alice", "library_scope": "both",
		"voting_method": "approval", "allow_guests": false,
	}, &created)

	if c := do(t, guest, http.MethodPost, ts.URL+"/api/polls/"+created.Code+"/join", map[string]string{"display_name": "Bob"}, nil); c != http.StatusForbidden {
		t.Fatalf("guest join status = %d, want 403", c)
	}
}

func TestVoteRequiresJoin(t *testing.T) {
	ts := newTestServer(t)
	host := newClient(t)
	stranger := newClient(t)

	var created pollView
	do(t, host, http.MethodPost, ts.URL+"/api/polls", map[string]any{
		"title": "P", "host_name": "Alice", "library_scope": "both", "voting_method": "approval", "allow_guests": true,
	}, &created)

	if c := do(t, stranger, http.MethodPost, ts.URL+"/api/polls/"+created.Code+"/nominations", map[string]string{"item_id": "m1"}, nil); c != http.StatusUnauthorized {
		t.Fatalf("stranger nominate status = %d, want 401", c)
	}
}

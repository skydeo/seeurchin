package jellyfin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthHeaderModernFormat(t *testing.T) {
	h := authHeader("secret-token")
	if !strings.HasPrefix(h, "MediaBrowser ") {
		t.Fatalf("header must use the MediaBrowser scheme, got %q", h)
	}
	for _, want := range []string{
		`Token="secret-token"`,
		`Client="seeurchin"`,
		`DeviceId="seeurchin-server"`,
		`Version="0.1.0"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("header %q missing %q", h, want)
		}
	}
	// Guard against accidentally reintroducing legacy schemes.
	for _, legacy := range []string{"X-Emby", "api_key", "X-MediaBrowser"} {
		if strings.Contains(h, legacy) {
			t.Errorf("header must not contain legacy token %q: %q", legacy, h)
		}
	}
}

func TestAuthHeaderOmitsEmptyToken(t *testing.T) {
	h := authHeader("")
	if strings.Contains(h, "Token=") {
		t.Errorf("empty token should be omitted, got %q", h)
	}
}

func TestSearchSendsModernHeaderAndParses(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/Items" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("SearchTerm") != "matrix" {
			t.Errorf("SearchTerm = %q, want matrix", r.URL.Query().Get("SearchTerm"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[{"Id":"abc","Name":"The Matrix","Type":"Movie","ProductionYear":1999,"RunTimeTicks":81600000000,"ImageTags":{"Primary":"tag1"}}],"TotalRecordCount":1}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	items, total, err := c.Search(context.Background(), SearchParams{Query: "matrix", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !strings.HasPrefix(gotAuth, `MediaBrowser `) || !strings.Contains(gotAuth, `Token="secret-token"`) {
		t.Fatalf("server saw non-modern auth header: %q", gotAuth)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("got %d items (total %d), want 1", len(items), total)
	}
	it := items[0]
	if it.Name != "The Matrix" || it.PrimaryImageTag() != "tag1" {
		t.Fatalf("unexpected item %+v", it)
	}
	if it.RuntimeMinutes() != 136 {
		t.Errorf("runtime = %d min, want 136", it.RuntimeMinutes())
	}
}

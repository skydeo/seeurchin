package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// createPollForPreview spins up a poll and returns its share code.
func createPollForPreview(t *testing.T, ts string, title string) string {
	t.Helper()
	host := newClient(t)
	var created pollView
	code := do(t, host, http.MethodPost, ts+"/api/polls", map[string]any{
		"title":         title,
		"host_name":     "Alice",
		"library_scope": "movie",
		"voting_method": "approval",
		"voting_config": json.RawMessage(`{"votes_per_user":3,"max_votes_per_option":1,"allow_self_vote":true}`),
		"allow_guests":  true,
	}, &created)
	if code != http.StatusCreated {
		t.Fatalf("create poll status = %d", code)
	}
	return created.Code
}

func TestPollPageInjectsOpenGraphTags(t *testing.T) {
	ts := newTestServer(t)
	code := createPollForPreview(t, ts.URL, "Friday Movie Night")

	resp, err := http.Get(ts.URL + "/p/" + code)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	wantImg := "http://example.test/p/" + code + "/preview.png"
	for _, want := range []string{
		`property="og:title" content="Friday Movie Night"`,
		`property="og:image" content="` + wantImg + `"`,
		`property="og:url" content="http://example.test/p/` + code + `"`,
		`name="twitter:card" content="summary_large_image"`,
		"code " + code, // description carries the code
		"</head>",      // injection didn't corrupt the document
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestPollPageUnknownCodeFallsBackToSPA(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/p/ZZZZZZ")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Unknown poll: still serve the SPA shell (no OG tags), never a 404.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "og:image") {
		t.Error("unknown code should not inject OG tags")
	}
}

func TestPollPreviewServesPNG(t *testing.T) {
	ts := newTestServer(t)
	code := createPollForPreview(t, ts.URL, "Friday Movie Night")

	resp, err := http.Get(ts.URL + "/p/" + code + "/preview.png")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) < 8 || string(body[1:4]) != "PNG" {
		t.Fatalf("not a PNG (len=%d)", len(body))
	}
}

func TestPollPreviewUnknownCode404(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/p/ZZZZZZ/preview.png")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

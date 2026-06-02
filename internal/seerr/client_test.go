package seerr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchMapsAndDropsPeople(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != "k" {
			t.Errorf("X-Api-Key = %q, want k", got)
		}
		if got := r.URL.Query().Get("query"); got != "dune" {
			t.Errorf("query = %q, want dune", got)
		}
		_, _ = io.WriteString(w, `{"results":[
			{"id":438631,"mediaType":"movie","title":"Dune","releaseDate":"2021-10-22","posterPath":"/d.jpg","mediaInfo":{"status":5}},
			{"id":1399,"mediaType":"tv","name":"Game of Thrones","firstAirDate":"2011-04-17","posterPath":"/g.jpg"},
			{"id":99,"mediaType":"person","name":"Denis Villeneuve"}
		]}`)
	}))
	defer ts.Close()

	res, err := New(ts.URL, "k").Search(context.Background(), "dune")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 results (person dropped), got %d: %+v", len(res), res)
	}
	if res[0].Title != "Dune" || res[0].Year != 2021 || res[0].MediaType != "movie" {
		t.Errorf("movie mapped wrong: %+v", res[0])
	}
	if res[0].PosterURL != posterBase+"/d.jpg" {
		t.Errorf("poster url = %q", res[0].PosterURL)
	}
	if !res[0].InLibrary {
		t.Errorf("status 5 should mean in-library")
	}
	if res[1].Title != "Game of Thrones" || res[1].Year != 2011 || res[1].MediaType != "tv" {
		t.Errorf("tv mapped wrong (title/name + firstAirDate): %+v", res[1])
	}
	if res[1].InLibrary {
		t.Errorf("no mediaInfo should mean not in-library")
	}
}

func TestSearchCapturesGenreIDs(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"results":[
			{"id":1,"mediaType":"movie","title":"A","genreIds":[878,12]}
		]}`)
	}))
	defer ts.Close()
	res, err := New(ts.URL, "k").Search(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || len(res[0].GenreIDs) != 2 || res[0].GenreIDs[0] != 878 {
		t.Fatalf("genre ids not captured: %+v", res)
	}
}

func TestGenreFiltering(t *testing.T) {
	// "Science Fiction" maps to the movie Sci-Fi id (878) and the TV bucket
	// (10765); unrecognized names contribute nothing.
	allowed := GenreIDSet([]string{"Science Fiction", "made up genre"})
	if !allowed[878] || !allowed[10765] {
		t.Fatalf("science fiction should map to 878 and 10765: %v", allowed)
	}
	if !MatchesGenres([]int{18, 878}, allowed) {
		t.Errorf("a sci-fi title should match")
	}
	if MatchesGenres([]int{18, 35}, allowed) {
		t.Errorf("a drama/comedy title should not match a sci-fi restriction")
	}
	// An empty allowed set (no restriction, or only unknown names) matches all.
	if !MatchesGenres([]int{18}, GenreIDSet(nil)) || !MatchesGenres([]int{18}, GenreIDSet([]string{"???"})) {
		t.Errorf("no recognized restriction should match everything")
	}
}

func TestCreateRequestBody(t *testing.T) {
	var gotMovie, gotTV map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/request" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["mediaType"] == "tv" {
			gotTV = body
		} else {
			gotMovie = body
		}
		_, _ = io.WriteString(w, `{"status":1}`)
	}))
	defer ts.Close()
	c := New(ts.URL, "k")

	// Movie with explicit profile/root/server/user.
	got, err := c.CreateRequest(context.Background(), RequestInput{
		MediaType: "movie", TMDBID: 550, ProfileID: 4, RootFolder: "/movies", ServerID: 0, UserID: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "pending" {
		t.Errorf("status label = %q, want pending", got.Status)
	}
	if gotMovie["mediaId"].(float64) != 550 || gotMovie["profileId"].(float64) != 4 ||
		gotMovie["rootFolder"] != "/movies" || gotMovie["serverId"].(float64) != 0 || gotMovie["userId"].(float64) != 7 {
		t.Errorf("movie body wrong: %+v", gotMovie)
	}
	if _, ok := gotMovie["seasons"]; ok {
		t.Errorf("movie request should not carry seasons: %+v", gotMovie)
	}

	// TV with omitted optional fields requests all seasons and omits
	// profile/root/server/user.
	if _, err := c.CreateRequest(context.Background(), RequestInput{
		MediaType: "tv", TMDBID: 1399, ProfileID: -1, ServerID: -1, UserID: -1,
	}); err != nil {
		t.Fatal(err)
	}
	if gotTV["seasons"] != "all" {
		t.Errorf("tv request should request all seasons: %+v", gotTV)
	}
	for _, k := range []string{"profileId", "rootFolder", "serverId", "userId"} {
		if _, ok := gotTV[k]; ok {
			t.Errorf("unset %q should be omitted: %+v", k, gotTV)
		}
	}
}

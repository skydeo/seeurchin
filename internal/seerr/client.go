// Package seerr is a minimal client for the Seerr (Overseerr/Jellyseerr) REST
// API, scoped to what seeurchin needs: searching for titles that aren't in the
// Jellyfin library (TMDB-backed) and requesting the winning write-in.
//
// It authenticates with the X-Api-Key header. The API lives under /api/v1.
package seerr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// posterBase is the TMDB image CDN prefix used to build full poster URLs.
const posterBase = "https://image.tmdb.org/t/p/w500"

// Client talks to a single Seerr server using an API key.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New returns a client for the given Seerr base URL and API key.
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Result is a movie or TV hit, mapped from Seerr's TMDB-backed responses.
type Result struct {
	TMDBID    int    `json:"tmdb_id"`
	MediaType string `json:"media_type"` // "movie" | "tv"
	Title     string `json:"title"`
	Year      int    `json:"year"`
	PosterURL string `json:"poster_url"`
	Overview  string `json:"overview"`
	InLibrary bool   `json:"in_library"`
}

// tmdbItem is the shared shape of search results and detail responses.
type tmdbItem struct {
	ID           int    `json:"id"`
	MediaType    string `json:"mediaType"`
	Title        string `json:"title"`        // movie
	Name         string `json:"name"`         // tv
	ReleaseDate  string `json:"releaseDate"`  // movie
	FirstAirDate string `json:"firstAirDate"` // tv
	PosterPath   string `json:"posterPath"`
	Overview     string `json:"overview"`
	MediaInfo    *struct {
		Status int `json:"status"` // 4 = partially available, 5 = available
	} `json:"mediaInfo"`
}

func (it tmdbItem) toResult(mediaType string) Result {
	return Result{
		TMDBID:    it.ID,
		MediaType: mediaType,
		Title:     pick(it.Title, it.Name),
		Year:      yearOf(pick(it.ReleaseDate, it.FirstAirDate)),
		PosterURL: posterURL(it.PosterPath),
		Overview:  it.Overview,
		InLibrary: it.MediaInfo != nil && it.MediaInfo.Status >= 4,
	}
}

// Search returns movie/TV matches for query (people and other types dropped).
func (c *Client) Search(ctx context.Context, query string) ([]Result, error) {
	q := url.Values{}
	q.Set("query", query)
	var out struct {
		Results []tmdbItem `json:"results"`
	}
	if err := c.getJSON(ctx, "/api/v1/search", q, &out); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(out.Results))
	for _, r := range out.Results {
		if r.MediaType != "movie" && r.MediaType != "tv" {
			continue
		}
		results = append(results, r.toResult(r.MediaType))
	}
	return results, nil
}

// GetMovie / GetTV resolve a single title by TMDB id (returns nil if missing).
func (c *Client) GetMovie(ctx context.Context, tmdbID int) (*Result, error) {
	return c.getDetail(ctx, "movie", tmdbID)
}
func (c *Client) GetTV(ctx context.Context, tmdbID int) (*Result, error) {
	return c.getDetail(ctx, "tv", tmdbID)
}

func (c *Client) getDetail(ctx context.Context, mediaType string, tmdbID int) (*Result, error) {
	var raw tmdbItem
	if err := c.getJSON(ctx, "/api/v1/"+mediaType+"/"+strconv.Itoa(tmdbID), nil, &raw); err != nil {
		return nil, err
	}
	if raw.ID == 0 {
		return nil, nil
	}
	r := raw.toResult(mediaType)
	return &r, nil
}

// RequestInput describes a request to create. Int fields < 0 and an empty
// RootFolder are omitted, letting Seerr apply its own defaults.
type RequestInput struct {
	MediaType  string // "movie" | "tv"
	TMDBID     int
	ProfileID  int
	RootFolder string
	ServerID   int
}

// RequestResult is the outcome of creating a request.
type RequestResult struct {
	Status string // "pending" | "approved" | "declined" | "requested"
}

// CreateRequest asks Seerr to request a title. For TV it requests all seasons.
func (c *Client) CreateRequest(ctx context.Context, in RequestInput) (RequestResult, error) {
	body := map[string]any{"mediaType": in.MediaType, "mediaId": in.TMDBID}
	if in.MediaType == "tv" {
		body["seasons"] = "all"
	}
	if in.ProfileID >= 0 {
		body["profileId"] = in.ProfileID
	}
	if in.RootFolder != "" {
		body["rootFolder"] = in.RootFolder
	}
	if in.ServerID >= 0 {
		body["serverId"] = in.ServerID
	}
	var out struct {
		Status int `json:"status"`
	}
	if err := c.postJSON(ctx, "/api/v1/request", body, &out); err != nil {
		return RequestResult{}, err
	}
	return RequestResult{Status: requestStatusLabel(out.Status)}, nil
}

// --- helpers ---

func pick(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(date[:4])
	return y
}

func posterURL(path string) string {
	if path == "" {
		return ""
	}
	return posterBase + path
}

// requestStatusLabel maps Seerr's MediaRequest status enum to a label.
func requestStatusLabel(status int) string {
	switch status {
	case 1:
		return "pending"
	case 2:
		return "approved"
	case 3:
		return "declined"
	default:
		return "requested"
	}
}

func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	return c.do(req, out)
}

func (c *Client) postJSON(ctx context.Context, path string, body any, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return fmt.Errorf("seerr %s %s: %s: %s", req.Method, req.URL.Path, resp.Status, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

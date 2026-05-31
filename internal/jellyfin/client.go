// Package jellyfin is a minimal client for the Jellyfin REST API, scoped to
// what seeurchin needs: reading the movie/series library and proxying poster
// images.
//
// It authenticates with the modern Authorization header
// (`Authorization: MediaBrowser Token="...", Client="...", ...`). Jellyfin is
// removing the legacy schemes (X-Emby-Token, the api_key query parameter, etc.)
// in 10.13, so this client deliberately avoids them.
package jellyfin

import (
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

const (
	clientName    = "seeurchin"
	clientVersion = "0.1.0"
	deviceName    = "seeurchin-server"
	deviceID      = "seeurchin-server"
)

// Client talks to a single Jellyfin server using an API key for library reads.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New returns a client for the given Jellyfin base URL and API key.
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// authHeader builds the modern Jellyfin Authorization header value. token is
// the access token or API key; it is omitted for unauthenticated calls.
func authHeader(token string) string {
	parts := []string{
		fmt.Sprintf("Client=%q", clientName),
		fmt.Sprintf("Device=%q", deviceName),
		fmt.Sprintf("DeviceId=%q", deviceID),
		fmt.Sprintf("Version=%q", clientVersion),
	}
	if token != "" {
		parts = append([]string{fmt.Sprintf("Token=%q", token)}, parts...)
	}
	return "MediaBrowser " + strings.Join(parts, ", ")
}

// Item is a movie or series from the Jellyfin library.
type Item struct {
	ID              string            `json:"Id"`
	Name            string            `json:"Name"`
	Type            string            `json:"Type"` // "Movie" | "Series"
	ProductionYear  int               `json:"ProductionYear"`
	Overview        string            `json:"Overview"`
	RunTimeTicks    int64             `json:"RunTimeTicks"`
	CommunityRating float64           `json:"CommunityRating"`
	ImageTags       map[string]string `json:"ImageTags"`
}

// RuntimeMinutes converts Jellyfin's 100ns ticks to whole minutes.
func (it Item) RuntimeMinutes() int {
	if it.RunTimeTicks <= 0 {
		return 0
	}
	return int(it.RunTimeTicks / 10_000_000 / 60)
}

// PrimaryImageTag returns the cache-busting tag for the primary image, if any.
func (it Item) PrimaryImageTag() string { return it.ImageTags["Primary"] }

// SearchParams controls a library query.
type SearchParams struct {
	Query      string   // free-text search; empty lists everything
	Types      []string // defaults to Movie + Series
	Limit      int
	StartIndex int
}

// Search returns library items matching the parameters along with the total
// record count for pagination.
func (c *Client) Search(ctx context.Context, p SearchParams) ([]Item, int, error) {
	types := p.Types
	if len(types) == 0 {
		types = []string{"Movie", "Series"}
	}
	q := url.Values{}
	q.Set("Recursive", "true")
	q.Set("IncludeItemTypes", strings.Join(types, ","))
	q.Set("Fields", "Overview,ProductionYear,RunTimeTicks,CommunityRating")
	q.Set("SortBy", "SortName")
	q.Set("SortOrder", "Ascending")
	q.Set("EnableImageTypes", "Primary")
	q.Set("ImageTypeLimit", "1")
	if p.Query != "" {
		q.Set("SearchTerm", p.Query)
	}
	if p.Limit > 0 {
		q.Set("Limit", strconv.Itoa(p.Limit))
	}
	if p.StartIndex > 0 {
		q.Set("StartIndex", strconv.Itoa(p.StartIndex))
	}

	var out struct {
		Items            []Item `json:"Items"`
		TotalRecordCount int    `json:"TotalRecordCount"`
	}
	if err := c.getJSON(ctx, "/Items", q, &out); err != nil {
		return nil, 0, err
	}
	return out.Items, out.TotalRecordCount, nil
}

// GetItem fetches a single item by ID. It returns (nil, nil) if not found.
func (c *Client) GetItem(ctx context.Context, id string) (*Item, error) {
	q := url.Values{}
	q.Set("Ids", id)
	q.Set("Recursive", "true")
	q.Set("Fields", "Overview,ProductionYear,RunTimeTicks,CommunityRating")
	q.Set("EnableImageTypes", "Primary")
	var out struct {
		Items []Item `json:"Items"`
	}
	if err := c.getJSON(ctx, "/Items", q, &out); err != nil {
		return nil, err
	}
	if len(out.Items) == 0 {
		return nil, nil
	}
	return &out.Items[0], nil
}

// FetchImage retrieves an item image (e.g. imageType "Primary"). The caller is
// responsible for closing and streaming the returned response body.
func (c *Client) FetchImage(ctx context.Context, itemID, imageType string, query url.Values) (*http.Response, error) {
	u := fmt.Sprintf("%s/Items/%s/Images/%s", c.baseURL, url.PathEscape(itemID), url.PathEscape(imageType))
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authHeader(c.apiKey))
	return c.http.Do(req)
}

// Ping verifies connectivity to the server's public info endpoint.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/System/Info/Public", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jellyfin ping: %s", resp.Status)
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values) (*http.Response, error) {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authHeader(c.apiKey))
	req.Header.Set("Accept", "application/json")
	return c.http.Do(req)
}

func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	resp, err := c.get(ctx, path, q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return fmt.Errorf("jellyfin GET %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

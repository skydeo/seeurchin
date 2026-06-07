// Package config loads seeurchin's runtime configuration from environment
// variables. All settings have sensible defaults except the Jellyfin
// connection, which is required for browsing the library.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all runtime configuration.
type Config struct {
	// Addr is the TCP listen address for the HTTP server, e.g. ":5858".
	Addr string
	// BaseURL is the public origin used to build shareable poll links,
	// e.g. "https://seeurchin.skydeo.xyz" (no trailing slash).
	BaseURL string
	// DBPath is the filesystem path to the SQLite database file.
	DBPath string
	// SessionSecret signs participant session cookies (HMAC-SHA256).
	SessionSecret []byte
	// SessionSecretGenerated is true when no secret was supplied and a random
	// one was generated; sessions will not survive a restart in that case.
	SessionSecretGenerated bool

	Jellyfin JellyfinConfig
	Seerr    SeerrConfig

	// EnableUserLogin turns on Jellyfin username/password login (Phase 2).
	// When false, only guest identities are available.
	EnableUserLogin bool

	// CodeStyle selects the share-code generator: "base32" (default) or "words".
	CodeStyle string

	// AdminToken gates the admin dashboard (poll history + management). When
	// empty the dashboard and its API are disabled entirely (every /api/admin/*
	// route returns 404).
	AdminToken string

	// PollRetentionDays auto-deletes polls this many days after they close
	// (cascading to their participants/nominations/votes). 0 (the default) keeps
	// poll history forever.
	PollRetentionDays int
}

// AdminEnabled reports whether the admin dashboard is configured (a token is
// set). When false, the dashboard and all /api/admin/* endpoints are disabled.
func (c Config) AdminEnabled() bool { return c.AdminToken != "" }

// JellyfinConfig describes how to reach the Jellyfin server for library reads.
type JellyfinConfig struct {
	// URL is the base URL of the Jellyfin server, e.g. "http://jellyfin:8096".
	URL string
	// APIKey is a Jellyfin API key (Dashboard -> API Keys) used for library reads.
	APIKey string
}

// SeerrConfig describes an optional Seerr (Overseerr/Jellyseerr) server used for
// write-in nominations (external search) and auto-requesting the winner. The
// feature is enabled only when both URL and APIKey are set.
type SeerrConfig struct {
	// URL is the base URL of the Seerr server, e.g. "http://seerr:5055".
	URL string
	// APIKey is a Seerr API key (Settings -> General -> API Key).
	APIKey string
	// Optional request defaults; applied to created requests only when set,
	// otherwise Seerr's own defaults are used. Int fields use -1 for "unset".
	MovieProfileID  int
	TVProfileID     int
	MovieRootFolder string
	TVRootFolder    string
	ServerID        int
	// RequestUserID attributes requests to a specific Seerr user (the dedicated
	// "movie night" account), using that user's defaults. -1 = the API key owner.
	RequestUserID int
}

// Enabled reports whether Seerr is configured (URL + API key present).
func (s SeerrConfig) Enabled() bool { return s.URL != "" && s.APIKey != "" }

// FromEnv builds a Config from the process environment, applying defaults and
// validating required fields.
func FromEnv() (Config, error) {
	c := Config{
		Addr:              envOr("SEEURCHIN_ADDR", ":5858"),
		BaseURL:           strings.TrimRight(envOr("SEEURCHIN_BASE_URL", "http://localhost:5858"), "/"),
		DBPath:            envOr("SEEURCHIN_DB_PATH", "./seeurchin.db"),
		EnableUserLogin:   envBool("SEEURCHIN_ENABLE_USER_LOGIN", false),
		CodeStyle:         envOr("SEEURCHIN_CODE_STYLE", "base32"),
		AdminToken:        strings.TrimSpace(os.Getenv("SEEURCHIN_ADMIN_TOKEN")),
		PollRetentionDays: envInt("SEEURCHIN_POLL_RETENTION_DAYS", 0),
		Jellyfin: JellyfinConfig{
			URL:    strings.TrimRight(os.Getenv("JELLYFIN_URL"), "/"),
			APIKey: os.Getenv("JELLYFIN_API_KEY"),
		},
		Seerr: SeerrConfig{
			URL:             strings.TrimRight(os.Getenv("SEERR_URL"), "/"),
			APIKey:          os.Getenv("SEERR_API_KEY"),
			MovieProfileID:  envInt("SEERR_MOVIE_PROFILE_ID", -1),
			TVProfileID:     envInt("SEERR_TV_PROFILE_ID", -1),
			MovieRootFolder: strings.TrimSpace(os.Getenv("SEERR_MOVIE_ROOT_FOLDER")),
			TVRootFolder:    strings.TrimSpace(os.Getenv("SEERR_TV_ROOT_FOLDER")),
			ServerID:        envInt("SEERR_SERVER_ID", -1),
			RequestUserID:   envInt("SEERR_USER_ID", -1),
		},
	}

	if c.Jellyfin.URL == "" {
		return Config{}, fmt.Errorf("JELLYFIN_URL is required")
	}
	if c.Jellyfin.APIKey == "" {
		return Config{}, fmt.Errorf("JELLYFIN_API_KEY is required")
	}

	secret, generated, err := loadSecret(os.Getenv("SEEURCHIN_SESSION_SECRET"))
	if err != nil {
		return Config{}, err
	}
	c.SessionSecret = secret
	c.SessionSecretGenerated = generated

	return c, nil
}

// loadSecret decodes the configured session secret. A hex string is decoded;
// any other non-empty value is used verbatim as bytes. An empty value yields a
// freshly generated 32-byte secret with generated=true.
func loadSecret(raw string) (secret []byte, generated bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, false, fmt.Errorf("generate session secret: %w", err)
		}
		return b, true, nil
	}
	if decoded, derr := hex.DecodeString(raw); derr == nil && len(decoded) >= 16 {
		return decoded, false, nil
	}
	return []byte(raw), false, nil
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

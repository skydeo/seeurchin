# 8 · External services, embedding & config

Three smaller topics that round out the backend: how the app talks to *other*
HTTP services (Jellyfin and Seerr), how the entire frontend gets baked into the
Go binary, and how configuration is loaded. Each is short, and each shows off a
standard-library feature you'll use constantly in Go.

---

## 8.1 HTTP *clients*: `net/http` from the other side

Chapters 3 and 7 used `net/http` to *serve* requests. The Jellyfin and Seerr
packages use the same library to *make* requests — the rough equivalent of
`httpx`/`requests` in Python. The `jellyfin.Client` is representative:

```go
// internal/jellyfin/client.go
type Client struct {
    baseURL string
    apiKey  string
    http    *http.Client      // a reusable client with a timeout
}

func New(baseURL, apiKey string) *Client {
    return &Client{
        baseURL: strings.TrimRight(baseURL, "/"),
        apiKey:  apiKey,
        http:    &http.Client{Timeout: 15 * time.Second},
    }
}
```

Key practice: **construct one `*http.Client` and reuse it** for every call. It
holds the connection pool (keep-alives), so reusing it is both faster and the
idiomatic way to set a global timeout. Creating a fresh client per request — the
Go beginner's mistake — defeats connection reuse. (Same spirit as reusing an
`httpx.Client` rather than calling `httpx.get` ad hoc.)

A typical GET threads the request's context through, sets auth headers, and
decodes JSON:

```go
// internal/jellyfin/client.go (the shared GET-JSON helper)
func (c *Client) get(ctx context.Context, path string, q url.Values) (*http.Response, error) {
    u := c.baseURL + path
    if len(q) > 0 {
        u += "?" + q.Encode()
    }
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", authHeader(c.apiKey))   // modern Jellyfin scheme
    req.Header.Set("Accept", "application/json")
    return c.http.Do(req)
}

func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
    resp, err := c.get(ctx, path, q)
    if err != nil {
        return err
    }
    defer resp.Body.Close()                                 // ALWAYS close the body
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
        return fmt.Errorf("jellyfin GET %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
    }
    return json.NewDecoder(resp.Body).Decode(out)
}
```

Things to absorb:

- **`http.NewRequestWithContext(ctx, ...)`** binds the outbound call to the
  caller's context. If the *incoming* user request is cancelled (browser closes),
  this *outbound* Jellyfin call is cancelled too — cancellation flows all the way
  through. This is `context.Context` (Chapter 1) doing its real job. Always use
  the `WithContext` constructor, never the context-free `http.NewRequest`.
- **`defer resp.Body.Close()` is mandatory.** An unclosed response body leaks the
  underlying connection (it can't be returned to the pool). The `defer` right
  after the error check is the canonical placement. Forgetting this is the #1
  resource leak in Go HTTP code.
- **Non-2xx is not an error by default.** Unlike `requests`' `raise_for_status`,
  Go's client returns a response for a 404 or 500 *without* erroring (`err` is
  only non-nil for transport-level failures — DNS, connection refused, timeout).
  So you check `resp.StatusCode` yourself. Here a non-2xx becomes a descriptive
  wrapped error, reading a bounded 2 KiB of the body for context.
- **`url.Values`** is Go's query-string builder (`q.Set("Recursive", "true")`,
  then `q.Encode()`), and **struct tags drive JSON decoding** of the response,
  exactly as they drove request decoding in Chapter 3. The Jellyfin `Item` struct
  maps Jellyfin's PascalCase JSON (`"Id"`, `"Name"`, `"RunTimeTicks"`) onto Go
  fields via tags — and adds small computed methods like `RuntimeMinutes()` to
  convert Jellyfin's 100-nanosecond "ticks" into minutes.

The `seerr.Client` is the same shape with a different auth header (`X-Api-Key`)
and a `postJSON` helper for creating requests. One Seerr-specific wrinkle worth a
chuckle, preserved as a comment in the code: Seerr rejects `+`-encoded spaces in
query strings, so the client post-processes `url.Values.Encode()` to use `%20`.
Real-world client code accumulates these; the comment tells you *why* so nobody
"simplifies" it back to a bug.

### Why these clients are tiny and purpose-built

Both clients implement only the handful of endpoints seeurchin needs — library
search, single-item lookup, image fetch (Jellyfin); search, detail, create-request
(Seerr). There's no attempt at a general-purpose SDK. That's idiomatic Go:
**write the narrow client your app needs**, not a wrapper for the whole API. It
keeps the dependency surface zero (just the standard library) and the code
readable.

One design touch tying back to earlier chapters: poster images are **proxied**
through the app (`GET /api/items/{id}/image` → `handleImage` → `jf.FetchImage`),
streaming the bytes with `io.Copy`, so the browser never sees the Jellyfin URL or
API key. The Jellyfin credentials never leave the server.

---

## 8.2 `go:embed`: the frontend lives *inside* the binary

This is one of Go's best deployment features and central to how seeurchin ships.
The compiled SvelteKit app (HTML/JS/CSS) is **embedded into the executable at
build time**, so the final artifact is a single static binary that serves both
the API and the web UI — no separate web server, no static-file directory to ship
alongside.

```go
// internal/httpapi/web.go
import "embed"

//go:embed all:webdist
var webDist embed.FS
```

That `//go:embed all:webdist` is a **compiler directive** (note: it's a magic
comment with no space after `//`, and it must sit immediately above the variable).
At build time the compiler walks the `webdist/` directory and bakes its entire
contents into the binary, exposing them as an `embed.FS` — a read-only,
in-memory filesystem. The `all:` prefix tells it to include files that start with
`.` or `_` too (SvelteKit emits `_app/...`).

The handler serves from that embedded FS with the single-page-app fallback we saw
in Chapter 3:

```go
// internal/httpapi/web.go
func (s *Server) spaHandler() http.Handler {
    sub, _ := fs.Sub(webDist, "webdist")          // treat webdist/ as the root
    fileServer := http.FileServer(http.FS(sub))   // standard static-file handler over the embedded FS
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        name := strings.TrimPrefix(r.URL.Path, "/")
        if name == "" { name = "index.html" }
        if f, err := sub.Open(name); err == nil {  // a real asset?
            f.Close()
            fileServer.ServeHTTP(w, r)              // serve it
            return
        }
        index, _ := sub.Open("index.html")          // otherwise: SPA entrypoint
        io.Copy(w, index)
    })
}
```

`embed.FS` satisfies the standard `fs.FS` interface, so it plugs straight into
`http.FileServer` and `http.FS` — *the same handler that serves files from disk*
serves them from inside the binary, with no code aware of the difference. That
interface-uniformity (a real file tree and an embedded one are interchangeable) is
the same "code to the interface" theme from across this series.

### The build-order consequence (a real gotcha)

Because the frontend is embedded *at Go build time*, there's a chicken-and-egg
detail spelled out in `CLAUDE.md`:

1. `npm --prefix web run build` compiles Svelte → writes `internal/httpapi/webdist/`.
2. `go build` then embeds whatever is currently in `webdist/`.

So a **backend-only rebuild serves whatever frontend was last built into
`webdist/`.** If you change Svelte code and only rebuild Go, you won't see it —
you must re-run the `npm` build first. A committed placeholder
(`webdist/.gitkeep`) lets the Go package compile even before any frontend build
exists (the `embed` directive needs the directory to be present). During frontend
development you bypass all this: `vite dev` serves the app and proxies `/api` to
the running Go backend.

The Dockerfile encodes the right order as a multi-stage build: Node stage builds
the frontend → Go stage builds the static binary embedding it → the result is
copied into a distroless image. One small container, no runtime dependencies.

---

## 8.3 Configuration: env vars → a validated `Config`

All configuration comes from environment variables, parsed once at startup into a
`Config` struct. There's no config file format, no settings library — just
`os.Getenv` and a few typed helpers. `internal/config/config.go` is the whole
story.

```go
// internal/config/config.go (shape)
func FromEnv() (Config, error) {
    c := Config{
        Addr:              envOr("SEEURCHIN_ADDR", ":5858"),
        BaseURL:           strings.TrimRight(envOr("SEEURCHIN_BASE_URL", "http://localhost:5858"), "/"),
        DBPath:            envOr("SEEURCHIN_DB_PATH", "./seeurchin.db"),
        AdminToken:        strings.TrimSpace(os.Getenv("SEEURCHIN_ADMIN_TOKEN")),
        PollRetentionDays: envInt("SEEURCHIN_POLL_RETENTION_DAYS", 0),
        Jellyfin: JellyfinConfig{
            URL:    strings.TrimRight(os.Getenv("JELLYFIN_URL"), "/"),
            APIKey: os.Getenv("JELLYFIN_API_KEY"),
        },
        Seerr: SeerrConfig{ /* URL, APIKey, optional request defaults... */ },
    }

    if c.Jellyfin.URL == "" {
        return Config{}, fmt.Errorf("JELLYFIN_URL is required")
    }
    if c.Jellyfin.APIKey == "" {
        return Config{}, fmt.Errorf("JELLYFIN_API_KEY is required")
    }
    secret, generated, err := loadSecret(os.Getenv("SEEURCHIN_SESSION_SECRET"))
    if err != nil { return Config{}, err }
    c.SessionSecret = secret
    c.SessionSecretGenerated = generated
    return c, nil
}
```

The small `envOr` / `envInt` / `envBool` helpers are just "read the var, trim it,
fall back to a default if empty or unparseable." This is roughly what Pydantic's
`BaseSettings` does for you — except it's a dozen lines you can read, with no
dependency. For an app with ~15 settings, that's a reasonable trade; a larger
config surface might justify a library.

Three patterns here are worth lifting into your own Go:

- **Defaults + validation live together, and validation fails fast.** Only the
  Jellyfin URL and key are mandatory; missing either returns an error that `main`
  turns into a `log.Fatalf` (Chapter 2). Everything else has a sensible default.
  You never boot a half-configured server.
- **"Feature enabled?" is derived, not a separate flag.** `SeerrConfig.Enabled()`
  returns `s.URL != "" && s.APIKey != ""`, and `Config.AdminEnabled()` returns
  `c.AdminToken != ""`. Presence of the credentials *is* the on-switch — no extra
  `SEERR_ENABLED=true` to keep in sync. This is why `main` can decide whether to
  build the Seerr client purely from config, and why the admin routes 404
  themselves when no token is set.
- **The session secret degrades safely and says so.** `loadSecret` accepts a hex
  string (decoded) or any raw value; if *nothing* is supplied it generates a
  random 32-byte secret and flags `generated = true`, so `main` can warn that
  sessions won't survive a restart. Secure default, loud about the consequence.

Because `Config` is a plain struct passed by value into `NewServer` and stored on
`Server.cfg`, every handler can read settings off `s.cfg` with no global state and
no per-request lookups. Configuration is resolved exactly once, at the edge, then
flows inward as immutable data — the same "decide at the boundary, pass plain
values inward" discipline you've seen throughout.

---

## 8.4 What this chapter reinforces

These three features look unrelated, but they share a thread that's very Go:

- **The standard library is enough.** HTTP client, JSON, query encoding, file
  embedding, env reading — all `net/http`, `encoding/json`, `embed`, `os`. The
  `go.mod` has just three direct dependencies (chi for routing, the SQLite
  driver, and `golang.org/x/image` for the preview card). A comparable Python
  stack would pull in dozens of transitive packages.
- **Interfaces make sources interchangeable.** An embedded FS serves like a disk
  FS; a context cancels an outbound call like an inbound one. You keep meeting the
  same small interfaces (`fs.FS`, `io.Reader`, `context.Context`) in new places.
- **Configuration and capability are explicit and validated at the edge**, then
  passed inward as plain data — no ambient globals, no surprise at runtime.

[Next: testing, building & running »](09-testing-building-running.md)

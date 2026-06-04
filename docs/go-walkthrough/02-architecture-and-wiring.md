# 2 · Architecture & wiring

Now that the language basics are in place, let's zoom out and see how the
packages fit together — and then watch `main()` assemble them. This chapter is
about *structure*: where the boundaries are, which way the dependencies point,
and why.

---

## 2.1 The package map

Here's the backend, package by package, with each one's job:

```
cmd/seeurchin/main.go      ← the composition root: builds everything, starts the server
│
internal/
├── config/                ← parse + validate configuration from env vars
├── poll/                  ← THE DOMAIN: types, business rules, the state machine
│   ├── types.go           ·   Poll, Participant, Nomination, Vote, ... (data)
│   ├── service.go         ·   Service: all the rules (the brain)
│   └── repository.go      ·   Repository interface (the persistence *contract*)
├── store/                 ← SQLite implementation of poll.Repository
├── voting/                ← pluggable voting methods (approval/ranked/score/random)
├── auth/                  ← signed session cookies + the (future) auth Provider seam
├── codes/                 ← short shareable poll codes (Crockford base32)
├── jellyfin/              ← minimal Jellyfin REST client (library reads, image proxy)
├── seerr/                 ← minimal Seerr/Overseerr client (write-ins, requests)
└── httpapi/               ← the web layer: router, handlers, views, SSE, embedded UI
    ├── server.go          ·   Server struct + chi route table + helpers
    ├── handlers.go        ·   one function per endpoint
    ├── views.go           ·   response shapes (the API's "schemas")
    ├── sse.go             ·   the live-update Hub
    ├── timer.go           ·   background sweeper + timer endpoints
    ├── admin.go           ·   admin dashboard endpoints
    ├── preview.go         ·   server-rendered link-preview cards
    └── web.go             ·   embeds + serves the built SvelteKit app
```

Mentally, group them into four rings:

1. **Domain core** — `poll` (and `voting`, `codes`). Pure logic and data. Knows
   nothing about HTTP, SQL, or Jellyfin.
2. **Adapters out** — `store` (SQL), `jellyfin` + `seerr` (HTTP clients). These
   *implement* what the core needs.
3. **Delivery in** — `httpapi`, `auth`, `config`. Turn the outside world (HTTP
   requests, cookies, env vars) into calls on the core.
4. **The root** — `cmd/seeurchin/main.go`. The only file that imports from every
   ring and stitches them together.

---

## 2.2 The dependency rule (and how Go enforces it)

The defining architectural choice in this codebase is that **the domain depends
on nothing concrete.** Look at the imports at the top of `internal/poll/service.go`:

```go
import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "strings"
    "time"

    "github.com/enderu/seeurchin/internal/codes"
    "github.com/enderu/seeurchin/internal/voting"
)
```

No `internal/store`. No `internal/jellyfin`. No `database/sql`. No `net/http`.
The business logic literally *cannot* reach out to the database or to Jellyfin,
because it doesn't import them. Instead it declares two **interfaces** describing
what it needs, and lets someone else supply the implementations:

```go
// internal/poll/service.go
type Service struct {
    repo    Repository      // "something that can persist polls"
    items   ItemResolver    // "something that can resolve a library item"
    codeLen int
}
```

`Repository` and `ItemResolver` are defined *in the poll package itself*
(`repository.go` and `service.go`). The concrete types that satisfy them —
`*store.Store` and the `itemResolver` adapter in `main.go` — live elsewhere and
import *poll*, not the other way around.

### Why this is the whole game

This is the [Dependency Inversion Principle](https://en.wikipedia.org/wiki/Dependency_inversion_principle),
and in Go it falls out naturally from interfaces. The payoff is concrete:

- **The domain is unit-testable with no infrastructure.** `service_test.go`
  constructs a `Service` against a real temp-file SQLite store and a 3-line fake
  resolver — no Jellyfin, no HTTP, no mocking framework (Chapter 9).
- **You can swap implementations without touching the core.** Want Postgres
  instead of SQLite? Write a new `Repository` implementation; `poll` doesn't
  change. The `auth.Provider` interface exists for exactly this reason — today
  there's only `GuestProvider`, but a `JellyfinProvider` can drop in later with
  no edits to the rest of the app.

If you've felt FastAPI apps drift toward "the route handler imports the ORM model
imports the settings imports..." — this layout is the antidote, and Go's
compiler keeps it honest because a bad import is a build error.

### A picture of the arrows

```
        ┌───────────────────────────────────────────────┐
        │                  cmd/main.go                   │  (imports everything)
        └───────────────────────────────────────────────┘
                 │ constructs & injects ▼
   ┌─────────────┴───────────────────────────────────────────┐
   ▼                          ▼                                ▼
internal/store ──────►  internal/poll  ◄────── internal/httpapi
(implements          (defines Repository,        (calls Service;
 Repository)          ItemResolver; pure)         defines its own views)
   ▲                          ▲
   │ implements               │ used by
internal/jellyfin ────────────┘  (via the itemResolver adapter in main)
```

Every arrow except `main`'s points *toward* `poll`. `poll` points at nobody
except tiny leaf utilities (`codes`, `voting`). That inward-pointing flow is the
architecture in one sentence.

---

## 2.3 The composition root: reading `main()` top to bottom

`func main()` is short (≈70 lines) and worth reading as a single narrative. In
FastAPI a lot of this is hidden inside the framework and `Depends`; here it's all
explicit, in one place, in execution order. Let's walk it.

```go
// 1. Load + validate configuration from the environment.
cfg, err := config.FromEnv()
if err != nil {
    log.Fatalf("config: %v", err)
}
```

`config.FromEnv()` reads every `SEEURCHIN_*` / `JELLYFIN_*` / `SEERR_*` variable,
applies defaults, and returns either a fully-populated `Config` or an error
(Jellyfin URL + key are the only hard requirements). `log.Fatalf` logs and calls
`os.Exit(1)` — fail fast, before anything is half-built. (Chapter 8 covers the
config package.)

```go
// 2. Open the database (and arrange to close it on exit).
st, err := store.Open(cfg.DBPath)
if err != nil {
    log.Fatalf("open database %q: %v", cfg.DBPath, err)
}
defer st.Close()
```

`store.Open` opens the SQLite file *and runs the schema migration* before
returning (Chapter 5). The `defer st.Close()` guarantees the DB is closed when
`main` returns — note it's declared right next to the thing it cleans up.

```go
// 3. Build the Jellyfin client and ping it (a warning, not a fatal — the
//    server can still boot if Jellyfin is briefly down).
jf := jellyfin.New(cfg.Jellyfin.URL, cfg.Jellyfin.APIKey)
pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
if err := jf.Ping(pingCtx); err != nil {
    log.Printf("warning: cannot reach Jellyfin at %s: %v", cfg.Jellyfin.URL, err)
}
cancel()
```

```go
// 4. Optionally build the Seerr client — only if it's configured.
var sr *seerr.Client
if cfg.Seerr.Enabled() {
    sr = seerr.New(cfg.Seerr.URL, cfg.Seerr.APIKey)
    log.Printf("Seerr enabled at %s ...", cfg.Seerr.URL)
}
```

`var sr *seerr.Client` declares a pointer whose zero value is `nil`. If Seerr
isn't configured, `sr` stays `nil` and is passed along as such. Throughout the
HTTP layer you'll then see `seerrEnabled()` (`return s.seerr != nil`) guarding
every Seerr code path. **A nil pointer used as an "optional dependency" is an
idiomatic Go pattern** — no feature flags, no null-object class, just `nil` and a
guard.

```go
// 5. THE WIRING: build the service, then the server, injecting dependencies.
svc := poll.NewService(st, itemResolver{jf}, 0)
sessions := auth.NewSessions(cfg.SessionSecret)
hub := httpapi.NewHub()
srv := httpapi.NewServer(cfg, svc, st, jf, sr, sessions, hub)
```

This is the heart of the composition root. Read the argument lists as the
dependency graph being assembled by hand:

- `poll.NewService(st, itemResolver{jf}, 0)` — the service receives the store
  (as a `Repository`) and a Jellyfin adapter (as an `ItemResolver`). The `0`
  means "use the default share-code length."
- `itemResolver{jf}` is a one-line **adapter**: the `itemResolver` struct
  (defined at the top of `main.go`) wraps the Jellyfin client and exposes the
  single `GetItem` method the domain's `ItemResolver` interface asks for. This is
  the seam that keeps `poll` from importing `jellyfin` — see §2.4.
- `httpapi.NewServer(...)` receives *everything* the web layer might need:
  config, the service, the repository (for read-only queries that skip the
  service), the Jellyfin + Seerr clients, the session signer, and the SSE hub. It
  just stores them on a `Server` struct (Chapter 3).

There's no magic here, and that's the point: you can read the entire dependency
graph of the application in five lines. Compare to tracing a FastAPI app's
`Depends` chain across many files.

```go
// 6. Wrap the router in an http.Server with sane timeouts.
httpServer := &http.Server{
    Addr:              cfg.Addr,
    Handler:           srv.Routes(),               // the chi router (Chapter 3)
    ReadHeaderTimeout: 10 * time.Second,
}
```

`srv.Routes()` returns the fully-built `http.Handler` (the route table). The
standard-library `http.Server` is the actual listener; chi just produces the
handler it serves.

```go
// 7. Start background workers as goroutines.
sweepCtx, stopSweeper := context.WithCancel(context.Background())
defer stopSweeper()
go srv.RunTimerSweeper(sweepCtx, time.Second)      // auto-advance expired timers

if cfg.PollRetentionDays > 0 {
    go srv.RunRetentionSweeper(sweepCtx, time.Hour) // purge old polls (opt-in)
}
```

Two long-lived background loops, each launched with `go` into its own goroutine,
each governed by `sweepCtx` so they stop cleanly on shutdown. The timer sweeper
is what makes deadlines work without any external cron — every second it checks
for polls whose round timer has expired and advances them (Chapter 4 & 7).

```go
// 8. Run the server in a goroutine, then block until a shutdown signal.
go func() {
    log.Printf("seeurchin listening on %s ...", cfg.Addr)
    if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        log.Fatalf("server: %v", err)
    }
}()

stop := make(chan os.Signal, 1)
signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
<-stop                                              // block here until Ctrl-C / SIGTERM

log.Println("shutting down...")
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
_ = httpServer.Shutdown(shutdownCtx)                // graceful: drain in-flight requests
```

This is Go's idiomatic **graceful shutdown**:

- `ListenAndServe()` *blocks* while serving, so it runs in its own goroutine.
- `main` then blocks on `<-stop` — receiving from a channel that
  `signal.Notify` feeds when the OS sends `SIGINT`/`SIGTERM`. (`make(chan ..., 1)`
  is a buffered channel of size 1 so the signal is never dropped.)
- On signal, `Shutdown` stops accepting new connections and waits up to 10s for
  in-flight requests to finish. The deferred `st.Close()` and `stopSweeper()`
  then run as `main` unwinds.

The Python equivalent would be uvicorn's lifespan + signal handling — but here
it's a dozen explicit lines you can read and reason about.

---

## 2.4 The adapter pattern: keeping `poll` free of `jellyfin`

This 16-line block at the top of `main.go` is small but conceptually important —
it's the glue that lets two packages that shouldn't know about each other
collaborate:

```go
// cmd/seeurchin/main.go
type itemResolver struct{ jf *jellyfin.Client }

func (r itemResolver) GetItem(ctx context.Context, id string) (*poll.ResolvedItem, error) {
    it, err := r.jf.GetItem(ctx, id)
    if err != nil || it == nil {
        return nil, err
    }
    return &poll.ResolvedItem{      // translate jellyfin.Item → poll.ResolvedItem
        ID:       it.ID,
        Title:    it.Name,
        Year:     it.ProductionYear,
        Type:     it.Type,
        Runtime:  it.RuntimeMinutes(),
        Overview: it.Overview,
        ImageTag: it.PrimaryImageTag(),
        Genres:   it.Genres,
    }, nil
}
```

What's happening:

- `poll` defines `ItemResolver` (one method, `GetItem`, returning `poll.ResolvedItem`).
- `jellyfin.Client` has *its own* `GetItem` returning a `jellyfin.Item` — close,
  but not the same type, and `poll` must not import `jellyfin`.
- So `main` (which is *allowed* to import both) defines a thin `itemResolver`
  that satisfies the `poll.ItemResolver` interface by calling the Jellyfin client
  and *translating* its `Item` into the domain's `ResolvedItem`.

This is the **Adapter pattern**, and it's where the dependency inversion gets
"closed." The domain says what it wants in its own vocabulary; `main` adapts the
outside world to that vocabulary. If you later add a different media backend,
you write another adapter — the domain is untouched.

> Notice the receiver is a *value* (`func (r itemResolver)`), not a pointer. The
> struct holds just one pointer field (`jf`), copying it is free, and the method
> doesn't mutate anything — so a value receiver is correct and idiomatic here.

---

## 2.5 How a feature is shaped across the layers

To make the layering concrete, here's where the responsibilities for "nominate a
title" live — one row per layer:

| Layer | File | Responsibility |
|-------|------|----------------|
| Delivery | `httpapi/handlers.go` → `handleNominate` | Decode JSON body, resolve the session → participant, branch library vs write-in, call the service, broadcast the change, respond with a view |
| Domain | `poll/service.go` → `SubmitNomination` | Enforce "round 1 only," resolve + snapshot the item, check scope/genre rules and the per-person cap, then ask the repo to record it |
| Persistence | `store/store.go` → `AddNomination` | In a transaction: find-or-create the nomination row, attach the nominator (idempotently) |
| Outbound | `jellyfin/client.go` → `GetItem` | Fetch the item details from Jellyfin to snapshot |

Each layer only talks to the next one *through an interface or a plain function
call*, and each speaks its own types (`nominateReq` → `CreatePollInput` →
`*Poll`/`ItemSnapshot` → SQL rows). No type leaks across more layers than it
should. The next chapter starts at the top of that table and works down.

[Next: the HTTP layer — chi, handlers, JSON, and views »](03-the-http-layer.md)

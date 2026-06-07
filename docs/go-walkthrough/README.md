# seeurchin backend walkthrough (for Python/FastAPI developers)

This is a guided tour of seeurchin's Go backend, written for someone who is
comfortable building HTTP APIs (routing, request/response, JSON, a database) in
Python — especially FastAPI — but is new to Go. The goal isn't to teach you Go
from zero; it's to map what you already know onto how this codebase is built,
and to point out the handful of places where Go thinks differently.

Every chapter is anchored in *real code in this repository*. Where a Go idiom
has a clean Python analogue, you'll see them side by side. Where it doesn't,
the difference is called out explicitly rather than glossed over.

## How to read this

Read it in order the first time — each chapter assumes the previous one. After
that it works as a reference: jump to the layer you're touching.

| # | Chapter | What it covers | The Go ideas you'll meet |
|---|---------|----------------|--------------------------|
| 1 | [Go for Python developers](01-go-for-python-developers.md) | A fast language primer grounded in this repo | packages, structs, interfaces, errors-as-values, pointers, slices/maps, zero values |
| 2 | [Architecture & wiring](02-architecture-and-wiring.md) | How the packages fit together and how `main` assembles them | the composition root, dependency injection by hand, the interface "seam" |
| 3 | [The HTTP layer](03-the-http-layer.md) | chi routing, handlers, JSON in/out, error translation, response "views" | `http.Handler`, middleware, struct tags, the request lifecycle |
| 4 | [The domain & state machine](04-the-domain-and-state-machine.md) | `poll.Service` — all the business rules and the round lifecycle | methods on types, the `*Error` pattern, compare-and-set concurrency |
| 5 | [Persistence with SQLite](05-persistence-with-sqlite.md) | The `Repository` interface and its `database/sql` implementation | no ORM, manual row scanning, transactions, hand-rolled migrations |
| 6 | [The voting engine](06-the-voting-engine.md) | The pluggable, self-registering voting methods | interfaces in depth, the registry pattern, optional interfaces |
| 7 | [Concurrency, sessions & SSE](07-concurrency-sessions-and-sse.md) | Live updates and signed cookies | goroutines, channels, `select`, `sync.Mutex`, HMAC |
| 8 | [External services, embedding & config](08-external-services-embedding-config.md) | The Jellyfin/Seerr clients, the embedded frontend, config from env | `net/http` clients, `context.Context`, `go:embed` |
| 9 | [Testing, building & running](09-testing-building-running.md) | How the project is tested and shipped | `go test`, table tests, `httptest`, the static binary |

## The 60-second mental model

If you only remember one diagram, make it this one — the path a request takes:

```
HTTP request
   │
   ▼
chi router  (internal/httpapi/server.go)      ← like FastAPI's APIRouter
   │
   ▼
handler     (internal/httpapi/handlers.go)    ← like a FastAPI path-operation function
   │   decode JSON, pull the session cookie
   ▼
poll.Service (internal/poll/service.go)       ← all business rules + the state machine
   │   validate, enforce, decide
   ▼
poll.Repository (interface)  ──►  internal/store (SQLite impl)   ← persistence
   │
   ▼
build a "view" struct (internal/httpapi/views.go) → JSON response
```

Two things make this layout tick, and both are pure Go:

- **`poll.Service` depends on an *interface* (`poll.Repository`), not on the
  SQLite code.** The database is plugged in from the outside. That's why the
  domain has zero `import "database/sql"`, and why tests can run against a real
  temp database without any mocking framework.
- **`main()` is the only place that knows about every concrete type.** It
  constructs the database, the Jellyfin client, the service, and the server,
  and wires them together. There is no global app object and no decorator-based
  dependency injection — the graph is assembled by hand, once, at startup. This
  is called the *composition root*, and Chapter 2 is all about it.

## What this app does (the one-paragraph product summary)

seeurchin is a self-hosted "what should we watch tonight" picker for a Jellyfin
media server. A host creates a poll and shares a 6-character code. In **round 1**
participants nominate titles from the library; in **round 2** they vote using
one of several methods (approval, ranked-choice, score, or a random pick that
skips voting entirely). Everyone sees updates live. There are no user accounts —
people join with just a display name. The whole frontend (a SvelteKit app) is
compiled and embedded *inside* the Go binary, so the deployable artifact is a
single static executable. See the top-level `README.md` and `CLAUDE.md` for the
product/feature reference; these docs are strictly about the Go code.

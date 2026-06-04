# 3 · The HTTP layer

This is the layer you'll feel most at home in — it's "the API." We'll map chi
onto FastAPI's router, see how a handler differs from a path-operation function,
how JSON gets decoded/encoded without Pydantic, how errors become status codes,
and how response "views" play the role of response models. Then we'll trace two
requests end to end.

Everything here lives in `internal/httpapi/`.

---

## 3.1 The `Server` struct holds the dependencies

There's no global `app` object. The web layer's dependencies live on a `Server`
struct, and every handler is a *method* on `*Server`, so it can reach them via
the receiver `s`:

```go
// internal/httpapi/server.go
type Server struct {
    cfg      config.Config
    svc      *poll.Service
    repo     poll.Repository      // for read-only queries that skip the service
    jf       *jellyfin.Client
    seerr    *seerr.Client        // nil when Seerr is not configured
    sessions *auth.Sessions
    hub      *Hub
}

func NewServer(cfg config.Config, svc *poll.Service, repo poll.Repository,
    jf *jellyfin.Client, sr *seerr.Client, sessions *auth.Sessions, hub *Hub) *Server {
    return &Server{cfg: cfg, svc: svc, repo: repo, jf: jf, seerr: sr, sessions: sessions, hub: hub}
}
```

Compare the FastAPI mental model: instead of pulling dependencies in per-request
with `Depends`, they're injected *once* at construction (by `main`, Chapter 2)
and held on `s`. A handler like `func (s *Server) handleVote(...)` then uses
`s.svc`, `s.repo`, `s.hub` directly. This is "constructor injection," and it's
the dominant Go style — explicit, testable, no framework.

> `&Server{...}` constructs a `Server` and returns a *pointer* to it (`*Server`).
> All handlers use a pointer receiver so they share one `Server` instance.

---

## 3.2 The router: chi vs FastAPI

`Routes()` builds the entire URL map and returns it as a single `http.Handler`.
Here's the core of it:

```go
// internal/httpapi/server.go
func (s *Server) Routes() http.Handler {
    r := chi.NewRouter()
    r.Use(middleware.RealIP)
    r.Use(middleware.Recoverer)

    r.Route("/api", func(r chi.Router) {
        r.Get("/health", s.handleHealth)
        r.Get("/features", s.handleFeatures)
        r.Get("/methods", s.handleMethods)
        r.Post("/polls", s.handleCreatePoll)
        r.Route("/polls/{code}", func(r chi.Router) {
            r.Get("/", s.handleGetPoll)
            r.Post("/join", s.handleJoin)
            r.Get("/library", s.handleLibrary)
            r.Post("/nominations", s.handleNominate)
            r.Delete("/nominations/{id}", s.handleWithdraw)
            r.Post("/advance", s.handleAdvance)
            r.Post("/votes", s.handleVote)
            r.Get("/results", s.handleResults)
            r.Get("/events", s.handleEvents)        // SSE stream
        })
        r.Get("/items/{id}/image", s.handleImage)

        r.Route("/admin", func(r chi.Router) {       // admin dashboard
            r.Get("/session", s.handleAdminSession)
            r.Post("/login", s.handleAdminLogin)
            r.Group(func(r chi.Router) {
                r.Use(s.requireAdmin)                 // middleware on just this group
                r.Get("/polls", s.handleAdminPolls)
                r.Delete("/polls/{code}", s.handleAdminDeletePoll)
            })
        })
    })

    r.Get("/p/{code}", s.handlePollPage)              // server-rendered OG preview
    r.Handle("/*", s.spaHandler())                    // everything else = the SPA
    return r
}
```

Translation table for a FastAPI dev:

| FastAPI | chi | Notes |
|---------|-----|-------|
| `app = FastAPI()` / `APIRouter()` | `chi.NewRouter()` | A router *is* the handler |
| `@router.post("/polls")` | `r.Post("/polls", s.handleCreatePoll)` | Method + path + handler, no decorator |
| `@router.get("/polls/{code}")` | `r.Get("/polls/{code}", ...)` | Same `{param}` syntax |
| `APIRouter(prefix="/api")` + `include_router` | `r.Route("/api", func(r) { ... })` | Nested sub-routers via a closure |
| `Depends(verify_admin)` on a router | `r.Group(func(r){ r.Use(s.requireAdmin); ... })` | Middleware scoped to a group |
| `path_param: str` in the signature | `chi.URLParam(r, "code")` | You pull params out manually |
| `app.add_middleware(...)` | `r.Use(middleware.RealIP)` | Order matters; runs outside-in |

The big structural differences:

- **Routes are registered imperatively, not via decorators.** `r.Post(path, fn)`
  is just a function call. Nesting is done by passing a closure to `r.Route` —
  the inner `r` is a sub-router scoped under the prefix. You can read the whole
  API surface in one function (which is genuinely nice).
- **`r.Use(...)` adds middleware.** `RealIP` rewrites the client IP from proxy
  headers; `Recoverer` catches any `panic` in a handler and turns it into a 500
  (so one bad request can't crash the process). `requireAdmin` is a *custom*
  middleware (Chapter 6/admin) applied to just the `/admin` data routes via
  `r.Group`.
- **Order of registration matters for overlap.** `/p/{code}` is registered
  *before* the `/*` catch-all so link-preview URLs are handled before falling
  through to the single-page-app handler.

### What a route handler *is*

Every handler has the exact same signature, dictated by the standard library's
`http.HandlerFunc`:

```go
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
    s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- `w http.ResponseWriter` — you *write* the response into this (status, headers,
  body). There is **no `return SomeModel`**; you imperatively write output. This
  is the single biggest difference from FastAPI, where you return a value and the
  framework serializes it.
- `r *http.Request` — the incoming request (URL, headers, body, context). The
  `_` here means "this handler ignores the request."

So a handler's job is: read from `r`, do work, write to `w`. FastAPI hides both
sides; Go hands you the raw `w`/`r` and a couple of helpers.

---

## 3.3 Decoding the request body (no Pydantic)

FastAPI binds + validates the body for you from a Pydantic model in the
signature. In Go you decode explicitly into a struct, and validation is a
separate, deliberate step (mostly down in the service layer). The pattern:

```go
// internal/httpapi/handlers.go
type voteReq struct {
    Selections map[string]int `json:"selections"`
}

func (s *Server) handleVote(w http.ResponseWriter, r *http.Request) {
    p, err := s.pollFromCode(r)              // load poll from {code}
    if err != nil { s.writeErr(w, err); return }

    me, ok := s.requireParticipant(w, r, p)  // 401 if not joined
    if !ok { return }

    var req voteReq
    if err := decodeJSON(r, &req); err != nil {
        s.writeErr(w, err)
        return
    }
    if err := s.svc.CastVotes(r.Context(), p, me, req.Selections); err != nil {
        s.writeErr(w, err)                   // service errors → right status code
        return
    }
    s.broadcast(p.ID, "votes")               // notify SSE subscribers
    s.respondView(w, r, p, me)               // 200 + the full refreshed poll view
}
```

The shared `decodeJSON` helper is worth studying — it bakes in three safety
decisions the standard library leaves to you:

```go
// internal/httpapi/server.go
func decodeJSON(r *http.Request, v any) error {
    dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))  // cap body at 1 MiB
    dec.DisallowUnknownFields()                            // reject unexpected fields
    if err := dec.Decode(v); err != nil {
        return &poll.Error{Code: http.StatusBadRequest, Msg: "invalid request body"}
    }
    return nil
}
```

- `io.LimitReader(r.Body, 1<<20)` refuses to read more than 1 MiB — a cheap
  guard against a giant body exhausting memory. (`1<<20` is `1 * 2^20` = 1 MiB; bit-shift constants are common in Go.)
- `DisallowUnknownFields()` makes decoding strict: an unknown JSON key is an
  error, like Pydantic's `model_config = {"extra": "forbid"}`.
- On any failure it returns a `*poll.Error` carrying HTTP 400, so the same
  `writeErr` machinery that handles domain errors handles malformed-body errors.

`json.NewDecoder(...).Decode(&req)` is the reflection-driven unmarshaller: it
matches JSON keys to struct fields using the `` `json:"..."` `` tags. Note
`var req voteReq` then `decodeJSON(r, &req)` — you declare the zero-valued struct,
then pass its *address* so the decoder can fill it in.

### Struct tags are the schema

The request shape is just a struct with tags. `createPollReq` is the biggest one:

```go
// internal/httpapi/handlers.go (excerpt)
type createPollReq struct {
    Title           string               `json:"title"`
    HostName        string               `json:"host_name"`
    LibraryScope    string               `json:"library_scope"`
    SubmissionRules poll.SubmissionRules `json:"submission_rules"`
    VotingMethod    string               `json:"voting_method"`
    VotingConfig    json.RawMessage      `json:"voting_config"`
    AllowGuests     bool                 `json:"allow_guests"`
    // ...
}
```

Two things to note for the Python eye:

- `json.RawMessage` is "leave this JSON un-decoded for now" — the raw bytes are
  passed straight through to the chosen voting method, which knows how to
  interpret its own config. (Chapter 6 explains why this is powerful: the HTTP
  layer never needs to know the shape of every voting method's config.)
- There's **no validation in the struct tags**. No `min_length`, no regex. All
  validation happens in `poll.Service` (`CreatePoll` checks the title is
  non-empty, the scope is valid, the rules are coherent, etc.). The HTTP layer's
  job is *transport*; the domain's job is *rules*. Keeping them separate is why
  the rules are unit-testable without HTTP.

---

## 3.4 Encoding the response: views as response models

Handlers don't return domain objects directly. They build a **view** struct —
the API's stable, public shape — and serialize that. Views live in
`internal/httpapi/views.go` and are the equivalent of FastAPI `response_model`
classes. This separation means you can refactor the `poll.Poll` domain struct
without breaking the wire format, and you control exactly what's exposed.

The writer helper is tiny:

```go
// internal/httpapi/server.go
func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    if v != nil {
        _ = json.NewEncoder(w).Encode(v)
    }
}
```

Note the *order*: set headers, then `WriteHeader(status)`, then write the body.
Once you call `WriteHeader` (or write any body), the status is locked in — a
common Go footgun is trying to change the status after the body has started.

The main view is `pollView`, assembled by `buildPollView` (views.go). It pulls
together participants, nominations, "me" (the caller's own state), optional
results, the timer, and a server timestamp — into one JSON object the SPA
renders. A couple of idioms worth calling out:

```go
// views.go — ensure empty collections serialize as [] not null
Nominations: []nominationView{},   // initialized empty on purpose
// ...
Genres: genresOrEmpty(p.Genres),   // helper returns []string{} when nil
```

Recall from Chapter 1 that a `nil` slice marshals to JSON `null`, which would
force every frontend consumer to null-check. Initializing to an empty slice
makes the contract "always an array." You'll see this defensive habit throughout
the view-building code.

The `meView` is a nice example of *computing* a tailored sub-object per caller —
"how many have I nominated, have I voted, what were my selections" — none of which
exists on the domain `Poll`; it's derived at view-build time:

```go
view.Me = &meView{
    ID:              me.ID,
    DisplayName:     me.DisplayName,
    IsHost:          me.IsHost(),
    NominationCount: count,
    HasVoted:        voted,
}
```

`view.Me` is a `*meView` (pointer) so it can be `nil` for a visitor who hasn't
joined — and `` `json:"me"` `` with a pointer serializes `null`, which the SPA
reads as "you're not a participant yet."

---

## 3.5 Errors → HTTP status codes

We met this in Chapter 1; here's how it plays out across the whole layer. The
domain returns rich errors; one function maps them to responses:

```go
// internal/httpapi/server.go
func (s *Server) writeErr(w http.ResponseWriter, err error) {
    var pe *poll.Error
    switch {
    case errors.As(err, &pe):                    // a business-rule error w/ a status
        s.writeJSON(w, pe.Code, errResp{pe.Msg})
    case poll.IsNotFound(err):                    // a "missing entity" sentinel
        s.writeJSON(w, http.StatusNotFound, errResp{"not found"})
    default:                                      // anything unexpected
        log.Printf("internal error: %v", err)    // log the detail server-side
        s.writeJSON(w, http.StatusInternalServerError, errResp{"internal error"})
    }
}
```

The contract this creates:

- A `*poll.Error` (built by `errBad`/`errForbid`/`errConflict` in the service)
  carries its own HTTP status — so "you can nominate at most 3" naturally becomes
  a `400`, "only the host can advance" a `403`, "nominations are closed" a `409`.
  The handler just calls `writeErr(w, err)` and the right code comes out.
- `poll.ErrNotFound` is a **sentinel error** (a single package-level value);
  `poll.IsNotFound` checks for it with `errors.Is`. This is the Go pattern for
  "a specific, recognizable error" — like catching a specific exception class.
- Anything else is treated as a bug/infra failure: logged in full server-side,
  but reported to the client as a bland `500` with no internal detail. Good
  security hygiene, for free, in one place.

Because *every* handler funnels failures through `writeErr`, error handling is
uniform and you never repeat status-code logic. The handler body stays a clean
sequence of `do X; if err != nil { writeErr; return }`.

---

## 3.6 Sessions → "who is this request?"

There are no user accounts. Identity is a signed cookie that maps each poll's ID
to a per-participant session token. The HTTP layer turns that cookie into a
`*poll.Participant`. Two small helpers do it (full detail in Chapter 7):

```go
// internal/httpapi/server.go
func (s *Server) currentParticipant(r *http.Request, p *poll.Poll) *poll.Participant {
    token := s.readTokens(r)[p.ID]          // decode the signed cookie → map, index by poll
    if token == "" {
        return nil
    }
    part, err := s.repo.GetParticipantBySession(r.Context(), p.ID, token)
    if err != nil {
        return nil
    }
    return part
}

func (s *Server) requireParticipant(w http.ResponseWriter, r *http.Request, p *poll.Poll) (*poll.Participant, bool) {
    me := s.currentParticipant(r, p)
    if me == nil {
        s.writeJSON(w, http.StatusUnauthorized, errResp{"join the poll first"})
        return nil, false
    }
    return me, true
}
```

`requireParticipant` is the manual equivalent of a FastAPI auth dependency: it
either yields the participant or writes a 401 and signals "stop" via the second
return value. The caller's `me, ok := s.requireParticipant(...); if !ok { return }`
is the idiom you'll see at the top of nearly every authenticated handler.

The cookie itself holds a *map* (`pollID → token`), so one browser can be host of
one poll and a guest in another simultaneously. `setToken` (also in server.go)
reads the existing map, adds/updates the entry for this poll, re-signs, and sets
the cookie — preserving membership in other polls.

---

## 3.7 Two requests, end to end

### A) `POST /api/polls` — create a poll

1. **Route** → `handleCreatePoll` (handlers.go).
2. **Decode** the body into `createPollReq` via `decodeJSON` (strict, size-limited).
3. **Translate** the request struct into a domain `poll.CreatePollInput` and call
   `s.svc.CreatePoll(ctx, in)`. The service validates everything, generates a
   unique share code, builds the `Poll` and the host `Participant`, and persists
   them in one transaction (Chapter 4 & 5). It returns the poll *and* the host
   (whose `SessionToken` the client must receive as a cookie).
4. **Set the cookie**: `s.setToken(w, r, p.ID, host.SessionToken)` — now the
   creator is authenticated as the host.
5. **Build + write the view**: `buildPollView(ctx, p, host)` → `writeJSON(w, 201, view)`.

Notice the handler is ~20 lines and contains *zero* business logic — it's pure
plumbing between JSON and the service. That's the layering working as intended.

### B) `POST /api/polls/{code}/nominations` — nominate a title

1. **Route** → `handleNominate`. Load the poll (`pollFromCode`, which
   *normalizes* the user-typed code first — see `codes.Normalize`), then
   `requireParticipant` (401 if not joined).
2. **Decode** `nominateReq { item_id, tmdb_id, media_type }`.
3. **Branch**: a non-zero `tmdb_id` means a *write-in* (a title not in the
   library) — only valid when Seerr is enabled; the handler resolves it via
   Seerr, dedupe-checks it against the real library, then calls
   `s.svc.SubmitWriteIn`. Otherwise it's a library item → `s.svc.SubmitNomination`.
4. **Broadcast**: `s.broadcast(p.ID, "nominations")` pushes a one-word event to
   every SSE subscriber of this poll, who then refetch (Chapter 7).
5. **Respond** with the refreshed `pollView`.

This handler is meatier because it *coordinates* (Seerr lookup, library
dup-check, branching) — but the actual *rules* (round must be open, scope/genre
gating, per-person cap, snapshotting) still live in the service. The handler
decides *which* service call to make and what to do around it; the service
decides whether it's *allowed*.

---

## 3.8 Serving the frontend from the same binary

The last route, `r.Handle("/*", s.spaHandler())`, serves the compiled SvelteKit
app that's *embedded inside the binary*. We'll cover `go:embed` in Chapter 8, but
the routing-relevant idea is the **single-page-app fallback**:

```go
// internal/httpapi/web.go (core of spaHandler)
if f, err := sub.Open(name); err == nil {   // a real built asset exists?
    f.Close()
    fileServer.ServeHTTP(w, r)               // serve the file
    return
}
// Unknown path: serve index.html so client-side routing can take over.
index, _ := sub.Open("index.html")
io.Copy(w, index)
```

Any path that isn't a real file (e.g. `/p/ABC123` loaded fresh) returns
`index.html`, and the Svelte router renders the right view client-side. This is
why one Go binary serves both the JSON API *and* the web app, with no nginx in
front.

[Next: the domain service and the poll state machine »](04-the-domain-and-state-machine.md)

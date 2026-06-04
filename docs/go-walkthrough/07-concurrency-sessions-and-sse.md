# 7 · Concurrency, sessions & SSE

This chapter covers the two places the app does things a typical synchronous
FastAPI app doesn't: **real concurrency** (multiple goroutines sharing state) and
**a live push stream** (Server-Sent Events). We'll also cover the signed-cookie
session scheme, since it's small and self-contained. These are the parts where
Go's built-in concurrency stops being trivia and starts being load-bearing.

---

## 7.1 The concurrency model you're actually running under

A crucial fact first: **the Go `net/http` server runs every request in its own
goroutine.** There's no single event loop and no GIL. Ten simultaneous requests
are ten goroutines, potentially on multiple CPU cores, *truly* in parallel. Add
the background timer sweeper (its own goroutine) and the SSE streams (one
goroutine each, blocked for minutes), and you have many goroutines touching some
shared structures at once.

That changes what you must worry about, compared to default sync Python:

- Shared mutable state (a map several requests can write) needs a **lock**, or
  it's a data race (Go even ships a race detector: `go test -race`).
- "Read-modify-write" on the database needs to be **atomic** (Chapter 4's
  compare-and-set), because two goroutines can interleave.
- Long-lived streams must notice when the **client disconnects**, or they leak.

The app handles all three deliberately. Let's see each.

---

## 7.2 Server-Sent Events: the live-update mechanism

seeurchin updates everyone's screen in real time — nominate a title and everyone
sees it appear. The transport is **SSE**: a long-lived HTTP response that the
server keeps writing little text events into. It's simpler than WebSockets and
one-directional (server → client), which is all this app needs.

The design (described in `CLAUDE.md`) is intentionally minimal: **events carry
only a type string; clients refetch full state on receipt.** The server never
diffs or streams the actual data — it just taps the client on the shoulder and
says "nominations changed, go refetch." That keeps the streaming code tiny and
the source of truth singular (the normal GET endpoint).

### The `Hub`: a fan-out registry guarded by a mutex

```go
// internal/httpapi/sse.go
type Hub struct {
    mu   sync.Mutex
    subs map[string]map[chan []byte]struct{}   // pollID → set of subscriber channels
}
```

Read the type of `subs` carefully — it's the data structure that makes fan-out
work:

- Outer map: `pollID → subscribers`. Each poll has its own audience.
- Inner `map[chan []byte]struct{}`: a **set of channels**, one per connected
  browser watching that poll. (`struct{}` values = the "map as a set" idiom from
  Chapter 1.) Each channel is the pipe to one SSE connection.

`sync.Mutex` (`mu`) guards the map because multiple goroutines mutate it
concurrently: SSE handlers subscribing/unsubscribing, and broadcasters iterating
it. Every access takes the lock:

```go
// internal/httpapi/sse.go — subscribe
func (h *Hub) Subscribe(pollID string) (<-chan []byte, func()) {
    ch := make(chan []byte, 8)            // buffered: tolerate a brief slow reader
    h.mu.Lock()
    if h.subs[pollID] == nil {
        h.subs[pollID] = map[chan []byte]struct{}{}
    }
    h.subs[pollID][ch] = struct{}{}
    h.mu.Unlock()

    var once sync.Once                    // ensure cleanup runs exactly once
    cancel := func() {
        once.Do(func() {
            h.mu.Lock()
            if set, ok := h.subs[pollID]; ok {
                delete(set, ch)
                if len(set) == 0 { delete(h.subs, pollID) }   // last one out: tidy up
            }
            h.mu.Unlock()
            close(ch)
        })
    }
    return ch, cancel
}
```

Several idioms packed in here:

- **`Subscribe` returns the channel *and* a `cancel` closure.** The caller defers
  `cancel()` so unsubscription is automatic when the handler returns. Returning a
  cleanup function alongside a resource is a very common Go API shape (you've seen
  it with `context.WithCancel`).
- **`<-chan []byte`** in the return type is a *receive-only* channel — the
  subscriber can read but not send. Channel direction in the type is a compile-
  time guardrail.
- **`make(chan []byte, 8)`** is a *buffered* channel (capacity 8). A slow client
  can fall up to 8 messages behind before the publisher gives up on it (next).
- **`sync.Once`** guarantees the cleanup body runs at most once, even if `cancel`
  is somehow called twice — defensive, cheap, idiomatic.

### Publishing: never block on a slow client

```go
// internal/httpapi/sse.go
func (h *Hub) publish(pollID string, data []byte) {
    h.mu.Lock()
    defer h.mu.Unlock()
    for ch := range h.subs[pollID] {
        select {
        case ch <- data:        // try to send
        default:                // buffer full → drop it, don't block the others
        }
    }
}
```

This `select` with a `default` case is the **non-blocking send** idiom. `ch <- data`
would block if the channel's buffer is full; the `default` makes the whole
`select` fall through instantly instead. So one wedged subscriber can't stall the
broadcast to everyone else — its message is simply dropped. Because clients
refetch on *any* event, a dropped "nominations" tap is harmless: the next event,
or the periodic refetch, brings it current. **"Lossy but live" is a deliberate
trade** that keeps the hub from ever blocking.

The friendly wrapper handlers call is `broadcast`:

```go
// internal/httpapi/sse.go
func (s *Server) broadcast(pollID, eventType string) {
    data, _ := json.Marshal(map[string]string{"type": eventType})
    s.hub.publish(pollID, data)
}
```

That's why throughout `handlers.go` you see `s.broadcast(p.ID, "nominations")`
after a nomination, `s.broadcast(p.ID, "votes")` after a vote, `"status"` after
an advance. One line, fire-and-forget.

### The SSE handler: a `select` loop that lives for minutes

```go
// internal/httpapi/sse.go (core)
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
    p, err := s.pollFromCode(r)
    if err != nil { s.writeErr(w, err); return }
    flusher, ok := w.(http.Flusher)         // need streaming support
    if !ok { http.Error(w, "streaming unsupported", 500); return }

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    ch, cancel := s.hub.Subscribe(p.ID)
    defer cancel()                          // unsubscribe when the handler returns

    io.WriteString(w, "event: ready\ndata: {}\n\n")
    flusher.Flush()

    keepalive := time.NewTicker(25 * time.Second)
    defer keepalive.Stop()
    ctx := r.Context()
    for {
        select {
        case <-ctx.Done():                  // client disconnected / request cancelled
            return
        case msg, ok := <-ch:               // hub pushed an event
            if !ok { return }               // channel closed → we were unsubscribed
            io.WriteString(w, "event: update\ndata: ")
            w.Write(msg)
            io.WriteString(w, "\n\n")
            flusher.Flush()
        case <-keepalive.C:                 // 25s passed with no events
            io.WriteString(w, ": keepalive\n\n")   // SSE comment line
            flusher.Flush()
        }
    }
}
```

This is the canonical Go "stream until done" loop, and it multiplexes three
event sources with one `select`:

1. **`<-ctx.Done()`** — the request's context is cancelled when the browser
   closes the tab or the connection drops. This is how the handler *knows* to
   exit (and via the deferred `cancel()`, to unsubscribe and free the channel).
   No client polling, no leaked goroutine. This is the concrete payoff of the
   `context.Context` threading from Chapter 1.
2. **`<-ch`** — a real event from the hub: format it as SSE (`event: update\ndata: ...\n\n`)
   and `Flush()` so it's sent immediately rather than buffered.
3. **`<-keepalive.C`** — a `time.Ticker` fires every 25s; the handler writes an
   SSE comment (`: keepalive`) to keep proxies and load balancers from killing an
   idle connection.

`w.(http.Flusher)` is another **type assertion** (Chapter 6): not every
`ResponseWriter` can flush mid-response, so the handler asks "does this writer
support streaming?" and bails cleanly if not. And `flusher.Flush()` is essential
— without it, the standard library would buffer the writes and the client would
see nothing until the response ended (which, for a stream, is never).

### The caveat, stated plainly

The hub is **in-memory and single-process** — it's a Go map living in this one
server. That's why `main` starts the timer sweeper exactly once and why
`CLAUDE.md` says the app is "not multi-instance safe." If you ran two replicas
behind a load balancer, a broadcast on replica A wouldn't reach an SSE client
connected to replica B. Scaling out would mean moving fan-out to Redis pub/sub or
similar. For a self-hosted single container, the in-memory hub is exactly right —
simple, fast, zero dependencies — and the design is honest about the boundary.

---

## 7.3 The background sweeper, revisited as concurrency

We met `SweepDueTimers` in Chapter 4 (the domain logic). Here's its driver, which
is pure concurrency plumbing:

```go
// internal/httpapi/timer.go
func (s *Server) RunTimerSweeper(ctx context.Context, interval time.Duration) {
    t := time.NewTicker(interval)
    defer t.Stop()
    for {
        select {
        case <-ctx.Done():               // shutdown: stop the loop
            return
        case now := <-t.C:               // ticker fired (every `interval`)
            changed, err := s.svc.SweepDueTimers(ctx, now)
            if err != nil { log.Printf("timer sweep: %v", err) }
            for _, p := range changed {
                s.broadcast(p.ID, "status")          // tell clients to refetch
                if p.Status == poll.StatusClosed {
                    s.autoRequestWinners(ctx, p)     // fire Seerr request for a winner
                }
            }
        }
    }
}
```

Same `for { select { ... } }` shape as the SSE handler — that pattern is *the*
Go idiom for "a long-running loop that reacts to a few channels and a cancellation
signal." Launched with `go srv.RunTimerSweeper(sweepCtx, time.Second)` in `main`,
it runs for the life of the process and stops the instant `sweepCtx` is cancelled
during shutdown. The `time.Ticker` is Go's repeating timer; `t.C` is the channel
it sends ticks on; `defer t.Stop()` releases it.

Notice how cleanly the layers cooperate even across a goroutine boundary: the
sweeper (HTTP layer) calls `SweepDueTimers` (domain), gets back the list of polls
that changed, and then does the *delivery-side* side effects (broadcast SSE, fire
Seerr). The domain decided *what* happened; the HTTP layer decided *who to tell*.

`RunRetentionSweeper` (admin.go, hourly) has the same structure, with one twist —
it runs the purge *first* and then waits, so an instance configured for retention
cleans up immediately on boot rather than after the first hour.

---

## 7.4 Sessions: a signed cookie, no server-side store

Identity is deliberately stateless: there's no session table, no Redis. The
server hands each participant a random token at join time, and the browser's
cookie carries a **map of `pollID → token`**, signed so it can't be tampered
with. The whole scheme is in `internal/auth/sessions.go` and it's only ~90 lines.

```go
// internal/auth/sessions.go
type Sessions struct {
    secret []byte                         // the HMAC key, from config
}

func (s *Sessions) Encode(tokens map[string]string) (string, error) {
    payload, _ := json.Marshal(tokens)
    body := base64.RawURLEncoding.EncodeToString(payload)
    return body + "." + s.sign(body), nil       // "<base64(json)>.<base64(hmac)>"
}

func (s *Sessions) Decode(cookie string) (map[string]string, error) {
    i := strings.LastIndexByte(cookie, '.')
    if i < 0 { return nil, ErrInvalidCookie }
    body, sig := cookie[:i], cookie[i+1:]
    if !hmac.Equal([]byte(sig), []byte(s.sign(body))) {   // verify before trusting
        return nil, ErrInvalidCookie
    }
    payload, _ := base64.RawURLEncoding.DecodeString(body)
    tokens := map[string]string{}
    json.Unmarshal(payload, &tokens)
    return tokens, nil
}

func (s *Sessions) sign(body string) string {
    mac := hmac.New(sha256.New, s.secret)
    mac.Write([]byte(body))
    return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
```

The mechanism is a classic **HMAC-signed token** (think a hand-rolled, minimal
JWT):

- The cookie value is `base64(json) + "." + base64(HMAC-SHA256(base64(json)))`.
  The payload isn't encrypted — it's just the poll→token map — but it *is* signed.
- On read, `Decode` recomputes the HMAC over the body and compares. If they don't
  match, the cookie was tampered with (or signed by a different secret) and is
  rejected. **`hmac.Equal` is a constant-time comparison** — it doesn't short-
  circuit on the first differing byte, which prevents timing attacks. Using `==`
  here would be a subtle security bug; the standard library gives you the right
  tool.
- The secret comes from `SEEURCHIN_SESSION_SECRET` (or a random ephemeral one,
  with a startup warning that sessions won't survive a restart — Chapter 8).

Why a *map* in the cookie? So one browser can hold multiple roles at once — host
of poll A, guest of poll B — each with its own token. The HTTP layer's `setToken`
(Chapter 3) reads the existing map, updates one entry, and re-signs, preserving
the others.

The cookie is set with the security flags you'd hope for:

```go
// internal/httpapi/server.go — setToken
http.SetCookie(w, &http.Cookie{
    Name:     cookieName,                 // "seeurchin_session"
    Value:    val,
    Path:     "/",
    HttpOnly: true,                        // JS can't read it (XSS mitigation)
    SameSite: http.SameSiteLaxMode,
    Secure:   strings.HasPrefix(s.cfg.BaseURL, "https"),  // HTTPS-only when deployed over TLS
    MaxAge:   60 * 60 * 24 * 30,           // 30 days
})
```

### The admin cookie: same primitive, different shape

The admin dashboard reuses the same `Sessions` signer with a single-value helper
(`SignValue`/`VerifyValue`) instead of a map. Worth a glance for one nice security
detail (`internal/httpapi/admin.go`):

```go
func adminFingerprint(token string) string {
    sum := sha256.Sum256([]byte("seeurchin-admin:" + token))
    return hex.EncodeToString(sum[:])
}
```

The admin cookie stores a *fingerprint* (a salted hash) of the current admin
token, signed. A request is authenticated only when its cookie's fingerprint
matches the fingerprint of the *current* `SEEURCHIN_ADMIN_TOKEN`. The payoff:
**rotating the env token instantly invalidates every outstanding admin session**,
with no server-side session list to clear. Login itself uses
`subtle.ConstantTimeCompare` to check the submitted token — constant-time again,
same reasoning as `hmac.Equal`.

---

## 7.5 The concurrency cheat-sheet

Everything in this chapter reduces to a handful of primitives. Here they are,
mapped to what you might reach for in Python:

| Go primitive | What it is | Rough Python analogue |
|--------------|-----------|------------------------|
| `go f()` | start a goroutine | `asyncio.create_task` / `threading.Thread` |
| `chan T` | typed pipe between goroutines | `asyncio.Queue` |
| `select { case ...: }` | wait on several channel ops | `asyncio.wait(FIRST_COMPLETED)` |
| `select { ...; default: }` | non-blocking channel op | `queue.put_nowait` + `except Full` |
| `sync.Mutex` | mutual exclusion for shared state | `threading.Lock` |
| `sync.Once` | run something exactly once | a guarded flag |
| `time.Ticker` / `time.NewTimer` | repeating / one-shot timer channel | `asyncio` loop + sleep |
| `context.Context` | cancellation + deadline propagation | cancellation token + timeout |

The mental upgrade from sync Python is mainly: *state shared across goroutines is
real and must be guarded*, and *long-lived work must watch `ctx.Done()` so it
shuts down cleanly*. This codebase models both well in a small space — the Hub is
a great little reference implementation to come back to.

[Next: external clients, embedding the frontend, and configuration »](08-external-services-embedding-config.md)

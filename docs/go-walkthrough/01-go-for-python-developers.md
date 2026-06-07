# 1 · Go for Python developers

A whirlwind tour of the Go you need to read this codebase, every point tied to a
real snippet from the repo. If you've written FastAPI, you already understand
~80% of the *shape* of this app; this chapter is about the other 20% — the
syntax and semantics that differ.

> Read this once for orientation. You don't need to memorize it — later chapters
> re-explain each idea in context the first time it matters.

---

## 1.1 The shape of a Go program

### Packages, not modules-as-files

In Python a *module* is one `.py` file and a *package* is a directory with
`__init__.py`. In Go, a **package** is a *directory of `.go` files that all
declare the same `package` name on line 1**. Every file in `internal/poll/`
starts with `package poll`:

```go
// internal/poll/types.go, service.go, repository.go all start with:
package poll
```

Those three files are one package. Within a package there are no imports between
files — `service.go` can call a function defined in `types.go` directly, as if
they were one file. There is no `from .types import Poll`.

The whole repo is one **module**, declared in `go.mod`:

```go
module github.com/enderu/seeurchin   // go.mod line 1
go 1.25.0
```

That module path is the import prefix. When `main.go` wants the poll package it
writes the full path:

```go
import "github.com/enderu/seeurchin/internal/poll"
```

The special directory name `internal/` is enforced by the compiler: packages
under `internal/` can only be imported by code rooted at the same parent. So
nothing outside this repo can import `internal/poll`. It's Go's version of
"this is private API," at the directory level.

### Exported = Capitalized

This is the single most surprising rule for newcomers. **Go has no `public`/
`private` keyword and no leading-underscore convention. Visibility is decided by
the case of the first letter of the identifier.**

```go
type Poll struct { ... }   // Exported: visible to other packages
type approvalConfig struct { ... }   // unexported: package-private

func NewService(...) *Service { ... }  // exported
func validateRules(r SubmissionRules) error { ... }  // unexported
```

Same rule for struct *fields*: `Poll.Title` (capital T) is visible everywhere;
a lowercase field would be invisible outside its package. This is why every
field on the `Poll` struct that needs to be serialized to JSON is capitalized.

| Python | Go |
|--------|-----|
| `def _helper():` (convention only) | `func helper()` (compiler-enforced) |
| `class Poll:` (public by convention) | `type Poll struct{}` (capital = exported) |
| `from x import y` | `import "module/path/x"`, then `x.Y` |

### `func main()` is the entry point

There's no `if __name__ == "__main__":`. A program is a package named `main`
that contains a `func main()`. Here it lives in `cmd/seeurchin/main.go`. The
`cmd/<name>/` layout is the standard Go convention for "this directory builds an
executable"; everything reusable lives under `internal/` or `pkg/`.

---

## 1.2 Types, structs, and "methods"

Go has no classes. You define a **struct** (a bundle of fields) and attach
**methods** to it separately. Compare a small domain type:

```python
# Python
from dataclasses import dataclass

@dataclass
class Participant:
    id: str
    display_name: str
    role: str

    def is_host(self) -> bool:
        return self.role == "host"
```

```go
// internal/poll/types.go
type Participant struct {
    ID           string    `json:"id"`
    PollID       string    `json:"poll_id"`
    DisplayName  string    `json:"display_name"`
    SessionToken string    `json:"-"`
    Role         string    `json:"role"`
    CreatedAt    time.Time `json:"created_at"`
}

// IsHost reports whether the participant is the poll host.
func (p Participant) IsHost() bool { return p.Role == RoleHost }
```

Things to notice:

- **The method is declared outside the struct.** `func (p Participant) IsHost()`
  — the `(p Participant)` part is the **receiver**, Go's equivalent of `self`.
  You choose its name (`p`, `s`, `c` — short, by convention).
- **`` `json:"id"` `` is a struct tag.** It's metadata attached to a field,
  read at runtime via reflection by the `encoding/json` package. This is exactly
  the role Pydantic's `Field(alias=...)` plays, except it's a raw string
  annotation rather than a typed object. `` `json:"-"` `` means "never serialize
  this field" — see `SessionToken`, which must never reach the client.
- **Methods can live on any named type, not just structs.** In `types.go`,
  `Status` is just a string, but it has constants; in `service.go`,
  `SubmissionRules` (a struct) has an `effectiveMax()` method. A method is just
  a function with a receiver.

### Value receiver vs pointer receiver

`func (p Participant) IsHost()` takes `p` **by value** — a copy. Reading is fine.
But when a method needs to *mutate* the struct, or the struct is large, or you
need to compare against `nil`, you take a **pointer receiver**:

```go
// internal/poll/types.go — mutating helper, pointer receiver (*Poll)
func (p *Poll) setActiveClosesAt(t *time.Time) {
    switch p.Status {
    case StatusRound1:
        p.Round1ClosesAt = t   // writes back to the caller's struct
    case StatusRound2:
        p.Round2ClosesAt = t
    }
}
```

`*Poll` means "pointer to a Poll." Mutations through a pointer are visible to the
caller; mutations through a value receiver are lost when the copy is discarded.
Rule of thumb you'll see throughout: **if it mutates or the type is non-trivial,
use a pointer receiver.** Pure read helpers like `IsHost` use a value receiver.

---

## 1.3 Pointers, briefly and without fear

Go has pointers, but none of C's arithmetic or manual memory management (there's
a garbage collector). You'll meet pointers in three everyday roles:

1. **"This struct, not a copy of it."** Functions return `*Poll` so the caller
   gets the actual object and can be handed a `nil` to mean "not found":

   ```go
   func (s *Store) GetPollByCode(ctx context.Context, code string) (*poll.Poll, error)
   ```

2. **Optional / nullable values.** Go has no `Optional[T]`. A pointer that can be
   `nil` *is* the optional. The `Poll` struct uses `*time.Time` for timer
   deadlines precisely because "no deadline" must be representable:

   ```go
   Round1ClosesAt *time.Time `json:"round1_closes_at,omitempty"`  // nil = no timer
   ```

   This is the moral equivalent of `round1_closes_at: datetime | None` in
   Pydantic. `&t` takes the address of `t` (making a pointer); `*closesAt`
   dereferences one (reading the value).

3. **Avoiding copies of big structs.** Passing `*Poll` around is cheaper than
   copying the whole thing, and means everyone sees the same instance.

You will rarely *think* about pointers here; just read `*T` as "a T that might be
absent / that I can mutate / that I'm sharing."

---

## 1.4 Errors are values, not exceptions

This is the biggest day-to-day difference from Python, and it's everywhere.

**Go has no `try`/`except` for normal error handling.** Functions that can fail
return an `error` as their *last return value*, and the caller checks it
immediately. A `nil` error means success.

```python
# Python: raise + try/except
def get_item(item_id: str) -> Item:
    resp = httpx.get(...)
    resp.raise_for_status()   # raises on failure, unwinds the stack
    return Item(**resp.json())
```

```go
// Go: return (value, error); check err right here
item, err := s.items.GetItem(ctx, itemID)
if err != nil {
    return nil, err          // pass it up, explicitly
}
if item == nil {
    return nil, errBad("that title is not in the library")
}
```

You will see the `if err != nil { return ..., err }` block constantly. It's
verbose on purpose: the control flow is visible, there are no invisible
stack-unwinds, and you can't *forget* a failure path silently — the compiler
makes you assign or explicitly discard every return value.

### Multiple return values

`item, err := ...` is **multiple return values**, a first-class Go feature (not a
tuple you unpack). The convention is `(result, error)`, or sometimes
`(result, ok bool)` for lookups that can simply "miss":

```go
// internal/voting/voting.go — the comma-ok idiom
func Get(key string) (Method, bool) {
    m, ok := registry[key]   // map lookup returns (value, found?)
    return m, ok
}

// caller:
method, ok := voting.Get(p.VotingMethod)
if !ok {
    return nil, nil, errBad("unknown voting method %q", in.VotingMethod)
}
```

`:=` is **declare-and-assign** (infers the type, like a Python assignment that
also declares). `=` is plain assignment to already-declared variables.

### Custom error types

An `error` is just *any value with an `Error() string` method* — it's an
interface (more on those next). This repo defines its own so handlers can turn a
business-rule failure into the right HTTP status:

```go
// internal/poll/service.go
type Error struct {
    Code int
    Msg  string
}
func (e *Error) Error() string { return e.Msg }   // now *Error satisfies `error`

func errBad(format string, a ...any) *Error {
    return &Error{Code: 400, Msg: fmt.Sprintf(format, a...)}
}
```

And the HTTP layer pulls the code back out with `errors.As` (Go's typed
error-matching, the analogue of `except MyError as e:`):

```go
// internal/httpapi/server.go
func (s *Server) writeErr(w http.ResponseWriter, err error) {
    var pe *poll.Error
    switch {
    case errors.As(err, &pe):                       // is this (wrapping) a *poll.Error?
        s.writeJSON(w, pe.Code, errResp{pe.Msg})
    case poll.IsNotFound(err):
        s.writeJSON(w, http.StatusNotFound, errResp{"not found"})
    default:
        log.Printf("internal error: %v", err)       // unexpected: log it, hide details
        s.writeJSON(w, http.StatusInternalServerError, errResp{"internal error"})
    }
}
```

That `switch { case <bool>: }` with no value after `switch` is just a tidy
`if/else if/else` chain — a common Go style.

> **`panic` exists** (it's the closest thing to an unchecked exception), but it's
> reserved for *programmer errors that should never happen*, not control flow.
> See `NewID()` in `types.go`: if the OS random source fails, the program can't
> sensibly continue, so it panics. The HTTP layer also installs a `Recoverer`
> middleware so a stray panic in a handler becomes a 500 instead of crashing the
> server.

---

## 1.5 Interfaces: duck typing, made explicit and checked

If one Go concept unlocks this codebase, it's interfaces. You already know duck
typing from Python ("if it has `.read()`, it's file-like"). Go interfaces are
duck typing that the *compiler* verifies.

An interface is **a set of method signatures**. Any type that has those methods
*automatically* satisfies the interface — there is **no `implements` keyword and
no explicit inheritance**. This is called *structural* typing.

```go
// internal/poll/repository.go (trimmed) — the persistence contract
type Repository interface {
    CreatePoll(ctx context.Context, p *Poll, host *Participant) error
    GetPollByCode(ctx context.Context, code string) (*Poll, error)
    ListNominations(ctx context.Context, pollID string) ([]Nomination, error)
    // ...~25 methods total
}
```

The `*store.Store` type (in `internal/store`) has methods with exactly these
signatures, so it *is* a `poll.Repository` — without ever naming the interface.
The service only ever sees the interface:

```go
// internal/poll/service.go
type Service struct {
    repo  Repository      // an interface, not *store.Store
    items ItemResolver    // another interface
}
```

Why this matters for you, the FastAPI dev:

- **It replaces dependency-injection frameworks.** In FastAPI you might wire a
  repository in with `Depends(get_repo)`. Here, `Service` declares "I need
  *something* that satisfies `Repository`," and `main()` hands it the real
  SQLite store. Tests hand it a real temp-file store too — but they *could* hand
  it any stand-in with the right methods, no mocking library required.
- **The domain doesn't depend on the database.** `internal/poll` defines the
  `Repository` interface and never imports `internal/store`. The dependency
  arrow points *inward* (store → poll), which is what keeps the business logic
  pure and testable. Chapter 2 expands on this.

### Small interfaces are idiomatic

The most idiomatic Go interfaces are tiny — often one method. `ItemResolver`
(what the service needs from Jellyfin) is exactly one method:

```go
// internal/poll/service.go
type ItemResolver interface {
    GetItem(ctx context.Context, id string) (*ResolvedItem, error)
}
```

`main.go` defines a 12-line adapter (`itemResolver`) that wraps the real Jellyfin
client to fit this one-method shape. The domain asks for the *narrowest* thing it
needs, not "the Jellyfin client." Chapter 6 shows the richer multi-method
`voting.Method` interface and the *plugin registry* built on top of it.

### Compile-time conformance checks

You'll spot lines like this:

```go
var _ AutoDecider = Random{}      // internal/voting/random.go
var _ Provider = GuestProvider{}  // internal/auth/provider.go
```

That's a Go idiom meaning "fail the build right here if `Random` ever stops
satisfying `AutoDecider`." `_` is the **blank identifier** — "assign this to
nothing, I only care about the type-check." You'll see `_` a lot: for ignored
return values (`x, _ := ...`), ignored loop variables, and import side effects
(`import _ "modernc.org/sqlite"` in `store.go` registers the driver without
naming it — exactly like importing a module purely for its side effects).

---

## 1.6 Collections: slices and maps

Two built-in collections cover almost everything.

### Slices ≈ Python lists

A **slice** (`[]T`) is a growable, ordered sequence of `T`. `append` is the
workhorse (note: it *returns* the possibly-reallocated slice, so you reassign):

```go
out := make([]libraryItem, 0, len(items))   // empty slice, capacity hint
for _, it := range items {
    out = append(out, libraryItem{ ... })   // out = append(out, x), always reassign
}
```

`range` is `for ... in ...`, but it yields **index and value**: `for i, v := range xs`.
Don't need the index? Use `_`: `for _, v := range xs`. Need only the index?
`for i := range xs`.

### Maps ≈ Python dicts

A **map** (`map[K]V`) is a hash map:

```go
nominated := map[string]bool{}            // like a set of strings
for _, n := range noms {
    nominated[n.JellyfinItemID] = true
}
if nominated[id] { ... }                  // missing key returns the zero value (false)
```

A crucial difference from Python: **reading a missing key never raises** — it
returns the *zero value* for V (see §1.7). To tell "absent" from "present but
zero," use the comma-ok form: `v, ok := m[key]`.

Two gotchas worth internalizing now:

- **Map iteration order is randomized.** Go deliberately shuffles it. That's why
  the voting code sorts IDs before tallying (`keysSorted`, `sort.Slice`) — to get
  deterministic, reproducible results. Never rely on map order.
- **`nil` maps and slices are usable for reads** but a `nil` map panics on
  *write*. You'll see defensive `if x == nil { x = []string{} }` in the store and
  views, partly for that and partly so JSON serializes `[]` instead of `null`.

---

## 1.7 Zero values: there is no `None`-by-default

Go has no `undefined` and no implicit `None`. **Every variable is born with a
well-defined zero value:** `0` for numbers, `""` for strings, `false` for bools,
`nil` for pointers/slices/maps/interfaces, and — for a struct — every field set
to *its* zero value. You never see "uninitialized."

This shapes the code in two visible ways:

1. **Structs are often built field-by-field and partially**, trusting zero
   values for the rest. A freshly created `Poll` leaves `WinnerNominationID` as
   `""` and `Round1ClosesAt` as `nil` simply by not setting them.
2. **"Is this set?" checks compare against the zero value:** `if code == ""`,
   `if closesAt == nil`, `if p.CreatedAt.IsZero()`. There's no separate "missing"
   state — the zero value *is* the missing state. This is why nullable timestamps
   use `*time.Time` (a pointer, whose zero value `nil` cleanly means "no time")
   rather than `time.Time` (whose zero value is a real-but-meaningless date).

---

## 1.8 Goroutines and channels (the concurrency you'll see)

Go's concurrency is built in, not a library. You'll meet it in three spots:
the SSE hub, the background timer sweeper, and the HTTP server itself. Chapter 7
goes deep; here's the vocabulary.

- **A goroutine is a function running concurrently**, started with the `go`
  keyword. It's far lighter than an OS thread (you can have thousands).
  Conceptually it's like `asyncio.create_task(...)`, but you don't `await` it and
  there's no `async`/`await` coloring — *any* function can be `go`-launched.

  ```go
  // cmd/seeurchin/main.go — run the timer sweeper alongside the server
  go srv.RunTimerSweeper(sweepCtx, time.Second)

  go func() {                 // and the HTTP server itself, in its own goroutine
      if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
          log.Fatalf("server: %v", err)
      }
  }()
  ```

- **A channel (`chan T`) is a typed pipe** for passing values *between*
  goroutines. The SSE `Hub` gives each connected browser a `chan []byte` and
  pushes event notifications into it.

- **`select` waits on several channel operations at once** — like
  `asyncio.wait(..., FIRST_COMPLETED)`. The SSE handler uses it to multiplex
  "client disconnected," "new event to send," and "time to send a keepalive":

  ```go
  // internal/httpapi/sse.go
  for {
      select {
      case <-ctx.Done():            // request cancelled / client gone
          return
      case msg, ok := <-ch:         // an event to forward
          if !ok { return }
          // ...write it to the response...
      case <-keepalive.C:           // 25s ticker fired
          // ...write a comment line to keep the connection alive...
      }
  }
  ```

- **Shared state is guarded by a mutex.** When goroutines must touch the same map
  (the hub's set of subscribers), a `sync.Mutex` serializes access:

  ```go
  h.mu.Lock()
  h.subs[pollID][ch] = struct{}{}
  h.mu.Unlock()
  ```

  `struct{}{}` is an empty struct — zero bytes — used as a "value I don't care
  about," turning a `map[chan][]byte]struct{}` into a set. Python would reach for
  `set()`; Go idiom is a map to empty structs.

---

## 1.9 `context.Context`: cancellation and deadlines, threaded through

Almost every function that does I/O takes `ctx context.Context` as its **first
parameter**. You'll see it so often it becomes background noise — but it's doing
real work.

A `Context` carries (a) a cancellation signal and (b) an optional deadline down
through a call tree. When an HTTP client disconnects, chi cancels the request's
context; that cancellation propagates into the database driver and the outbound
Jellyfin call, which abort instead of doing wasted work. `main` also uses it to
bound startup and shutdown:

```go
// cmd/seeurchin/main.go
pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
if err := jf.Ping(pingCtx); err != nil { ... }
cancel()
```

The closest Python analogues are an `asyncio` cancellation token plus a timeout,
but in Go it's an explicit value you pass along rather than ambient task state.
You almost never construct one in a handler — you take `r.Context()` from the
request and pass it down. Just thread it through; Chapter 8 shows it in the
HTTP clients.

---

## 1.10 `defer`: cleanup that runs on the way out

`defer` schedules a call to run when the surrounding *function* returns, no
matter how it returns. It's Go's `try/finally` and its `with` block rolled into
one, and it sits right next to the thing it's cleaning up:

```go
rows, err := s.db.QueryContext(ctx, ...)
if err != nil {
    return nil, err
}
defer rows.Close()        // guaranteed to run when this function returns
for rows.Next() { ... }
```

You'll see `defer resp.Body.Close()` after every HTTP call, `defer st.Close()`
in `main`, and `defer cancel()` after creating a context. Reading tip: when you
see `defer`, mentally note "...and this will be undone at the end."

---

## 1.11 A few syntax notes so nothing trips you up

- **No semicolons, no parens around conditions.** `if x > 2 {` not `if (x > 2):`.
  Braces are mandatory even for one-liners. `gofmt` (the canonical formatter)
  decides all whitespace — there are no style debates; run `gofmt -l` /
  `go vet ./...` (see Chapter 9).
- **`:=` vs `=`.** `:=` declares + assigns (only inside functions); `=` assigns
  to something already declared. Package-level vars use `var x = ...`.
- **Constants and enums.** Go has no `enum` type. The pattern is a named string
  type plus typed constants, exactly as `Status` does:

  ```go
  type Status string
  const (
      StatusRound1 Status = "round1"
      StatusRound2 Status = "round2"
      StatusClosed Status = "closed"
  )
  ```

  This is the Go equivalent of a `class Status(str, Enum)`. The values are just
  strings (so they serialize and store trivially), but the *type* keeps you from
  passing an arbitrary string where a `Status` is expected.
- **`iota`** auto-increments constants; this repo doesn't really use it, so don't
  worry about it.
- **Variadic functions.** `func errBad(format string, a ...any) *Error` — the
  `...any` is "zero or more arguments of any type," like Python's `*args`. `any`
  is an alias for `interface{}`, the empty interface every type satisfies (Go's
  `typing.Any`). Call site: `errBad("you can nominate at most %d", max)`.

---

## You're ready

That's the whole vocabulary. The rest of the series is *applied* Go — each
chapter takes one slice of the app and reads it closely. If a construct ever
looks alien, it's almost certainly one of the eleven things above. Next up:
[how the packages fit together and how `main` wires them up »](02-architecture-and-wiring.md)

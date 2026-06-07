# 9 · Testing, building & running

The last chapter is the practical one: how this project is tested, and how it's
built and run. If you're used to `pytest` and `uvicorn`, you'll find Go's
built-in tooling refreshingly batteries-included — testing, formatting, vetting,
and building are all `go` subcommands with no extra packages to install.

---

## 9.1 The testing philosophy: it's just code

Go testing is part of the standard toolchain (`go test`) and the standard library
(`testing`). There's no pytest, no fixtures-by-decorator, no assertion DSL. A test
is a regular function in a `_test.go` file whose name starts with `Test` and which
takes a `*testing.T`:

```go
func TestApprovalTally(t *testing.T) {
    // ...arrange...
    if got != want {
        t.Fatalf("tally = %v, want %v", got, want)   // you report failures yourself
    }
}
```

The conventions:

- **File naming:** tests for `service.go` live in `service_test.go`, in the same
  directory. `go test ./...` finds and runs them all.
- **Failure reporting is manual.** There's no `assert ==`. You compare and call
  `t.Errorf` (record a failure, keep going) or `t.Fatalf` (record and stop this
  test). For deep equality of slices/maps, the standard tool is
  `reflect.DeepEqual` (you'll see it in the voting tests comparing `WinnerIDs`).
  Some projects add the `testify` assertion library; this one stays standard-lib.
- **Two package styles.** Tests can live *in* the package (`package voting`, white-
  box — can touch unexported things) or in a sibling `_test` package
  (`package poll_test`, black-box — only the public API). This repo uses both
  deliberately: `voting_test.go` is `package voting` so it can build configs from
  the unexported `approvalConfig`; `service_test.go` is `package poll_test` so it
  exercises the service exactly as a real caller would.

Run them the way `CLAUDE.md` documents:

```sh
go test ./...                                              # everything
go test ./internal/voting -run TestRankedIRVRedistribution -v   # one package / one test, verbose
go test -race ./...                                        # with the data-race detector
```

That `-race` flag is worth knowing given Chapter 7: it instruments the binary to
detect concurrent unsynchronized access at runtime — invaluable for the Hub and
the sweeper. There's nothing equivalent built into CPython.

---

## 9.2 Helpers, `t.Helper()`, and the no-mock approach

Go tests lean on small *helper functions* instead of pytest fixtures. The poll
service tests set up a real service over a real temp-file database — **no mocking
library** — because the architecture (Chapter 2) made the dependencies injectable:

```go
// internal/poll/service_test.go
type fakeResolver struct{}
func (fakeResolver) GetItem(_ context.Context, id string) (*poll.ResolvedItem, error) {
    return &poll.ResolvedItem{ID: id, Title: "Title " + id, Type: "Movie", Year: 2020}, nil
}

func newSvc(t *testing.T) (*poll.Service, *store.Store) {
    t.Helper()                                              // attribute failures to the CALLER
    st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
    if err != nil {
        t.Fatalf("open store: %v", err)
    }
    t.Cleanup(func() { _ = st.Close() })                   // teardown, registered inline
    return poll.NewService(st, fakeResolver{}, 0), st
}
```

Three standard-library testing features doing the work a framework would:

- **`t.TempDir()`** creates a unique temp directory that's *automatically removed*
  when the test ends. The SQLite DB is created inside it, so every test gets a
  pristine database with zero cleanup code and full isolation. (This is why the
  tests use a *real* store rather than a mock — spinning one up is free and tests
  the real SQL.)
- **`t.Cleanup(fn)`** registers teardown that runs at test end, in LIFO order —
  Go's answer to a fixture's teardown, but co-located with the setup that needs
  it. No separate `tearDown` method.
- **`t.Helper()`** marks a function as a helper, so when it calls `t.Fatalf` the
  reported failure line points at the *test* that called the helper, not at the
  helper's innards. Put it at the top of every assertion/setup helper.

`fakeResolver` is a hand-written stub: the *only* thing the service needs from
Jellyfin is `GetItem`, expressed as the one-method `ItemResolver` interface
(Chapter 1), so faking it is a 3-line struct, not a mock framework. **This is the
single biggest practical payoff of programming to small interfaces** — test
doubles are trivial to write by hand. The HTTP-layer tests (`server_test.go`) do
the same with a `fakeResolver{ items: map[...] }` and spin up the *whole* server
with `httptest.NewServer(srv.Routes())`, then drive it with a real `http.Client`
(cookie jar and all) — true end-to-end tests of the API with no external
services.

---

## 9.3 Sub-tests and table tests

For checking many cases, Go uses **table-driven tests** (a slice of cases in a
loop) and **sub-tests** (`t.Run(name, fn)` for isolated, individually-named
cases). `TestSelfVoteLimits` is effectively a table of scenarios checked in
sequence:

```go
// internal/voting/voting_test.go (excerpt)
func TestSelfVoteLimits(t *testing.T) {
    all := []string{"A", "B", "C"}
    own := []string{"A"}                       // the voter nominated A

    apr := cfg(approvalConfig{VotesPerUser: 5, MaxVotesPerOption: 5, AllowSelfVote: true, MaxSelfVotes: intp(1)})
    if err := (Approval{}).ValidateBallot(apr, ballot(map[string]int{"A": 1}), all, own); err != nil {
        t.Errorf("1 self-vote within cap rejected: %v", err)
    }
    if err := (Approval{}).ValidateBallot(apr, ballot(map[string]int{"A": 2}), all, own); err == nil {
        t.Error("expected 2 self-votes to exceed cap of 1")
    }
    // ...more cases: override, none, ranked cap, score cap...
}
```

The idiomatic *table* form (used widely in Go, worth recognizing) collects cases
into a slice and runs each as a sub-test:

```go
cases := []struct {
    name    string
    ballot  map[string]int
    wantErr bool
}{
    {"within cap", map[string]int{"A": 1}, false},
    {"over cap",   map[string]int{"A": 2}, true},
}
for _, tc := range cases {
    t.Run(tc.name, func(t *testing.T) {        // each case is its own named sub-test
        err := (Approval{}).ValidateBallot(apr, ballot(tc.ballot), all, own)
        if (err != nil) != tc.wantErr {
            t.Fatalf("got err=%v, wantErr=%v", err, tc.wantErr)
        }
    })
}
```

The anonymous-struct slice (`[]struct{ ... }{ ... }`) is a Go staple: a one-off
record type defined and populated inline. `t.Run` gives each row its own name in
the output and lets you run just one with `-run TestX/within_cap`. The timer tests
in `service_test.go` are a great real example — they walk a quick-timer through
start → pause → extend → resume and assert the remaining seconds at each step
using a `near()` tolerance helper (clocks aren't exact).

---

## 9.4 The other quality gates

Beyond tests, two commands keep the codebase consistent — both standard, both
fast, both worth running before you commit:

```sh
gofmt -l internal/...     # list files that aren't canonically formatted (-w to fix in place)
go vet ./...              # static checks for likely bugs
```

- **`gofmt`** is *the* formatter, and it's non-negotiable in Go culture: there's
  exactly one correct format and the tool produces it. No Black-vs-autopep8
  debates, no style config. Most editors run it on save. CI typically fails if
  `gofmt -l` prints anything.
- **`go vet`** catches a class of real mistakes the compiler allows: `Printf`
  format strings that don't match their arguments, copying a struct that contains
  a mutex, unreachable code, and so on. Think of it as a built-in, zero-config
  linter focused on correctness rather than style.

The compiler itself is your strictest reviewer: unused imports and unused local
variables are **compile errors**, not warnings. That feels harsh coming from
Python, but it means dead code can't quietly accumulate.

---

## 9.5 Running it locally

From `CLAUDE.md`, the backend needs only a Jellyfin URL + API key to boot:

```sh
JELLYFIN_URL=http://localhost:8096 JELLYFIN_API_KEY=... SEEURCHIN_ADDR=:5859 \
    go run ./cmd/seeurchin
```

`go run ./cmd/seeurchin` compiles and runs the `main` package in one step (like
`python -m`). The env vars are exactly the `config.FromEnv` inputs from Chapter 8.
Recall that the embedded `webdist/` only contains a placeholder until you build
the frontend — so a bare `go run` serves the API and a stub page. For real UI
work you run the two-process dev setup, where Vite serves the live frontend and
proxies `/api` to the Go backend:

```sh
cd web && npm install && npm run dev      # frontend on :5173, proxies /api → :5859
```

---

## 9.6 Building the shippable artifact

The production build is the embedding story from Chapter 8, in order:

```sh
npm --prefix web ci                       # install frontend deps
npm --prefix web run build                # Svelte → internal/httpapi/webdist/
CGO_ENABLED=0 go build -o seeurchin ./cmd/seeurchin   # static binary, frontend baked in
```

The important flag is **`CGO_ENABLED=0`**. Because the SQLite driver is pure Go
(Chapter 5), the build needs no C compiler and produces a **fully static binary**
with no shared-library dependencies. That single file *is* the deployable: it
contains the API, the business logic, the SQLite engine, and the entire web UI.
Copy it to a machine and run it — nothing else to install. The `Dockerfile`
automates exactly this as a multi-stage build (Node → Go → distroless), yielding a
tiny container.

Coming from Python, sit with how different this is: there's no interpreter to
match, no virtualenv, no `requirements.txt` to resolve on the target, no static-
files step at deploy time. `go build` cross-compiles too (`GOOS=linux GOARCH=arm64
go build ...` builds a Linux ARM binary from your Mac), so the same command
targets a Raspberry Pi or a cloud VM without a build server.

---

## 9.7 A suggested path to keep learning *in this repo*

You now have the whole map. The highest-leverage way to cement it is to make a
small change and watch it ripple through the layers you've read about. A few
graded exercises, easiest first:

1. **Add a read-only endpoint.** Add `GET /api/polls/{code}/participants` that
   returns display names. You'll touch the router (Chapter 3), write a handler and
   a tiny view, and reuse `s.repo.ListParticipants`. No domain changes — pure
   plumbing practice.
2. **Add a field to the poll view.** Surface something already on the domain
   `Poll` (say, `CreatedAt`) in `pollView` and confirm it serializes. This teaches
   the view ↔ domain ↔ frontend-types boundary (`CLAUDE.md` notes `web/src/lib/
   types.ts` must mirror the Go views).
3. **Write a test for an untested rule.** Pick a branch in `service.go` (e.g.
   guest join is rejected when `AllowGuests` is false) and add a `service_test.go`
   case using the existing `newSvc`/`nominate` helpers. This is where the
   no-infrastructure testability really lands.
4. **Add a voting method.** The capstone. Create `internal/voting/<yours>.go`,
   implement the six `Method` methods + a private config, add one `Register(...)`
   line to `init()`, and write a tally test. If you did it right, the create page,
   the API, and the results screen all pick it up with **no other changes** —
   which is the whole point of Chapter 6, felt firsthand.

Each exercise re-walks a slice of this series. By exercise 4 you'll have touched
the router, handlers, views, the service, the store, the voting registry, and the
test harness — i.e. the entire backend — and the Go that looked foreign in
Chapter 1 will read like ordinary code.

---

## Where to go from here

- The [Go Tour](https://go.dev/tour/) — the official interactive intro; skim the
  parts this series referenced (interfaces, goroutines, channels).
- [Effective Go](https://go.dev/doc/effective_go) — the canonical style/idiom
  guide; you'll now recognize most of it in the wild.
- [`database/sql` tutorial](https://go.dev/doc/database/) — deeper on the
  patterns in Chapter 5.
- This repo's own `go test ./...` output — the fastest feedback loop you have.

That's the tour. You came in fluent in APIs and routing; you now know how this
particular Go app expresses them, where it differs from FastAPI, and why it's
shaped the way it is. Welcome to Go.

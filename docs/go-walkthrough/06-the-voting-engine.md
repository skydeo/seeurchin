# 6 · The voting engine

`internal/voting` is the most "Go-idiomatic" package in the project and the best
place to *really* understand interfaces. It's a small plugin system: four voting
methods (approval, ranked-choice, score, random), each self-contained and
self-registering, with the rest of the app written entirely against an interface
and never against a concrete method. Adding a fifth method touches exactly one
new file.

If Chapter 1 introduced interfaces in the abstract, this chapter is where they
earn their keep. The Python analogue would be an ABC plus a registry dict — but
Go's structural typing makes the whole thing lighter.

---

## 6.1 The `Method` interface: the contract every voting style fulfills

```go
// internal/voting/voting.go
type Method interface {
    Key() string                                  // stable id stored on the poll, e.g. "approval"
    Label() string                                // human-friendly name for the UI
    DefaultConfig() json.RawMessage               // the method's default config as JSON
    ValidateConfig(raw json.RawMessage) error     // check a host-provided config blob
    ValidateBallot(raw json.RawMessage, b Ballot, allIDs, ownIDs []string) error
    Tally(raw json.RawMessage, allIDs []string, ballots []Ballot) (Results, error)
}
```

This single interface is the entire surface the rest of the app uses to talk to
*any* voting style. Notice what's threaded through almost every method:
`raw json.RawMessage`. That's the **opaque per-method config** — the poll stores
a JSON blob whose shape only the method itself understands, and every call hands
that blob back so the method can interpret it. The HTTP layer, the service, and
the store never need to know what an "approval config" looks like; they just
shuttle the bytes.

The supporting data types are deliberately generic:

```go
// internal/voting/voting.go
type Ballot struct {
    ParticipantID string
    Selections    map[string]int   // nominationID → an int whose meaning is method-specific
}

type Results struct {
    Method    string        `json:"method"`
    Ranked    []Result      `json:"ranked"`       // best-first
    WinnerIDs []string      `json:"winner_ids"`   // >1 only on a tie
    Rounds    []RoundResult `json:"rounds,omitempty"`  // ranked-choice elimination rounds
}
```

The genius of `Selections map[string]int` is that the *same* ballot shape serves
every method, because the integer means different things per method (documented
right on the `Ballot` type):

- **approval:** votes placed on that nomination (a budget you distribute)
- **score:** the 0..max score given to that nomination
- **ranked:** the 1-based rank position (1 = top choice)

So the HTTP `voteReq { selections: map[string]int }` is method-agnostic. The
*meaning* is resolved entirely inside the chosen `Method`. One wire format, four
interpretations.

---

## 6.2 A concrete method: `Approval`

A voting method is just a type with those six methods. `Approval` is the simplest
full example. The type itself is *empty* — it holds no state, it's pure behavior:

```go
// internal/voting/approval.go
type Approval struct{}              // zero fields — a "namespace for methods"

type approvalConfig struct {        // THIS method's private config shape
    VotesPerUser      int  `json:"votes_per_user"`
    MaxVotesPerOption int  `json:"max_votes_per_option"`
    AllowSelfVote     bool `json:"allow_self_vote"`
    MaxSelfVotes      *int `json:"max_self_votes,omitempty"`
}

func (Approval) Key() string   { return "approval" }
func (Approval) Label() string { return "Approval / N votes" }

func (Approval) DefaultConfig() json.RawMessage {
    return mustJSON(approvalConfig{VotesPerUser: 3, MaxVotesPerOption: 1, AllowSelfVote: true})
}
```

`type Approval struct{}` is an **empty struct** — zero bytes, no fields. It exists
only to *carry methods*. This is a common Go pattern for a "strategy" or
"namespace": the behavior is in the methods, and there's no per-instance state,
so the value is interchangeable and free to copy. (Receivers are written
`func (Approval)` — they don't even bother naming the receiver, because they
never use it.)

`approvalConfig` is unexported (lowercase) — it's nobody's business but
`Approval`'s. `DefaultConfig()` marshals a sensible default to JSON;
`ValidateConfig` parses the blob and bounds-checks it:

```go
func (Approval) ValidateConfig(raw json.RawMessage) error {
    cfg, err := parseApprovalConfig(raw)
    if err != nil { return err }
    if cfg.VotesPerUser < 1 {
        return fmt.Errorf("votes_per_user must be >= 1")
    }
    if cfg.MaxVotesPerOption < 0 {
        return fmt.Errorf("max_votes_per_option must be >= 0")
    }
    return nil
}
```

`ValidateBallot` is where the server-side rules live — a client can't cheat by
crafting a ballot, because the method re-checks everything against the config:

```go
func (Approval) ValidateBallot(raw json.RawMessage, b Ballot, allIDs, ownIDs []string) error {
    cfg, _ := parseApprovalConfig(raw)
    all := toSet(allIDs)                                    // valid nomination ids
    own := toSet(ownIDs)                                    // the voter's own noms
    selfLimit := selfVoteLimit(cfg.AllowSelfVote, cfg.MaxSelfVotes)
    total, selfVotes := 0, 0
    for id, votes := range b.Selections {
        if votes <= 0 { return fmt.Errorf("vote count for %q must be positive", id) }
        if !all[id]   { return fmt.Errorf("unknown nomination %q", id) }
        if cfg.MaxVotesPerOption > 0 && votes > cfg.MaxVotesPerOption {
            return fmt.Errorf("at most %d vote(s) per option", cfg.MaxVotesPerOption)
        }
        if own[id] { selfVotes += votes }
        total += votes
    }
    // ...enforce self-vote limit and the total budget...
    if total > cfg.VotesPerUser {
        return fmt.Errorf("at most %d vote(s) total", cfg.VotesPerUser)
    }
    return nil
}
```

And `Tally` computes the result — sum the votes per nomination, then rank:

```go
func (Approval) Tally(raw json.RawMessage, allIDs []string, ballots []Ballot) (Results, error) {
    scores := make(map[string]float64, len(allIDs))
    for _, b := range ballots {
        for id, v := range b.Selections {
            if v > 0 { scores[id] += float64(v) }
        }
    }
    ranked, winners := rankByScore(scores, allIDs)         // shared helper
    return Results{Method: "approval", Ranked: ranked, WinnerIDs: winners}, nil
}
```

That's an entire voting method in ~100 lines: a config struct, six methods, done.
`Score` is nearly identical (0..max ratings, total or average aggregation).
`Ranked` is the meaty one — an instant-runoff algorithm with elimination rounds —
but it satisfies the *same* interface, so the rest of the app treats it
identically.

### Shared helpers, not inheritance

Go has no class inheritance, so common logic is shared via **plain package
functions**, not a base class. `voting.go` holds the helpers every method reuses:
`toSet` (slice → lookup set), `rankByScore` (score map → sorted results + winner
set), and crucially `selfVoteLimit`, which centralizes the "can you vote for your
own nomination?" rule so all three voting methods resolve it identically:

```go
// internal/voting/voting.go — one source of truth for the self-vote rule
func selfVoteLimit(allowSelfVote bool, maxSelfVotes *int) int {
    if maxSelfVotes != nil {            // newer field wins when present
        if *maxSelfVotes < 0 { return -1 }   // unlimited
        return *maxSelfVotes               // 0 = none, N = cap
    }
    if allowSelfVote { return -1 }      // legacy bool fallback
    return 0
}
```

This is "composition over inheritance" in its simplest form: behavior shared
across methods lives in functions they all call, not in a superclass they all
extend. The `*int` for `maxSelfVotes` is the nullable-via-pointer trick from
Chapter 1 doing real work — `nil` means "field absent, fall back to the legacy
bool," which is how the schema evolved without breaking old polls.

---

## 6.3 The registry: self-registration via `init()`

Here's the part that makes it a *plugin system* rather than just a set of types.
The registry is a package-level map, and each method registers itself:

```go
// internal/voting/voting.go
var registry = map[string]Method{}

func Register(m Method) { registry[m.Key()] = m }

func Get(key string) (Method, bool) {
    m, ok := registry[key]
    return m, ok
}

func All() []Method { /* every registered method, sorted by key */ }

func init() {
    Register(Approval{})
    Register(Ranked{})
    Register(Score{})
    Register(Random{})
}
```

`init()` is a special Go function: **it runs automatically when the package is
first loaded, before `main` and before anything else uses the package.** (A
package can even have several `init`s.) So merely importing `internal/voting`
populates the registry with all four methods — no explicit setup call anywhere.
By the time `poll.Service.CreatePoll` does `voting.Get(in.VotingMethod)`, the
registry is already full.

This is the same idea as the `import _ "modernc.org/sqlite"` driver registration
from Chapter 5 — *self-registration in `init()`* is a pervasive Go pattern for
plugin-style extensibility.

### How the rest of the app consumes it

Every other layer talks to voting *only* through `Get`/`All` and the `Method`
interface — never by naming `Approval` or `Ranked`:

- **Create** (`poll.Service.CreatePoll`): `voting.Get(method)` → `DefaultConfig`
  / `ValidateConfig`.
- **Vote** (`poll.Service.CastVotes`): `voting.Get(method)` → `ValidateBallot`.
- **Tally** (`poll.Service.Results`): `voting.Get(method)` → `Tally`.
- **List methods for the UI** (`httpapi.handleMethods`): `voting.All()` →
  `{key, label, default_config}` for each, so the create page renders the right
  options dynamically. The frontend doesn't hardcode the method list either; it
  asks the API.

So **to add a new voting method you write one file** — define a type, give it the
six methods and a private config, and add one `Register(...)` line to `init()`.
Nothing in the HTTP layer, the service, the store, or the frontend's method
picker needs to change. That's the open/closed principle delivered by an
interface + a registry, and it's worth internalizing as *the* canonical Go way to
build pluggable behavior.

---

## 6.4 Optional interfaces: `AutoDecider` and the type assertion

Now the elegant part. "Random pick" is special: it has *no voting round at all* —
the host closes round 1 and a winner is drawn on the spot. But the `Method`
interface has no concept of "decide without voting." Rather than bloat `Method`
with a method that's meaningless for the other three, the design uses an
**optional interface**:

```go
// internal/voting/voting.go
type AutoDecider interface {
    Method                                                  // embeds Method...
    Decide(raw json.RawMessage, allIDs []string) (string, error)  // ...plus one more method
}

func Decider(key string) (AutoDecider, bool) {
    m, ok := registry[key]
    if !ok { return nil, false }
    d, ok := m.(AutoDecider)        // type assertion: does m ALSO satisfy AutoDecider?
    return d, ok
}
```

Two new Go concepts here:

1. **Interface embedding.** `AutoDecider` *embeds* `Method` (the bare `Method`
   line inside the interface), so an `AutoDecider` is "everything a `Method` is,
   plus `Decide`." This is how Go composes interfaces — like `io.ReadWriter`
   being `Reader` + `Writer`.

2. **The type assertion `m.(AutoDecider)`.** Given a value held as a `Method`,
   `m.(AutoDecider)` asks at *runtime*: "does the concrete type behind this
   interface *also* satisfy `AutoDecider`?" The two-value form `d, ok := m.(...)`
   returns `ok == false` instead of panicking when it doesn't. This is exactly
   the Python idiom `if isinstance(m, AutoDecider):` — feature-detection on a
   value — but checked structurally.

`Random` opts in simply by *having* a `Decide` method:

```go
// internal/voting/random.go
func (Random) Decide(_ json.RawMessage, allIDs []string) (string, error) {
    if len(allIDs) == 0 {
        return "", fmt.Errorf("no nominations to choose from")
    }
    n, err := crand.Int(crand.Reader, big.NewInt(int64(len(allIDs))))  // crypto/rand, unbiased
    if err != nil { return "", err }
    return allIDs[n.Int64()], nil
}

var _ AutoDecider = Random{}        // compile-time proof Random satisfies AutoDecider
```

The other three methods simply *don't* have a `Decide` method, so `Decider(key)`
returns `false` for them. That's the whole opt-in mechanism — no flags, no
registration of "capabilities," just "does the type happen to implement this
extra interface?"

### Where the optional interface is used

Back in the domain (Chapter 4), `advanceCore` feature-detects on the way out of
round 1:

```go
// internal/poll/service.go
if dec, ok := voting.Decider(p.VotingMethod); ok {
    // This method decides without voting (random): draw + freeze a winner, close now.
    winner, err := dec.Decide(p.VotingConfig, ids)
    // ...CompareAndSetStatus(round1 → closed); SetPollWinner(winner)...
}
```

And `applyDeadlineConfig` uses the same `Decider` check to know that an
AutoDecider poll has no round 2, so it ignores any round-2 timer settings. The
poll lifecycle adapts to the method's *capabilities*, discovered through the
optional interface, with zero `if method == "random"` special-casing. If you add
another "decide instantly" method later, it just implements `AutoDecider` and the
lifecycle handles it for free.

> **Why is `Random.Tally` still defined?** Because `Random` must satisfy the full
> `Method` interface too (it's in the registry like any other). Its `Tally`
> returns the candidates with no scores; the *actual* winner is the frozen
> `WinnerNominationID`, which `poll.Service.Results` overlays on top (Chapter 4
> §4.7). The draw is persisted, so re-reading a closed random poll always shows
> the same winner — determinism on top of a non-deterministic method.

---

## 6.5 The pattern, generalized

Step back and notice the layered design, because it's reusable far beyond voting:

| Mechanism | Go feature | What it buys |
|-----------|-----------|--------------|
| One contract for all methods | `Method` interface | the app codes against behavior, not concrete types |
| Per-method config nobody else parses | `json.RawMessage` opaque blob | new config shapes need no changes upstream |
| Methods register themselves on import | `init()` + a registry map | adding a method = one file, zero wiring |
| "Decide-without-voting" capability | optional interface + type assertion | extend behavior for *some* methods without bloating the common interface |
| Shared rules (self-vote, ranking) | plain helper functions | code reuse without inheritance |
| Conformance guarantees | `var _ Iface = T{}` | the build breaks the moment a type drifts from its contract |

This is what people mean when they say Go favors *composition and small
interfaces* over class hierarchies. There's no `AbstractVotingMethod` base class,
no metaclass registry magic, no decorator — just an interface, a map, and `init`.
Read this package twice; it's the clearest teacher of idiomatic Go in the repo.

[Next: concurrency, sessions, and live updates (SSE) »](07-concurrency-sessions-and-sse.md)

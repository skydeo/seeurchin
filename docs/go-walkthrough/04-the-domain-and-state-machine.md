# 4 · The domain & state machine

`internal/poll` is the brain of the application. Everything else — HTTP, SQL,
Jellyfin — exists to feed it or carry out its decisions. This chapter reads the
domain: the data types, the `Service` that enforces the rules, the round
lifecycle, and the surprisingly subtle concurrency around timers.

If you've ever wished your FastAPI app had a clear "service layer" that wasn't
tangled up with request objects and ORM sessions — this is what that looks like.

---

## 4.1 Data vs behavior: `types.go` vs `service.go`

The package splits cleanly:

- **`types.go`** — the *data*: `Poll`, `Participant`, `Nomination`, `Vote`, plus
  small typed enums (`Status`, `LibraryScope`, `DeadlineMode`) and a few tiny
  helper methods. These are mostly inert structs with `json` tags. No I/O.
- **`service.go`** — the *behavior*: the `Service` type and all the rules
  (create, join, nominate, vote, advance, tally, the timer logic). The service
  holds no data itself beyond its injected dependencies.
- **`repository.go`** — the *contract* to the outside world for persistence (an
  interface; Chapter 5).

This is a deliberate "anemic data, rich service" design. The `Poll` struct
doesn't know how to advance itself; `Service.Advance` does. That keeps all the
rules in one readable place and keeps the data types trivially serializable and
storable.

### The enums

```go
// internal/poll/types.go
type Status string
const (
    StatusDraft  Status = "draft"
    StatusRound1 Status = "round1"   // accepting nominations
    StatusRound2 Status = "round2"   // accepting votes
    StatusClosed Status = "closed"   // finished; results final
)
```

`Status` is a named string type — the Go enum pattern from Chapter 1. The value
is a plain string (so it stores and serializes with zero ceremony), but the
*type* stops you from accidentally passing a random string where a `Status` is
required. `LibraryScope` and `DeadlineMode` follow the same shape.

---

## 4.2 The `Service` and its constructor

```go
// internal/poll/service.go
type Service struct {
    repo    Repository
    items   ItemResolver
    codeLen int
}

func NewService(repo Repository, items ItemResolver, codeLen int) *Service {
    if codeLen <= 0 {
        codeLen = codes.DefaultLength
    }
    return &Service{repo: repo, items: items, codeLen: codeLen}
}
```

`NewService` is a **constructor function** — Go has no `__init__`; the convention
is a package-level `NewX` that returns `*X`. It takes the injected dependencies
(both interfaces) and applies a default (`codeLen <= 0` → the package default).
The returned `*Service` is what `main` hands to the HTTP layer.

Everything below is a method on `*Service`, so it can use `s.repo`, `s.items`,
and `s.codeLen`.

---

## 4.3 `CreatePoll`: validation, then construction, then persistence

`CreatePoll` is the best single example of how a domain method is shaped:
*validate → derive → construct → persist*, returning rich errors at each gate.
Trimmed to its skeleton:

```go
func (s *Service) CreatePoll(ctx context.Context, in CreatePollInput) (*Poll, *Participant, error) {
    title := strings.TrimSpace(in.Title)
    if title == "" {
        return nil, nil, errBad("title is required")
    }
    hostName := strings.TrimSpace(in.HostName)
    if hostName == "" {
        return nil, nil, errBad("your name is required")
    }
    if in.LibraryScope == "" {
        in.LibraryScope = ScopeBoth                 // default
    }
    switch in.LibraryScope {
    case ScopeMovies, ScopeSeries, ScopeBoth:       // ok
    default:
        return nil, nil, errBad("invalid library scope %q", in.LibraryScope)
    }
    if err := validateRules(in.SubmissionRules); err != nil {
        return nil, nil, err
    }
    // ... reveal-scope + genres normalization ...

    method, ok := voting.Get(in.VotingMethod)        // look up the chosen method
    if !ok {
        return nil, nil, errBad("unknown voting method %q", in.VotingMethod)
    }
    cfg := in.VotingConfig
    if len(cfg) == 0 {
        cfg = method.DefaultConfig()                 // method supplies its default
    }
    if err := method.ValidateConfig(cfg); err != nil {
        return nil, nil, errBad("voting config: %v", err)
    }

    code, err := codes.GenerateUnique(s.codeLen, 10, func(c string) (bool, error) {
        return s.repo.CodeExists(ctx, c)             // retry until a free code is found
    })
    if err != nil {
        return nil, nil, err
    }

    p := &Poll{
        ID:           NewID(),
        Code:         code,
        Title:        title,
        Status:       StatusRound1,                  // created straight into round 1
        VotingMethod: in.VotingMethod,
        VotingConfig: cfg,
        // ...all the other fields from `in`...
    }
    if err := applyDeadlineConfig(p, in, time.Now()); err != nil {
        return nil, nil, err
    }

    host := &Participant{
        ID:           NewID(),
        DisplayName:  hostName,
        SessionToken: NewID(),
        Role:         RoleHost,
    }
    if err := s.repo.CreatePoll(ctx, p, host); err != nil {  // one transaction
        return nil, nil, err
    }
    return p, host, nil
}
```

Things worth absorbing:

- **Three return values: `(*Poll, *Participant, error)`.** The host participant
  comes back separately because the HTTP layer needs its `SessionToken` to set
  the creator's cookie. On any failure, *both* value returns are `nil` and the
  error is non-nil — the Go convention is "on error, don't trust the other
  returns."
- **Validation lives here, not in the HTTP layer.** Every rule ("title required,"
  "valid scope," "coherent submission rules," "known voting method," "valid
  voting config") is checked in the service and produces a `*poll.Error` with the
  right status. The handler stays dumb. This is exactly the unit-testable core
  the architecture is built to give you.
- **The voting method validates its own config.** `CreatePoll` doesn't know what
  an "approval" config looks like — it asks the registered `Method` to supply a
  default and to validate. That's the plugin boundary (Chapter 6) showing up in
  the create path.
- **Unique-code generation takes a callback.** `codes.GenerateUnique(len, tries,
  exists func(string)(bool,error))` is passed an inline closure that checks the
  store. Passing a function as an argument is everyday Go; the closure captures
  `ctx` and `s.repo` from the surrounding scope. (Python would do the same with a
  lambda or nested def.)
- **Polls are born in `round1`.** There's a `StatusDraft` constant, but creation
  jumps straight to `round1` (accepting nominations). The state machine starts
  "open."

---

## 4.4 The state machine

The lifecycle is small and the transitions are all funneled through one method,
`advanceCore`. Here's the shape:

```
                 ┌──────────────────────────────────────────────┐
                 │              (created here)                   │
                 ▼                                               │
   round1 ───────────────► round2 ───────────────► closed       │
 (nominate)   advance     (vote)     advance     (results)       │
     │                                                            │
     │ if method is an AutoDecider (e.g. "random"):               │
     └────────────────────────────────────────────► closed       │
        advance draws + freezes a winner, skipping round 2        │
```

- The **host** drives transitions by calling `Advance` (which the HTTP
  `/advance` endpoint and the "end now" timer button hit).
- The **timer sweeper** drives the *same* transitions automatically when a
  round's deadline passes (§4.6).
- **Random** (and any future "decide without voting" method) collapses
  `round1 → closed` in a single advance, because it implements the optional
  `AutoDecider` interface (Chapter 6).

### `advanceCore`: one method, two callers, careful guards

```go
// internal/poll/service.go (heavily trimmed)
func (s *Service) advanceCore(ctx context.Context, p *Poll, auto bool) (*Poll, error) {
    switch p.Status {
    case StatusRound1:
        noms, err := s.repo.ListNominations(ctx, p.ID)
        if err != nil { return nil, err }

        if len(noms) < 2 {
            if !auto && p.DeadlineMode == DeadlineNone {
                return nil, errConflict("need at least 2 nominations to start voting")
            }
            return s.resolveSparseRound1(ctx, p, noms)   // 1 → crown it; 0 → close empty
        }
        if dec, ok := voting.Decider(p.VotingMethod); ok {
            // AutoDecider: draw + freeze a winner, close immediately.
            winner, err := dec.Decide(p.VotingConfig, idsOf(noms))
            // ...CompareAndSetStatus(round1 → closed); SetPollWinner(winner)...
            return p, nil
        }
        // Normal path: round1 → round2, arm the voting clock if "quick".
        ok, err := s.repo.CompareAndSetStatus(ctx, p.ID, StatusRound1, StatusRound2)
        if err != nil { return nil, err }
        if !ok { return s.repo.GetPollByID(ctx, p.ID) }  // someone else already advanced
        p.Status = StatusRound2
        // ...stamp Round2ClosesAt if quick mode...
        return p, nil

    case StatusRound2:
        ok, err := s.repo.CompareAndSetStatus(ctx, p.ID, StatusRound2, StatusClosed)
        if err != nil { return nil, err }
        if !ok { return s.repo.GetPollByID(ctx, p.ID) }
        p.Status = StatusClosed
        return p, nil

    default:
        return nil, errConflict("the poll cannot be advanced from %q", p.Status)
    }
}
```

The `auto bool` parameter is the key to reuse. The *same* transition logic serves
two callers with slightly different politeness:

- **Host-driven (`auto=false`)** via `Advance`: an untimed round 1 with fewer
  than 2 nominations is a hard error ("need at least 2"). This is the
  long-standing guard against starting a vote with nothing to vote on.
- **Sweeper / admin (`auto=true`)** via `SweepDueTimers`/`ForceAdvance`: a sparse
  round 1 resolves *gracefully* instead of erroring — exactly one nomination is
  crowned the winner, zero closes the poll empty (`resolveSparseRound1`). You
  don't want a fired deadline to throw; you want it to do the sensible thing.

`Advance` itself is a thin authorization wrapper:

```go
func (s *Service) Advance(ctx context.Context, p *Poll, participant *Participant) (*Poll, error) {
    if !participant.IsHost() {
        return nil, errForbid("only the host can advance the poll")
    }
    return s.advanceCore(ctx, p, false)
}
```

Splitting the *authorized, strict* entry point (`Advance`) from the *shared
mechanism* (`advanceCore`) — and from the *lenient, unauthenticated* entry points
(`SweepDueTimers`, `ForceAdvance`) — is a clean way to encode "same transition,
different rules about who/when." Worth stealing as a pattern.

---

## 4.5 Concurrency you can't see in Python: compare-and-set

Here's a subtle, important detail. Two things can try to advance the same poll at
the *same instant*: the host clicking "start voting," and the background sweeper
noticing the round-1 timer just expired. In a multi-threaded Go server these run
concurrently. Without care, both could move `round1 → round2` and double-fire
side effects.

The fix is an **atomic compare-and-set in the database**, not a lock in Go:

```go
// internal/store/store.go
func (s *Store) CompareAndSetStatus(ctx context.Context, id string, from, to poll.Status) (bool, error) {
    // ... (the closed case also stamps closed_at) ...
    res, err := s.db.ExecContext(ctx,
        `UPDATE polls SET status = ? WHERE id = ? AND status = ?`,  // only if still `from`
        string(to), id, string(from))
    if err != nil { return false, err }
    n, err := res.RowsAffected()
    return n > 0, err          // true iff THIS call performed the transition
}
```

The `WHERE ... AND status = ?` clause means the `UPDATE` only changes a row if
the poll is *still* in the expected `from` state. Whichever caller runs first
wins (one row affected → `true`); the loser's update matches zero rows → `false`,
and `advanceCore` responds by simply re-reading the now-current poll
(`GetPollByID`) instead of double-advancing. The database is the arbiter, which
also makes it correct even though `MaxOpenConns(1)` serializes writes.

This is the kind of race you genuinely have to think about in Go (real
concurrency, real shared state) that a single-worker sync Python app can often
ignore — and the resolution (atomic DB conditional update) is a transferable
technique for any concurrent system.

---

## 4.6 Timers, deadlines, and the sweeper

The deadline feature is the most intricate slice of the domain, but it's
organized around a single principle stated in the `DeadlineMode` doc comment:

> The engine always works from absolute `Round{1,2}ClosesAt` timestamps. Modes
> are a UI/behavior hint.

Three modes (`types.go`):

- **`none`** (`""`) — legacy: the host advances every round by hand. No timer.
- **`quick`** — per-round *durations* are configured up front; the host taps
  "Start" to *arm the clock*, which stamps an absolute `ClosesAt = now + duration`.
  Can be paused (remembers remaining seconds) and extended.
- **`scheduled`** — absolute per-round close times are set at creation and run
  immediately.

The `Poll` struct carries all the timer state as a handful of fields, several of
them `*time.Time` (nullable) precisely so "no timer / not yet started / paused"
are representable:

```go
// internal/poll/types.go
DeadlineMode      DeadlineMode `json:"deadline_mode,omitempty"`
Round1DurationSec int          `json:"round1_duration_sec,omitempty"`
Round2DurationSec int          `json:"round2_duration_sec,omitempty"`
Round1ClosesAt    *time.Time   `json:"round1_closes_at,omitempty"`  // nil = not running
Round2ClosesAt    *time.Time   `json:"round2_closes_at,omitempty"`
TimerPausedSec    int          `json:"timer_paused_sec,omitempty"`  // >0 = paused, N left
```

Because the rules depend on *which round is active*, `types.go` provides small
accessor helpers that switch on `Status` so the service logic doesn't repeat the
branch everywhere — a nice example of pushing tiny behavior onto the data type:

```go
func (p *Poll) activeClosesAt() *time.Time { /* returns Round1 or Round2 ClosesAt by status */ }
func (p *Poll) setActiveClosesAt(t *time.Time) { /* sets the right one */ }
func (p *Poll) activeDurationSec() int { /* the active round's configured length */ }
```

The host controls (`StartTimer`, `PauseTimer`, `ExtendTimer`) are all
`*Service` methods that (a) check `participant.IsHost()`, (b) manipulate these
fields via the accessors, and (c) persist with `repo.UpdatePollTimers`. For
example, pause computes and stores the remaining time, then clears the absolute
deadline:

```go
func (s *Service) PauseTimer(ctx context.Context, p *Poll, participant *Participant) (*Poll, error) {
    if !participant.IsHost() {
        return nil, errForbid("only the host can control the timer")
    }
    closesAt := p.activeClosesAt()
    if closesAt == nil {
        return nil, errConflict("there's no running timer to pause")
    }
    remaining := int(time.Until(*closesAt).Seconds())
    if remaining < 1 { remaining = 1 }
    p.TimerPausedSec = remaining          // remember how much was left
    p.setActiveClosesAt(nil)              // stop counting down
    return p, s.repo.UpdatePollTimers(ctx, p)
}
```

### The sweeper

Nothing magical advances a poll when its deadline passes — a goroutine started in
`main` polls for due timers once a second:

```go
// internal/poll/service.go
func (s *Service) SweepDueTimers(ctx context.Context, now time.Time) ([]*Poll, error) {
    polls, err := s.repo.ListActiveTimedPolls(ctx)   // only polls with a running timer
    if err != nil { return nil, err }
    var changed []*Poll
    for _, p := range polls {
        closesAt := p.activeClosesAt()
        if closesAt == nil || closesAt.After(now) {
            continue                                  // not due yet
        }
        updated, err := s.advanceCore(ctx, p, true)   // auto=true: lenient path
        if err != nil { return changed, err }
        changed = append(changed, updated)
    }
    return changed, nil
}
```

The HTTP-layer wrapper, `RunTimerSweeper` (timer.go, Chapter 7), drives this on a
ticker and, for each poll that changed, broadcasts an SSE event and fires any
winner auto-request. The store query `ListActiveTimedPolls` keeps it cheap —
it only returns polls that are in round 1 or 2 *and* have a `ClosesAt` set, so a
quiet instance does almost no work each tick.

This design — durable absolute deadlines in the DB + a stateless sweeper that
just enforces them — means timers survive restarts and even fired-while-down
deadlines resolve on the next tick after boot. There's no in-memory timer that
could be lost.

---

## 4.7 Tallying: the service delegates, then overrides

When results are computed, the service gathers ballots and hands them to the
poll's voting `Method`, then applies one domain-level override:

```go
// internal/poll/service.go (Results, trimmed)
res, err := method.Tally(p.VotingConfig, allIDs, ballots)
if err != nil { return voting.Results{}, nil, err }

// A frozen winner (random pick, or any decided-once method) is authoritative.
if p.WinnerNominationID != "" {
    res.WinnerIDs = []string{p.WinnerNominationID}
}
return res, noms, nil
```

The interesting line is the override: for methods that *decided once and froze a
winner* (random, or a lone-nominee crowning), the persisted `WinnerNominationID`
trumps whatever a live re-tally would say. This guarantees the announced winner
never changes on a later read — even for a non-deterministic method. The actual
counting algorithms live in `internal/voting`, which the next-but-one chapter
dissects.

---

## 4.8 Why this layer is a pleasure to test

Because `Service` depends only on interfaces and pure inputs, the tests
(`service_test.go`) read like a spec — no HTTP, no mocks, just "create a poll,
nominate, advance, assert." A representative one:

```go
func TestManualAdvanceUntimedStillNeedsTwo(t *testing.T) {
    svc, _ := newSvc(t)                                 // real temp-file store + fake resolver
    p, host, _ := svc.CreatePoll(ctx, baseInput())      // DeadlineNone
    nominate(t, svc, p, host, "only-one")
    if _, err := svc.Advance(ctx, p, host); err == nil {
        t.Fatal("untimed poll should still require 2 nominations to advance manually")
    }
}
```

That test exercises a real rule end-to-end in five lines, with no scaffolding —
the direct payoff of keeping the domain free of infrastructure. Chapter 9 covers
the testing idioms in full.

[Next: persistence — the Repository interface and its SQLite implementation »](05-persistence-with-sqlite.md)

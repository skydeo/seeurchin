# 5 · Persistence with SQLite

Coming from FastAPI you probably reach for SQLAlchemy (or SQLModel/Tortoise) and
let an ORM map rows to objects. This project does the opposite: **no ORM, no
query builder, just `database/sql` and hand-written SQL.** That sounds like more
work, and in a sense it is — but it's also explicit, dependency-light, and easy
to reason about. This chapter shows how it's organized and the idioms that keep
it from being tedious.

Everything lives in `internal/store`, which is the concrete implementation of
the `poll.Repository` interface from Chapter 4.

---

## 5.1 The interface is the boundary; the struct is the detail

Recall the `Repository` interface (Chapter 4) is defined *in the `poll` package*.
The `store` package's job is to satisfy it. The whole `Store` type is just a
wrapper around a `*sql.DB` handle:

```go
// internal/store/store.go
type Store struct {
    db *sql.DB
}
```

`*sql.DB` is the standard library's database handle. Despite the name it isn't a
single connection — it's a **connection pool** that's safe for concurrent use.
You don't open/close connections yourself; you call methods on `db` and the pool
manages connections under the hood. (Closest Python analogue: a SQLAlchemy
`Engine`, not a `Session`.)

The `Store` never appears in the `poll` package's vocabulary — `poll.Service`
only knows the `Repository` interface. `main` is the only place the two meet, via
`poll.NewService(st, ...)`. That's the dependency inversion from Chapter 2, made
real: swapping SQLite for Postgres means writing a new `Repository`
implementation and changing one line in `main`.

---

## 5.2 Opening the database (with pragmas and a gotcha)

```go
// internal/store/store.go
func Open(path string) (*Store, error) {
    dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
        url.PathEscape(path))
    db, err := sql.Open("sqlite", dsn)
    if err != nil {
        return nil, err
    }
    // modernc.org/sqlite is not safe for arbitrary concurrent writers on one
    // connection; a single open connection keeps writes serialized.
    db.SetMaxOpenConns(1)
    s := &Store{db: db}
    if err := s.migrate(context.Background()); err != nil {
        _ = db.Close()
        return nil, err
    }
    return s, nil
}
```

Three things to understand here:

- **The driver is registered by an anonymous import.** Up top, `store.go` has
  `import _ "modernc.org/sqlite"`. That blank import runs the package's `init()`,
  which calls `sql.Register("sqlite", ...)`. Then `sql.Open("sqlite", dsn)` looks
  the driver up *by name*. This is the standard `database/sql` plugin pattern —
  the same way you'd anonymously import `github.com/lib/pq` for Postgres. The
  `database/sql` package is the stable interface; the driver is swappable.
- **`modernc.org/sqlite` is pure Go — no CGO.** That's a deliberate, important
  choice (noted in `CLAUDE.md`): it means the project builds a fully static
  binary with `CGO_ENABLED=0` and no C toolchain, which is what makes the
  distroless single-binary Docker image possible. The popular `mattn/go-sqlite3`
  driver would have pulled in CGO.
- **The DSN sets pragmas**: `foreign_keys(1)` (enforce the `REFERENCES` so
  `ON DELETE CASCADE` works), `journal_mode(WAL)` (better read/write concurrency),
  and `busy_timeout(5000)` (wait up to 5s on a locked DB instead of erroring).
- **`SetMaxOpenConns(1)` serializes all access** to one connection. This driver
  isn't safe for concurrent writers, so capping the pool at one connection
  sidesteps the problem entirely. It's a fine tradeoff for a self-hosted app
  with light traffic, and it's *why* the compare-and-set transitions in Chapter 4
  are safe. The cost is that writes can't truly parallelize — acceptable here.

`Open` also runs `migrate` before returning, so a freshly-opened store is always
schema-current. Failing to migrate closes the half-open DB and returns the error
— note the `_ = db.Close()` (we're already failing, so we explicitly ignore the
close error rather than shadowing the real one).

---

## 5.3 The schema and hand-rolled migrations

There's no Alembic. The schema is a single idempotent SQL string of
`CREATE TABLE IF NOT EXISTS` statements, applied on every `Open`:

```go
// internal/store/store.go
const schema = `
CREATE TABLE IF NOT EXISTS polls (
  id                  TEXT PRIMARY KEY,
  code                TEXT NOT NULL UNIQUE,
  title               TEXT NOT NULL,
  status              TEXT NOT NULL,
  voting_method       TEXT NOT NULL,
  voting_config       TEXT NOT NULL,
  ...
);
CREATE TABLE IF NOT EXISTS participants ( ... poll_id TEXT NOT NULL REFERENCES polls(id) ON DELETE CASCADE, ... );
CREATE TABLE IF NOT EXISTS nominations ( ..., UNIQUE(poll_id, jellyfin_item_id) );
CREATE TABLE IF NOT EXISTS nomination_nominators ( ..., PRIMARY KEY (nomination_id, participant_id) );
CREATE TABLE IF NOT EXISTS votes ( ... );
CREATE TABLE IF NOT EXISTS seerr_requests ( ..., PRIMARY KEY (poll_id, nomination_id) );
`
```

A couple of schema-design notes that explain the domain:

- **`ON DELETE CASCADE` everywhere.** Deleting a poll automatically deletes its
  participants, nominations, votes, and Seerr requests — so `DeletePoll` is a
  one-line `DELETE FROM polls`. The database enforces referential cleanup.
- **`nomination_nominators` is a join table.** A nomination is keyed
  `UNIQUE(poll_id, jellyfin_item_id)`, and each person who nominated the same
  title gets a row in `nomination_nominators`. That's how "3 people nominated
  Dune" is modeled as one nomination with three nominator rows (Chapter 4's merge
  behavior).

For *evolving* an existing database (a deployed instance has live data you can't
drop), there's a small idempotent column-adder:

```go
// internal/store/store.go
func (s *Store) addColumns(ctx context.Context) error {
    type col struct{ table, name, ddl string }
    wanted := []col{
        {"polls", "reveal_nominators", "INTEGER NOT NULL DEFAULT 0"},
        {"polls", "closed_at", "TEXT"},
        {"polls", "deadline_mode", "TEXT NOT NULL DEFAULT ''"},
        // ...every column added after the table first shipped...
    }
    for _, c := range wanted {
        has, err := s.columnExists(ctx, c.table, c.name)
        if err != nil { return err }
        if has { continue }
        if _, err := s.db.ExecContext(ctx,
            fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.table, c.name, c.ddl)); err != nil {
            return fmt.Errorf("add column %s.%s: %w", c.table, c.name, err)
        }
    }
    return nil
}
```

`columnExists` runs `PRAGMA table_info(...)` and scans the rows. This is a poor
man's migration system: the `CREATE TABLE IF NOT EXISTS` block is the
fresh-install path, and `addColumns` brings older databases forward one column at
a time. It's enough for a single-file SQLite app and avoids a migration-tool
dependency. (For a larger or multi-developer schema you'd graduate to a real
migration tool — but the pattern here is perfectly legible.)

> **`fmt.Errorf("...: %w", err)`** wraps an error with context while preserving
> the original for `errors.Is`/`errors.As`. The `%w` verb is Go's error-wrapping
> mechanism — like `raise X from err`, but the wrapped error stays
> programmatically inspectable up the chain.

---

## 5.4 Reading rows: manual scanning (no ORM magic)

This is where the absence of an ORM is most visible. To load a poll you write the
`SELECT`, then `Scan` each column into a local variable, then assemble the struct.
The column list is a shared `const` so the single-row and multi-row queries stay
in sync:

```go
// internal/store/store.go
const pollSelect = `SELECT id, code, title, host_participant_id, library_scope, status,
    submission_rules, voting_method, voting_config, allow_guests, results_live,
    ... , timer_paused_sec, closed_at FROM polls`

func (s *Store) GetPollByCode(ctx context.Context, code string) (*poll.Poll, error) {
    return s.scanPoll(s.db.QueryRowContext(ctx, pollSelect+` WHERE code = ?`, code))
}
```

`QueryRowContext` runs a query expected to return (at most) one row; the `?` is a
**bound parameter** (positional placeholder — never string-format user input into
SQL; this is parameterized and injection-safe). The result is handed to
`scanPoll`:

```go
func (s *Store) scanPoll(row rowScanner) (*poll.Poll, error) {
    var (
        p                             poll.Poll
        scope, status, rules, vmethod string
        vconfig                       string
        allowGuests, resultsLive      int          // SQLite has no bool; use int
        decidedAt                     sql.NullString  // nullable column
        r1, r2                        sql.NullString
        // ...one local per column...
    )
    err := row.Scan(&p.ID, &p.Code, &p.Title, &p.HostParticipantID, &scope, &status,
        &rules, &vmethod, &vconfig, &allowGuests, &resultsLive, /* ... */ &closedAt)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, poll.ErrNotFound          // translate "no rows" → domain sentinel
    }
    if err != nil {
        return nil, err
    }
    // Now convert the raw column values into the typed domain struct:
    p.LibraryScope = poll.LibraryScope(scope)
    p.Status = poll.Status(status)
    p.AllowGuests = allowGuests != 0          // int → bool
    p.DecidedAt = parseNullTime(decidedAt)    // NullString → *time.Time
    p.Round1ClosesAt = parseNullTime(r1)
    // ...decode the JSON columns...
    if err := json.Unmarshal([]byte(rules), &p.SubmissionRules); err != nil {
        return nil, fmt.Errorf("decode submission_rules: %w", err)
    }
    return &p, nil
}
```

Everything an ORM does invisibly is here, in the open:

- **`row.Scan(&dest...)` copies columns into variables by position.** You pass
  *pointers* (`&p.ID`) so `Scan` can write into them. The order must match the
  `SELECT` exactly — hence the shared `pollSelect` const, so you maintain the
  column list once.
- **Type mismatches are handled by hand.** SQLite has no boolean type, so bools
  are stored as `INTEGER` and converted with `allowGuests != 0`. Nullable columns
  are scanned into `sql.NullString` (a struct with `.String` and `.Valid`) and
  converted to `*time.Time` via the `parseNullTime` helper. The mapping is
  explicit and you can see exactly what each column becomes.
- **JSON columns are sub-documents.** `submission_rules`, `voting_config`, and
  `genres` are stored as JSON text and `json.Unmarshal`-ed into their Go types.
  This is a pragmatic middle ground — structured-but-flexible data lives as JSON
  in a column rather than its own table. (`voting_config` *has* to be opaque JSON
  because each voting method defines its own config shape — Chapter 6.)
- **"No rows" becomes a domain error.** `sql.ErrNoRows` is translated to
  `poll.ErrNotFound` right here, so the rest of the app deals only in domain
  errors and never imports `database/sql`. The HTTP layer's `poll.IsNotFound`
  check (Chapter 3) closes that loop into a clean 404.

### The `rowScanner` trick (one scanner, two query types)

Notice `scanPoll` takes a `rowScanner`, not a concrete `*sql.Row`:

```go
// rowScanner is the common Scan surface of *sql.Row and *sql.Rows.
type rowScanner interface {
    Scan(dest ...any) error
}
```

`*sql.Row` (single result) and `*sql.Rows` (a result set you iterate) *both* have
a `Scan(...)` method, so both satisfy this one-method interface. That lets
`scanPoll` serve both `GetPollByCode` (single row) and `ListPolls`/
`ListActiveTimedPolls` (looping over many rows) without duplicating the 40-line
scan. It's a perfect miniature of "define the narrowest interface you need" from
Chapter 1.

Multi-row reads follow the standard `database/sql` loop:

```go
rows, err := s.db.QueryContext(ctx, pollSelect+` ORDER BY created_at DESC`)
if err != nil { return nil, err }
defer rows.Close()                 // always close the result set
var out []*poll.Poll
for rows.Next() {                  // advance to the next row
    p, err := s.scanPoll(rows)
    if err != nil { return nil, err }
    out = append(out, p)
}
return out, rows.Err()             // check for an error that ended the loop
```

The shape is always: `Query` → `defer rows.Close()` → `for rows.Next()` →
`Scan` → finally `rows.Err()` (an error can terminate `rows.Next()` and you must
check it explicitly). Memorize this loop; it's every list query in the file.

---

## 5.5 Writes and transactions

Inserts/updates use `ExecContext` (no rows returned), and the result's
`RowsAffected()` doubles as a "did this actually hit a row?" check:

```go
// internal/store/store.go
func mustAffect(res sql.Result) error {
    n, err := res.RowsAffected()
    if err != nil { return err }
    if n == 0 { return poll.ErrNotFound }   // updated nothing → the row didn't exist
    return nil
}
```

For operations that must be **atomic** — touch several rows or all-or-nothing —
there's a small transaction helper that captures the classic begin/rollback/
commit dance once:

```go
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil { return err }
    if err := fn(tx); err != nil {
        _ = tx.Rollback()      // any error → roll everything back
        return err
    }
    return tx.Commit()
}
```

This is the **"execute around a transaction"** pattern: you hand `tx` a function
that does the work using the provided `*sql.Tx`; the helper guarantees the
transaction is either fully committed or fully rolled back. It's the Go analogue
of a `with session.begin():` context manager. `CreatePoll` uses it to insert the
poll *and* its host participant as one unit:

```go
return s.tx(ctx, func(tx *sql.Tx) error {
    if _, err := tx.ExecContext(ctx, `INSERT INTO polls (...) VALUES (...)`, ...); err != nil {
        return err
    }
    _, err := tx.ExecContext(ctx, `INSERT INTO participants (...) VALUES (...)`, ...)
    return err
})
```

If the participant insert fails, the poll insert is rolled back too — you never
get a poll with no host. `AddNomination` (find-or-create the nomination, then
attach the nominator) and `ReplaceVotes` (delete the old ballot, insert the new)
use the same helper for the same reason.

### Find-or-create and idempotent upserts

`AddNomination` is a good read for how the "merge duplicate nominations" rule is
implemented at the SQL level — inside one transaction it looks up an existing
nomination for `(poll_id, jellyfin_item_id)`, inserts it only if absent, then
adds the nominator with an idempotent upsert:

```go
if _, err := tx.ExecContext(ctx, `
    INSERT INTO nomination_nominators (nomination_id, participant_id, created_at)
    VALUES (?,?,?)
    ON CONFLICT(nomination_id, participant_id) DO NOTHING`,   // re-nominating is a no-op
    existingID, nominatorID, nowText()); err != nil {
    return err
}
```

`ON CONFLICT ... DO NOTHING` makes re-nominating the same title harmlessly
idempotent. `seerr_requests` uses `ON CONFLICT ... DO UPDATE SET status = ...` so
a winner is requested at most once but its status can refresh. These are SQLite
upserts — worth knowing if you've only used them via an ORM before.

---

## 5.6 Time, and why it's stored as text

SQLite has no native timestamp type, so times are stored as **UTC RFC3339Nano
strings**, via a few helpers:

```go
func nowText() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func timeText(t *time.Time) any {       // nil-safe: returns nil for a nil *time.Time
    if t == nil { return nil }
    return t.UTC().Format(time.RFC3339Nano)
}
func parseNullTime(ns sql.NullString) *time.Time { /* "" or NULL → nil, else parse */ }
```

Storing UTC RFC3339 has a neat property exploited by the retention sweeper: the
string sort order *is* chronological, so a lexicographic SQL comparison works as
a time comparison:

```go
// DeleteClosedPollsBefore — purge closed polls older than a cutoff
`DELETE FROM polls
 WHERE status = ?
   AND COALESCE(NULLIF(closed_at, ''), NULLIF(decided_at, '')) IS NOT NULL
   AND COALESCE(NULLIF(closed_at, ''), NULLIF(decided_at, '')) < ?`   // string < works because UTC RFC3339
```

(`closed_at` is the authoritative end time, stamped on *every* close;
`decided_at` is a fallback for older rows and for frozen-winner methods. Both
nullable, hence the `COALESCE`/`NULLIF`.)

---

## 5.7 One efficiency pattern worth stealing: grouped counts

The admin dashboard needs participant/nomination/voter counts for *every* poll.
The naive approach is N+1 queries (one count per poll per metric). Instead,
`AllPollCounts` runs three **grouped** queries and assembles a map keyed by poll
ID:

```go
// internal/store/store.go (shape)
groupedCount(`SELECT poll_id, COUNT(*) FROM participants GROUP BY poll_id`, setParticipants)
groupedCount(`SELECT poll_id, COUNT(*) FROM nominations  GROUP BY poll_id`, setNominations)
groupedCount(`SELECT poll_id, COUNT(DISTINCT participant_id) FROM votes GROUP BY poll_id`, setVoters)
```

`groupedCount` is a local closure taking a query and a setter callback — three
queries total, regardless of how many polls exist. The service then zips these
counts onto the poll list (`ListHistory`). If you've ever fought the N+1 problem
in an ORM, this is the same fix done by hand and fully visible.

---

## 5.8 The mental shift from ORM-land

| You're used to (SQLAlchemy) | Here (`database/sql`) |
|------------------------------|------------------------|
| `session.query(Poll).get(id)` | write `SELECT`, `QueryRow`, `Scan` into vars |
| Models map columns ↔ attributes automatically | you map each column by hand in `scanPoll` |
| Relationships / lazy loading | explicit second query (`ListNominations` attaches nominators) |
| `session.add()/commit()` | `tx(ctx, func(tx){ ... })` |
| Alembic migrations | `CREATE TABLE IF NOT EXISTS` + `addColumns` |
| `Mapped[bool]` | `INTEGER` column + `x != 0` |
| `Optional[datetime]` | `sql.NullString` + `parseNullTime` → `*time.Time` |

It's more typing, but there's no hidden behavior, no session-state surprises, no
lazy-load N+1 ambushes, and one fewer large dependency. For a focused app with a
handful of tables, many Go developers consider this a feature, not a hardship —
and reading it teaches you exactly what an ORM was doing for you.

[Next: the voting engine — interfaces and the plugin registry »](06-the-voting-engine.md)

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

seeurchin is a self-hosted, group movie/show picker for Jellyfin: a Go backend (chi + pure-Go SQLite) serving a REST + SSE API and an **embedded** SvelteKit (Svelte 5 + Tailwind v4) single-page app. See `README.md` for the product/feature/config reference and `docs/design-system.md` for the "Reef" UI system — this file focuses on how the code fits together.

## Commands

```sh
# Backend tests (all)
go test ./...
# A single package / single test
go test ./internal/voting -run TestRankedIRVRedistribution -v
go vet ./...
gofmt -l internal/...            # list unformatted files; -w to fix

# Run the backend on :5859 (the frontend dev server proxies /api here)
JELLYFIN_URL=http://localhost:8096 JELLYFIN_API_KEY=... SEEURCHIN_ADDR=:5859 go run ./cmd/seeurchin

# Frontend dev server (:5173, hot reload, proxies /api → :5859)
cd web && npm install && npm run dev
npm --prefix web run check       # svelte-check (type/lint)

# Full build: frontend → embed dir → static Go binary
npm --prefix web ci
npm --prefix web run build       # writes internal/httpapi/webdist/
CGO_ENABLED=0 go build -o seeurchin ./cmd/seeurchin

docker build -t seeurchin:latest .   # multi-stage: Node build → Go build → distroless
```

To point the dev frontend at a running container instead of a local `go run`, set `SEEURCHIN_API_PROXY=http://localhost:5858` (see `web/vite.config.ts`).

## Architecture

**The frontend is compiled into the binary.** `npm run build` (svelte adapter-static, SPA fallback) writes to `internal/httpapi/webdist/`, which `internal/httpapi/web.go` pulls in via `//go:embed all:webdist`. A committed placeholder `webdist/.gitkeep` lets the Go package compile before any frontend build. **Consequence:** a backend-only rebuild serves whatever HTML/JS is currently in `webdist/` — re-run `npm run build` to see frontend changes in the Go server. During `vite dev` this is bypassed: the dev server serves the app and proxies `/api` to the backend.

**Request flow & layering** (`cmd/seeurchin/main.go` wires it all):
`chi` router (`internal/httpapi/server.go`) → handlers (`handlers.go`) → `poll.Service` (`internal/poll/service.go`, all business rules + state machine) → `poll.Repository` (implemented by `internal/store`, SQLite). The domain (`internal/poll`) never imports Jellyfin: `main.go` provides an `itemResolver` adapter satisfying `poll.ItemResolver`, so the service resolves item IDs without a Jellyfin dependency. Handlers translate `*poll.Error` (which carries an HTTP status) to responses via `writeErr`.

**Poll state machine** (`poll.Status`): `round1` (nominate) → `round2` (vote) → `closed`. The host drives transitions via `Service.Advance`. Polls are created directly into `round1`.

**Voting engine** (`internal/voting`) is a pluggable registry. Each method (`approval`, `ranked`, `score`, `random`) implements the `Method` interface and self-registers in `init()`; a poll persists only the method **key** plus an opaque `voting_config` JSON blob the method interprets. To add a method: implement `Method` and `Register` it — nothing else changes. `random` additionally implements `AutoDecider`: such methods have **no round 2** — `Advance` from `round1` draws the winner immediately, persists it to `winner_nomination_id`, and closes the poll. A frozen `WinnerNominationID` always overrides the live tally in `Service.Results`.

**Self-vote rules** are resolved by `selfVoteLimit` in `voting.go`: the newer `max_self_votes` (`<0` unlimited, `0` none, `N` cap) is authoritative when present and overrides the legacy `allow_self_vote` bool. Methods enforce vote budgets / per-option caps / self-vote limits server-side in `ValidateBallot`.

**Live updates (SSE)** — `internal/httpapi/sse.go`. A single in-memory `Hub` keyed by poll ID fans out to subscribers. Events carry only a type string (`"nominations"`, `"votes"`, `"status"`, `"results"`); clients **refetch full state** on receipt. After any mutating handler, call `s.broadcast(pollID, type)`. Slow subscribers drop messages (they catch up on the next refetch). Not multi-instance safe — one process only.

**Sessions / identity** — `internal/auth`. One HMAC-signed, HTTP-only cookie (`seeurchin_session`) holds a **map of pollID → session token**, so a browser can hold roles across multiple polls. There are no accounts: participants are guests created on create/join. The model is built behind an `auth.Provider` seam and `participants.jellyfin_user_id` already exists (unused) so Jellyfin login can be added with no schema migration.

**Nominations snapshot the item** at nomination time (`ItemSnapshot`) so ballots render even if the library changes. The nomination row is keyed by `jellyfin_item_id`, which doubles as a surrogate for Seerr write-ins: library items use the real Jellyfin ID; write-ins use `seerr:<movie|tv>:<tmdbID>`. Multiple people nominating the same title are merged into one nomination with a `nomination_nominators` join row each.

**External services**
- **Jellyfin** (`internal/jellyfin`): library search + item lookup using the modern `Authorization: MediaBrowser` header. Poster images are **proxied** through `GET /api/items/{id}/image` so the browser never sees the Jellyfin URL or API key.
- **Seerr** (`internal/seerr`, optional — enabled only when `SEERR_URL` + `SEERR_API_KEY` set, `Server.seerr` is nil otherwise): TMDB write-in search and auto-requesting a winning write-in. Always guard Seerr code paths with `seerrEnabled()`.

**Share codes** (`internal/codes`): 6-char Crockford base32 (no `O/0/I/1/L`), generated unique against the store. `codes.Normalize` canonicalizes user-entered codes before lookup.

**Link-preview cards** — `internal/httpapi/preview.go`. `/p/{code}` is server-rendered with Open Graph tags (registered *before* the SPA catch-all) and `/p/{code}/preview.png` generates a branded share card; real browsers still boot the SPA from the injected page.

**Persistence** — `internal/store`: pure-Go `modernc.org/sqlite` (no CGO). The schema is an idempotent `CREATE TABLE IF NOT EXISTS` block applied on `Open`; WAL + foreign keys + busy-timeout pragmas are set in the DSN. `MaxOpenConns(1)` serializes writes (the driver isn't safe for concurrent writers on one connection).

## Frontend notes

- Svelte 5 **runes mode is forced** for app code (`svelte.config.js`). Only two routes: `/` (`+page.svelte`, create/home) and `/p/[code]` (the whole poll lifecycle UI). Everything else is components in `web/src/lib/components/`.
- `web/src/lib/api.ts` is the single typed API client; types in `web/src/lib/types.ts` mirror the Go view structs (`internal/httpapi/views.go`). When you change a backend response shape, update both.
- Theming is token-driven (`web/src/routes/layout.css`): light values in `@theme`, dark re-declares the same `--color-*` vars under `[data-theme="dark"]`, so **no `dark:` variants** — use semantic utilities (`bg-surface`, `text-ink`, `bg-primary`, …), never raw hex or the old `brand-*` palette. Theme store: `web/src/lib/theme.svelte.ts`; no-flash inline script in `app.html`. Reuse the `@layer components` classes (`.btn`, `.card`, `.opt`, `.pill`, …) — see `docs/design-system.md` for the full token/class/role reference and the color rule-of-thumb (ocean primary = main action, coral = host round-advance CTA, teal accent = identity/selection/focus).

## Conventions

- Commit at logical points as components finish, rather than one large commit at the end.
- Keep all configuration in env vars via `internal/config` (`FromEnv`); only Jellyfin URL + API key are required. Re-run `tools/screenshots` after a UI change to refresh the README gallery.

# seeurchin 🌊🦔

A self-hosted group movie/show picker for [Jellyfin](https://jellyfin.org/). Drop a
link in a group chat and let everyone collectively choose what to watch through a
two-round poll:

1. **Round 1 — Submissions.** Participants browse/search your Jellyfin library and
   nominate titles, subject to host-defined controls (min / max / required count).
2. **Round 2 — Voting.** Everyone votes on the nominated set using a configurable
   voting method (approval, ranked-choice, or score).

Mobile-first, joinable via a short share code, supports guest users (no account
needed), and runs as a single container alongside your existing Jellyfin stack.

## Status

Early development. See the implementation plan for the roadmap. MVP scope:
anonymous/guest identity, all three voting methods, live updates, Dockerized.
Jellyfin user login (modern auth) and Seerr integration are planned follow-ups.

## Stack

- **Backend:** Go (`chi`, pure-Go SQLite via `modernc.org/sqlite`), embedded frontend.
- **Frontend:** SvelteKit (static) + Tailwind CSS.
- **Live updates:** Server-Sent Events.

## Configuration

Configured entirely via environment variables:

| Variable | Required | Default | Description |
|---|---|---|---|
| `JELLYFIN_URL` | yes | — | Jellyfin base URL, e.g. `http://jellyfin:8096` |
| `JELLYFIN_API_KEY` | yes | — | API key (Dashboard → API Keys) for library reads |
| `SEEURCHIN_BASE_URL` | no | `http://localhost:5858` | Public origin for share links |
| `SEEURCHIN_ADDR` | no | `:5858` | Listen address |
| `SEEURCHIN_DB_PATH` | no | `./seeurchin.db` | SQLite file path |
| `SEEURCHIN_SESSION_SECRET` | recommended | random | HMAC secret for session cookies |
| `SEEURCHIN_CODE_STYLE` | no | `base32` | Share-code style |
| `SEEURCHIN_ENABLE_USER_LOGIN` | no | `false` | Enable Jellyfin login (Phase 2) |

## Development

```sh
go test ./...        # backend tests
go run ./cmd/seeurchin
```

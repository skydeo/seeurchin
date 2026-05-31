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

## Quick start (Docker)

```sh
export JELLYFIN_API_KEY=...          # Jellyfin Dashboard → API Keys
export SEEURCHIN_SESSION_SECRET=$(openssl rand -hex 32)
export SEEURCHIN_BASE_URL=http://localhost:5858
docker compose up --build
```

Then open <http://localhost:5858>. The image is ~13 MB (distroless + a static Go
binary with the frontend embedded).

### Adding it to an existing Jellyfin stack

Drop this service into your stack's `docker-compose.yml`, on the same network as
Jellyfin, and route a hostname to it through your reverse proxy / Cloudflare tunnel
(then you can drop the `ports` mapping):

```yaml
  seeurchin:
    image: seeurchin:latest      # or: build: ./seeurchin
    container_name: seeurchin
    environment:
      - TZ=America/Chicago
      - JELLYFIN_URL=http://jellyfin:8096
      - JELLYFIN_API_KEY=<dashboard API key>
      - SEEURCHIN_BASE_URL=https://seeurchin.example.com
      - SEEURCHIN_SESSION_SECRET=<random 32+ bytes>
    volumes:
      - ./config/seeurchin:/config
    ports:
      - "5858:5858"
    networks:
      - media-network
    restart: unless-stopped
```

## Development

Backend (serves API on :5859 here; the frontend dev server proxies `/api` to it):

```sh
go test ./...                                   # backend tests
JELLYFIN_URL=http://localhost:8096 \
JELLYFIN_API_KEY=... \
SEEURCHIN_ADDR=:5859 \
go run ./cmd/seeurchin
```

Frontend (hot-reloading dev server on :5173):

```sh
cd web
npm install
npm run dev
```

## Build

The frontend compiles to static files embedded in the Go binary:

```sh
npm --prefix web ci
npm --prefix web run build        # writes internal/httpapi/webdist/
CGO_ENABLED=0 go build -o seeurchin ./cmd/seeurchin
```

## Project layout

```
cmd/seeurchin        entrypoint + Jellyfin→domain adapter
internal/config      env-var configuration
internal/jellyfin    Jellyfin client (modern auth header, search, image proxy)
internal/store       SQLite repository (modernc.org/sqlite)
internal/poll        domain types + service (state machine, rules)
internal/voting      pluggable voting engine (approval, ranked, score)
internal/codes       Crockford base32 share codes
internal/auth        session cookies + provider seam (guest now, Jellyfin later)
internal/httpapi     REST + SSE handlers, embedded SPA
web/                 SvelteKit + Tailwind frontend
```

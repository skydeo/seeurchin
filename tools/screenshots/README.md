# README screenshot generator

Drives a **running** seeurchin instance with Playwright (using your installed
Google Chrome — no bundled browser download) to capture the mobile screenshots
embedded in the top-level `README.md`. It walks the real two-round flow against
your real Jellyfin library, so the posters are genuine. Shots are **dark theme
by default**.

Output (390×844 @2x) is written to `../../docs/screenshots/`:

| File | Screen |
|---|---|
| `home.png` | Landing / create-a-poll |
| `nominate.png` | Round 1 — nominations grid (Approval poll) |
| `vote.png` | Round 2 — Approval ballot |
| `results.png` | Winner reveal + full results |
| `results-light.png` | Same results in **light** theme (for the README theming pair) |
| `vote-ranked.png` | Ranked-choice ballot |
| `vote-score.png` | Star / score ballot |
| `random.png` | Random-pick result (no ballot) |
| `browse-genres.png` | "Add titles" modal — type + scrollable genre filter |

## Prerequisites

- A seeurchin frontend reachable at `SEEURCHIN_ORIGIN`, with `/api` reaching a
  backend whose Jellyfin library has ≥6 movies with posters.
- Google Chrome installed (the script launches it via Playwright's `channel: 'chrome'`).
- Node.js. `node_modules/` is committed-ignored; if it's missing, run `npm install`.

## Run

Capture against a frontend that reflects current source. Easiest is the dev
server with its `/api` proxied to the running backend:

```sh
# terminal 1 — dev server (vite :5173) proxied to the live backend
cd web
SEEURCHIN_API_PROXY=http://localhost:5858 npm run dev

# terminal 2 — capture
cd tools/screenshots
npm install                                   # only if node_modules/ is missing
SEEURCHIN_ORIGIN=http://localhost:5173 npm run shoot
```

To shoot a built instance instead, point `SEEURCHIN_ORIGIN` straight at it
(e.g. `http://localhost:5858`).

## Options (env vars)

| Var | Default | Purpose |
|---|---|---|
| `SEEURCHIN_ORIGIN` | `http://localhost:5173` | The frontend to drive. |
| `SEEURCHIN_SHOT_OUT` | `../../docs/screenshots` | Where PNGs are written. |
| `SEEURCHIN_SHOT_FILMS` | _(curated set in `shoot.mjs`)_ | Comma-separated titles to feature, overriding the default `DEFAULT_FILMS` list. Each is looked up in the library; any leftover slots (need 6 total) are filled from the default browse list. |

```sh
# pin specific films:
SEEURCHIN_SHOT_FILMS="The Matrix, Spirited Away, Parasite, Heat, Arrival, Up" npm run shoot
```

## Notes

- The demo poll is created with **write-ins and auto-request disabled**, so it
  never triggers a real Seerr/Radarr/Sonarr request.
- It creates a throwaway poll (host "Alex" + a guest "Maya") plus a couple of
  ballots in your instance's DB each run. Harmless, but it does leave rows
  behind — there's no delete endpoint yet.
- `debug.mjs <code>` opens a poll page with console/network logging — handy when
  a screen won't render.
- The featured films are curated in `DEFAULT_FILMS` at the top of `shoot.mjs`;
  edit that list to change the standing set.

# seeurchin — "Reef" design system

> **Status:** Applied to the app on 2026-06-02. This document is the design-system
> reference for building new UI that matches the brand. The "What changed" and
> "drop-in" sections below are historical migration notes (the re-skin is already
> in `web/`). Brand source masters (fonts, icon masters, white/ink mark cuts) live
> in `docs/brand/`.

This package restyles the seeurchin frontend to the **Reef** brand and adds a
**system/light/dark** appearance toggle. It's a pure **re-skin + theming** pass —
no API calls, routes, types, voting logic, or component props changed. Every
`+page.svelte` / component keeps its original `<script>` behavior; only markup
classes and a few small additions changed.

Hand this to Claude Code for continued feature work — the design system below is
the contract for building new UI that matches.

---

## What changed

**New files**
| File | Purpose |
|---|---|
| `src/lib/theme.svelte.ts` | Appearance store: `system` / `light` / `dark`, persisted, writes `data-theme` on `<html>`. |
| `src/lib/confetti.ts` | Brand-colored confetti burst for the winner reveal. |
| `src/lib/components/ThemeToggle.svelte` | The header pill toggle (cycles Auto → Light → Dark). |
| `src/lib/components/UrchinMark.svelte` | The 12-spike "Reef" urchin mark, sizable. |
| `static/brand/*` | Favicon + app-icon kit (16/32/48 PNG, apple-touch-180, icon-192/512 maskable, mark SVG). |
| `static/manifest.webmanifest` | PWA manifest (name, theme colors, maskable icons). |

**Replaced files**
| File | What changed |
|---|---|
| `src/routes/layout.css` | Full Reef theme: Tailwind v4 `@theme` tokens (light) + `[data-theme="dark"]` overrides + a `@layer components` design-system layer. |
| `src/app.html` | Brand font links, **favicon + app-icon + manifest** links, theme-color meta, and a **no-flash** inline script that sets `data-theme` before first paint. |
| `src/routes/+layout.svelte` | Imports `layout.css`; initializes the theme store. (Favicons now live in `app.html`.) |
| `src/lib/assets/favicon.svg` | **Removed** — icons now served from `static/brand/` (see `app.html`). |
| `src/routes/+page.svelte` | Home (join + create) restyled; mark lockup + theme toggle. |
| `src/routes/p/[code]/+page.svelte` | Loading/error states restyled. |
| `src/lib/components/PollHeader.svelte` | Mark lockup, status pill, code button, theme toggle. |
| `src/lib/components/JoinForm.svelte` | Restyled card + mark. |
| `src/lib/components/Round1.svelte` | Nominate grid, badges, browse modal restyled. |
| `src/lib/components/Ballot.svelte` | Approval / ranked / star ballots restyled. |
| `src/lib/components/Results.svelte` | Winner card + sun ring + confetti on mount; tally bars. |
| `src/lib/components/PosterImage.svelte` | Uses `.poster` / `.poster-fallback`. |
| `src/lib/components/LiveResults.svelte` | Tally bars restyled. |

**Drop-in:** copy this `web/src/` over the repo's `web/src/`. No `package.json`
or config changes are required — it's still SvelteKit (Svelte 5) + Tailwind v4.

```sh
cd web && npm install && npm run dev   # dev on :5173, proxies /api → :5859
npm run build                          # embeds into internal/httpapi/webdist/
```

---

## Theming — how it works

The active theme is the attribute **`<html data-theme="light|dark">`**.

1. **No-flash:** the inline script in `app.html` reads `localStorage['seeurchin-theme']`
   (default `system`) and sets `data-theme` before paint.
2. **Tailwind v4 token swap:** light values live in `@theme` in `layout.css`;
   `[data-theme="dark"]` re-declares the **same `--color-*` custom properties**.
   Every utility (`bg-surface`, `text-ink`, …) reads `var(--color-…)`, so colors
   adapt automatically — **no `dark:` variants needed** anywhere.
3. **Store:** `src/lib/theme.svelte.ts` exports a singleton `theme` with
   `theme.mode`, `theme.set(mode)`, `theme.cycle()`. It persists to localStorage
   and re-applies on OS scheme change while in `system` mode.

```svelte
import { theme } from '$lib/theme.svelte';
theme.cycle();        // system → light → dark → system
theme.set('dark');    // explicit
theme.mode;           // reactive: 'system' | 'light' | 'dark'
```

---

## Design tokens

Defined in `src/routes/layout.css`. Use the **semantic** names, never raw hex.

### Color roles
| Token (utility) | Role | Light | Dark |
|---|---|---|---|
| `bg` | App background (sand / deep ocean) | `#fdf7ec` | `#072e3d` |
| `surface` | Cards | `#ffffff` | `#0d4055` |
| `surface2` | Raised panels / host cards | `#fbf4e6` | `#114a60` |
| `surface3` | Inputs, insets, unselected | `#f3ead6` | `#0a3645` |
| `line` / `line2` | Borders / stronger borders | `#ece2cf` / `#e0d3b6` | translucent |
| `ink` / `muted` / `faint` | Text: primary / secondary / tertiary | `#143a45` / `#50707a` / `#93a4ab` | `#e9f3f1` / `#a3c0c5` / `#6f9298` |
| `primary` | **Primary actions** (create, submit, join) — ocean | `#0e5a7d` | `#1d7aa3` |
| `accent` / `accent-ink` | Identity, links, selected accents, focus — teal | `#0f9a92` / `#0c7d76` | `#1ccfc3` / `#3be0d4` |
| `coral` / `coral-ink` | **Host round-advance CTA**, "yours" badge, errors | `#ff6f5e` / `#d64a3a` | `#ff7d6d` / `#ff9e8f` |
| `mango` | Stars, "Nominating" status, request badge | `#ffa23a` | `#ffb056` |
| `sun` | Winner celebration | `#ffce5c` | `#ffd76e` |

**Semantic rule of thumb:** ocean `primary` = the main button on a screen;
teal `accent` = identity/links/selected highlights and focus rings; coral = the
host's *move-the-round-forward* button (deliberately distinct & exciting);
mango/sun = stars + winner celebration; status pills are mango (Nominating),
teal (Voting), sun (Results).

### Type
- `font-display` → **Baloo 2** — wordmark, poll titles, winner title.
- `font-title` → **Quicksand** — section headers, control labels, eyebrows, codes.
- `font-sans` (default) → **Nunito** — all body/UI text. (Loaded in `app.html`.)

### Radii
`--radius-sm 10px`, `--radius-md 14px`, `--radius-lg 20px`, `--radius-xl 26px`.

---

## Component classes (`@layer components`)

Prefer these for the "designed" elements; use Tailwind utilities for layout
(`flex`, `grid`, `gap-*`, `px-*`). All are theme-aware automatically.

- **Buttons:** `.btn` + one of `.btn-primary` (ocean), `.btn-coral` (host CTA),
  `.btn-ghost`; size with `.btn-sm`.
- **Inputs:** `.input` (+ `.input-code` for the share-code field), `.select`.
- **Containers:** `.card`, `.panel`.
- **Choosers:** `.opt` (segmented) and `.chip` (genre/filter). Selected state:
  add `is-on` (e.g. `class:is-on={selected}`).
- **Switch:** `.switch` with `role="switch" aria-checked={bool}`.
- **Status pill:** `.pill` + `.pill-round1|round2|closed|draft`.
- **Posters:** `.poster` (+ `.poster-fallback` for the no-image state),
  `.badge` + `.badge-yours|count|req`, `.poster-pick` + `.poster-pick-on|lib`.
- **Results bar:** `.bar` (+ `.bar-win` for the winner).
- **Winner pop:** `.winner-pop` (enhancement only — never gates visibility;
  respects `prefers-reduced-motion`).

---

## The mark & favicon

`UrchinMark.svelte` renders the 12-spike mark (`<UrchinMark size={40} />`).
Pair it with a lowercase **Baloo 2** "seeurchin" wordmark for lockups; the brand
drops the old 🌊🦔 emoji.

Favicons, the iOS home-screen icon, and the PWA manifest are wired in `app.html`,
served from `web/static/brand/` + `web/static/manifest.webmanifest`:
- tab favicon: `favicon-16/32.png` + `seeurchin-mark.svg`
- iOS “Add to Home Screen”: `apple-touch-icon-180.png` (sand squircle)
- Android/PWA install: `icon-192/512.png` (maskable, with safe-zone padding)

The brand kit also ships `favicon-48.png`, `appicon-sand/ocean-512/1024.png`, and
the `seeurchin-mark-white/ink.svg` cuts if you need them elsewhere.

---

## Confetti

```svelte
import { launchConfetti } from '$lib/confetti';
import { onMount } from 'svelte';
onMount(() => { if (hasWinner) launchConfetti(hostEl); });
```
`Results.svelte` already wires this: a fixed full-viewport overlay div is the
`host`, fired once on mount when there's a winner, and re-fired when the user
taps the winning poster. No-op under reduced-motion.

---

## Conventions for new UI (please follow)

- **Never hardcode** slate/`#hex` colors or use the old `brand-*` palette. Use
  the semantic tokens (`bg-surface`, `text-ink`, `bg-primary text-on-primary`, …)
  so light/dark both work for free.
- **Pick the right role color** (see the rule of thumb above) rather than
  reaching for accent everywhere.
- **Mobile-first**; hit targets ≥ 44px; keep the existing density.
- Reuse the component classes; only add to the `@layer components` block in
  `layout.css` when a genuinely new primitive is needed.
- Match the existing copy voice (friendly, lowercase wordmark, plain language).

## Before shipping
- [ ] `npm run build` and visually QA all four states (Home, Round 1, Ballot ×3
      methods, Results) in **both** light and dark.
- [ ] Confirm real Jellyfin posters fill `.poster` correctly (the prototype used
      placeholders; `PosterImage` keeps the real proxy/`onerror` fallback).

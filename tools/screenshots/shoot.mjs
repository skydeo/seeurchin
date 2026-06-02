import { chromium } from 'playwright';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
// Capture against a frontend that reflects current source. Point this at the
// dev server (vite, :5173) with its /api proxied to the backend, or at a built
// instance (:5858). Override with SEEURCHIN_ORIGIN.
const ORIGIN = process.env.SEEURCHIN_ORIGIN || 'http://localhost:5173';
// default: ../../docs/screenshots relative to this script (repo docs/screenshots)
const OUT = process.env.SEEURCHIN_SHOT_OUT || path.resolve(__dirname, '../../docs/screenshots');
const VP = { width: 390, height: 844 };
const DSF = 2;

// The titles featured in the demo poll. Each is looked up in the library; any
// slots left over are filled from the default browse list. Needs 6 total for
// the nominate/vote/results layout. Override per-run with a comma-separated
// SEEURCHIN_SHOT_FILMS="Title A, Title B, …".
//
// Slot meaning (main poll): #2 is the shared pick (both Alex & Maya nominate
// it -> ×2) and is set up to win; #5 and #6 are Maya-only; the rest are Alex's.
const DEFAULT_FILMS = [
  'Past Lives',
  'Arrival', // shared pick + winner
  'Spider-Man: Across the Spider-Verse',
  'Luca',
  'Knives Out', // Maya
  'Your Name' // Maya
];
const FILMS = (process.env.SEEURCHIN_SHOT_FILMS
  ? process.env.SEEURCHIN_SHOT_FILMS.split(',')
  : DEFAULT_FILMS
)
  .map((s) => s.trim())
  .filter(Boolean);

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// fetch helper that runs inside a page so cookies (session) are shared
async function apiCall(page, method, path, body) {
  return await page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(path, {
        method,
        headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
        body: body !== undefined ? JSON.stringify(body) : undefined,
        credentials: 'same-origin'
      });
      const text = await res.text();
      const data = text ? JSON.parse(text) : null;
      if (!res.ok) throw new Error((data && data.error) || res.statusText);
      return data;
    },
    { method, path, body }
  );
}

// wait until poster <img>s have actually decoded (or give up after timeout).
// NOTE: poll pages hold an SSE connection open, so 'networkidle' never fires —
// poll the DOM instead.
async function waitPosters(page, timeout = 8000) {
  const start = Date.now();
  while (Date.now() - start < timeout) {
    const ok = await page.evaluate(() => {
      const imgs = [...document.querySelectorAll('.poster img')];
      if (imgs.length === 0) return true;
      return imgs.every((i) => i.complete && i.naturalWidth > 0);
    });
    if (ok) break;
    await sleep(150);
  }
  await sleep(450); // fonts + layout settle
}

// theme: 'dark' (default for these shots) | 'light'
async function newCtx(browser, theme = 'dark') {
  const ctx = await browser.newContext({
    viewport: VP,
    deviceScaleFactor: DSF,
    colorScheme: theme === 'light' ? 'light' : 'dark'
  });
  await ctx.addInitScript((t) => {
    try { localStorage.setItem('seeurchin-theme', t); } catch {}
  }, theme);
  return ctx;
}

// Navigate to a poll page and wait for the expected screen heading. The long-
// lived browser context can stall a client fetch behind lingering SSE streams
// on HTTP/1.1, so retry with a hard reload if the heading doesn't appear.
async function gotoPoll(page, code, headingName) {
  const url = `${ORIGIN}/p/${code}`;
  for (let attempt = 0; attempt < 4; attempt++) {
    if (attempt === 0) await page.goto(url, { waitUntil: 'domcontentloaded' });
    else await page.reload({ waitUntil: 'domcontentloaded' });
    try {
      await page.getByRole('heading', { name: headingName }).waitFor({ timeout: 12000 });
      return;
    } catch {
      console.log(`  (stalled on "${headingName}", reloading…)`);
    }
  }
  throw new Error(`never reached "${headingName}" at ${url}`);
}

async function shot(page, name, fullPage = true) {
  const path = `${OUT}/${name}.png`;
  await page.screenshot({ path, fullPage });
  console.log('  saved', name + '.png');
}

const baseBody = (method, cfg) => ({
  title: 'Friday Movie Night',
  host_name: 'Alex',
  library_scope: 'movie',
  voting_method: method,
  voting_config: cfg,
  submission_rules: { min: 0, max: 0, required: 0 },
  allow_guests: true,
  results_live: false,
  reveal_nominators: true,
  reveal_scope: 'all',
  genres: [],
  allow_writeins: false, // SAFE: no external Seerr requests
  auto_request_winner: false
});

// pick 6 library items with posters for a given poll code (host session)
async function pickItems(host, code) {
  const NEED = 6;
  const picked = [];
  const seen = new Set();
  const take = (it) => { if (it && !seen.has(it.id)) { seen.add(it.id); picked.push(it); } };
  for (const title of FILMS) {
    if (picked.length >= NEED) break;
    const res = await apiCall(host, 'GET', `/api/polls/${code}/library?q=${encodeURIComponent(title)}&type=`);
    const hit = res.items.find((i) => i.image_tag) || res.items[0];
    if (hit) take(hit); else console.log(`  (no library match for "${title}")`);
  }
  if (picked.length < NEED) {
    const lib = await apiCall(host, 'GET', `/api/polls/${code}/library?q=&type=`);
    for (const it of lib.items.filter((i) => i.image_tag)) { if (picked.length >= NEED) break; take(it); }
  }
  const items = picked.slice(0, NEED);
  if (items.length < NEED) throw new Error(`only found ${items.length}/${NEED} library items`);
  return items;
}

async function nominate(page, code, item) {
  await apiCall(page, 'POST', `/api/polls/${code}/nominations`, { item_id: item.id });
}

async function nomMap(host, code) {
  const poll = await apiCall(host, 'GET', `/api/polls/${code}`);
  const m = {};
  for (const n of poll.nominations) m[n.item_id] = n.id;
  return m;
}

// Create a poll whose round-2 ballot is the screenshot subject (host nominates
// all 6 so the ballot is full), advance to voting, fill it, and capture.
async function captureMethodBallot(host, { method, cfg, fill, out }) {
  const { code } = await apiCall(host, 'POST', '/api/polls', baseBody(method, cfg));
  const items = await pickItems(host, code);
  for (const it of items) await nominate(host, code, it);
  await apiCall(host, 'POST', `/api/polls/${code}/advance`); // -> round2
  await gotoPoll(host, code, 'Cast your vote');
  await waitPosters(host);
  await fill(host, items);
  await sleep(350);
  await shot(host, out);
}

const run = async () => {
  const browser = await chromium.launch({ channel: 'chrome', headless: true });

  // host = dark (primary look for all shots); guest "Maya" = light (used only
  // for the light-theme results shot in the README's theming comparison).
  const host = await (await newCtx(browser, 'dark')).newPage();
  const guest = await (await newCtx(browser, 'light')).newPage();
  host.on('pageerror', (e) => console.log('    [pageerror]', e.message));

  // ---- HOME (create screen, dark) ----
  console.log('home…');
  await host.goto(ORIGIN, { waitUntil: 'domcontentloaded' });
  await host.getByRole('button', { name: 'Create poll' }).waitFor({ timeout: 20000 });
  await sleep(700);
  await shot(host, 'home');

  const methods = await apiCall(host, 'GET', '/api/methods');
  const cfgOf = (k) => methods.find((m) => m.key === k)?.default_config ?? {};

  // ================= MAIN POLL — Approval (the default method) =================
  console.log('main poll (approval)…');
  const { code } = await apiCall(host, 'POST', '/api/polls', baseBody('approval', cfgOf('approval')));
  const items = await pickItems(host, code);
  console.log('  picked', items.map((i) => i.title).join(' | '));

  // Alex nominates 0..3
  for (const it of items.slice(0, 4)) await nominate(host, code, it);
  // Maya (guest) joins and nominates 1 (overlap -> ×2), 4, 5
  await guest.goto(`${ORIGIN}/p/${code}`, { waitUntil: 'domcontentloaded' });
  await apiCall(guest, 'POST', `/api/polls/${code}/join`, { display_name: 'Maya' });
  for (const it of [items[1], items[4], items[5]]) await nominate(guest, code, it);

  const nid = await nomMap(host, code);

  // NOMINATE (host, round1)
  console.log('nominate…');
  await gotoPoll(host, code, 'Nominations');
  await waitPosters(host);
  await shot(host, 'nominate');

  // advance to voting
  await apiCall(host, 'POST', `/api/polls/${code}/advance`);
  // Maya approves Arrival(1) + Knives Out(4)
  await apiCall(guest, 'POST', `/api/polls/${code}/votes`, {
    selections: { [nid[items[1].id]]: 1, [nid[items[4].id]]: 1 }
  });
  await guest.goto('about:blank'); // park guest SSE while host captures

  // VOTE (host, approval ballot — pick Arrival/Spider-Verse/Luca = rows 1,2,3)
  console.log('vote (approval)…');
  await gotoPoll(host, code, 'Cast your vote');
  await waitPosters(host);
  const rows = host.locator('section .space-y-3 > div');
  for (const i of [1, 2, 3]) {
    await rows.nth(i).getByRole('button', { name: 'Pick', exact: true }).click().catch(() => {});
  }
  await sleep(300);
  await shot(host, 'vote');

  // host casts the matching approval ballot, then close -> clear winner Arrival
  await apiCall(host, 'POST', `/api/polls/${code}/votes`, {
    selections: { [nid[items[1].id]]: 1, [nid[items[2].id]]: 1, [nid[items[3].id]]: 1 }
  });
  await apiCall(host, 'POST', `/api/polls/${code}/advance`);

  // RESULTS (host, dark)
  console.log('results (dark)…');
  await gotoPoll(host, code, 'Full results');
  await waitPosters(host);
  await host.locator('.winner-pop button').first().click().catch(() => {});
  await sleep(250);
  await shot(host, 'results');

  // RESULTS (guest, light) — for the README light/dark theming comparison
  console.log('results (light)…');
  await gotoPoll(guest, code, 'Full results');
  await waitPosters(guest);
  await guest.locator('.winner-pop button').first().click().catch(() => {});
  await sleep(250);
  await shot(guest, 'results-light');

  // ================= VOTING-METHOD BALLOTS (dark) =================
  // Approval already captured above as vote.png; here: ranked, score, random.

  console.log('vote (ranked)…');
  await captureMethodBallot(host, {
    method: 'ranked',
    cfg: cfgOf('ranked'),
    out: 'vote-ranked',
    fill: async (page) => {
      // tap three titles, in order, to build the ranked list
      for (const t of ['Arrival', 'Past Lives', 'Luca']) {
        await page.getByRole('button', { name: t }).first().click().catch(() => {});
        await sleep(200);
      }
    }
  });

  console.log('vote (score)…');
  await captureMethodBallot(host, {
    method: 'score',
    cfg: cfgOf('score'),
    out: 'vote-score',
    fill: async (page) => {
      const r = page.locator('section .space-y-3 > div');
      const pattern = [5, 4, 5, 3];
      for (let i = 0; i < pattern.length; i++) {
        const stars = r.nth(i).getByRole('button', { name: '★' });
        if ((await stars.count()) === 0) continue;
        await stars.nth(pattern[i] - 1).click().catch(() => {});
      }
    }
  });

  // Random has no ballot — the winner is drawn when round 1 closes. Show its
  // result screen instead.
  console.log('random (no ballot — result)…');
  {
    const { code: rc } = await apiCall(host, 'POST', '/api/polls', baseBody('random', cfgOf('random')));
    const its = await pickItems(host, rc);
    for (const it of its) await nominate(host, rc, it);
    await apiCall(host, 'POST', `/api/polls/${rc}/advance`); // draws the winner -> closed
    await gotoPoll(host, rc, 'The other nominations');
    await waitPosters(host);
    await host.locator('.winner-pop button').first().click().catch(() => {});
    await sleep(250);
    await shot(host, 'random');
  }

  await browser.close();
  console.log('\ndone.');
};

run().catch((e) => { console.error(e); process.exit(1); });

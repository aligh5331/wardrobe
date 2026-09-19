// ING-020 — read-only frontend grid UI.
//
// The frontend has no JS test runner installed (no vitest/jsdom/testing-library
// in frontend/node_modules) and Tester may not add dependencies or edit
// frontend/**, so this test runs on Node's built-in test runner and verifies
// the two things that are actually runnable here:
//
//   1. the real Vite build output produced by `npm run build` (AC4), and
//   2. the source-level behaviour the component encodes (AC1-AC3).
//
// It does not install anything and does not run a browser/DOM. Run with:
//   node --test tests/frontend/ing_020_app_test.mjs
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, existsSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const root = join(here, '..', '..')
const frontend = join(root, 'frontend')

const read = (rel) => readFileSync(join(root, rel), 'utf8')

const appSrc = read('frontend/src/App.jsx')
const cssSrc = read('frontend/src/index.css')
const viteSrc = read('frontend/vite.config.js')
const htmlSrc = read('frontend/index.html')
const mainSrc = read('frontend/src/main.jsx')

// --- AC1: fetch GET /api/items and render one card per item --------------
test('AC1: fetches GET /api/items on mount', () => {
  assert.ok(appSrc.includes("fetch('/api/items')"), 'App fetches the relative /api/items URL')
  assert.ok(appSrc.includes('res.ok'), 'non-OK responses are not treated as data')
  assert.match(appSrc, /useEffect\(/, 'the fetch runs in an effect')
  // Relative, same-origin URL: works through the dev proxy and in the built
  // embedded app (ING-021) without CORS.
  assert.ok(!appSrc.includes('http://'), 'no absolute API base URL')
  assert.ok(!appSrc.includes('photo_path'), 'does not reconstruct filesystem paths')
  assert.ok(!appSrc.includes('data/photos'), 'does not reconstruct filesystem paths')
})

test('AC1: renders one ItemCard per item using item.photo_url', () => {
  assert.ok(appSrc.includes('Array.isArray(data) ? data : []'), 'non-array body falls back to empty')
  assert.ok(appSrc.includes("setStatus('ready')"), 'success enters the ready state')
  assert.ok(appSrc.includes('items.map((item) =>'), 'maps items to cards')
  assert.ok(appSrc.includes('<ItemCard key={item.id}'), 'keyed by id, one card per item')
  assert.ok(appSrc.includes('src={item.photo_url}'), 'photo comes from photo_url')
})

test('AC1: each card shows the tagged fields', () => {
  for (const token of [
    'item.category',
    'item.subcategory',
    'item.dominant_color',
    'item.secondary_colors',
    'item.pattern',
    'item.warmth_tier',
    'item.formality',
    'item.added_date',
    'item.notes',
  ]) {
    assert.ok(appSrc.includes(token), `card renders ${token}`)
  }
})

// --- AC2: empty catalog -> empty state, no error -------------------------
test('AC2: empty catalog renders an empty state and no error', () => {
  assert.ok(appSrc.includes('items.length === 0'), 'empty branch keyed on length 0')
  assert.ok(appSrc.includes('No items cataloged yet.'), 'empty-state message present')
  // The error branch is only reachable from the fetch catch, so an empty
  // (but successful) catalog cannot enter it.
  assert.ok(appSrc.includes('Could not load the catalog.'), 'error state text present')
  assert.match(appSrc, /\.catch\(/, 'error state comes from the fetch catch')
})

// --- AC3: broken photo -> placeholder, card still shows fields -----------
test('AC3: photo load failure degrades to a placeholder', () => {
  assert.ok(appSrc.includes('onError={() => setPhotoFailed(true)}'), 'img onError flips the flag')
  assert.ok(appSrc.includes('role="img"'), 'placeholder exposes an img role')
  assert.ok(appSrc.includes('aria-label="Missing photo"'), 'placeholder is labelled')
  assert.ok(appSrc.includes('No photo'), 'placeholder has visible text')
  assert.ok(appSrc.includes("Boolean(item.photo_url) && !photoFailed"), 'empty URL also gets placeholder')
})

test('AC3: the field list renders regardless of photo state', () => {
  // The <dl> is outside the showPhoto conditional: only the <img>/placeholder
  // is swapped. Assert the field list is emitted after the conditional block.
  const conditionalEnd = appSrc.indexOf(')}\n\n      <div')
  const dl = appSrc.indexOf('<dl')
  assert.ok(dl > 0, 'a <dl> field list exists')
  assert.ok(conditionalEnd > 0 && conditionalEnd < dl, 'field list is outside the photo conditional')
})

// --- AC4: npm run build -> frontend/dist/ with Tailwind v4 ---------------
test('AC4: Vite config uses the @tailwindcss/vite plugin', () => {
  assert.ok(viteSrc.includes("from '@tailwindcss/vite'"), 'imports the v4 plugin')
  assert.match(viteSrc, /plugins:\s*\[[^\]]*tailwindcss\(\)/, 'plugin is registered')
  assert.ok(viteSrc.includes("'/api'"), 'dev proxy covers /api')
  assert.ok(viteSrc.includes('http://localhost:8080'), 'proxy targets the Go API')
})

test('AC4: single @import "tailwindcss" entry point, no v3 scaffolding', () => {
  assert.equal(cssSrc.trim(), '@import "tailwindcss";')
  assert.ok(!cssSrc.includes('@tailwind base'), 'no v3 @tailwind directives')
  assert.ok(!cssSrc.includes('@tailwind components'), 'no v3 @tailwind directives')
  assert.ok(!cssSrc.includes('@tailwind utilities'), 'no v3 @tailwind directives')
})

test('AC4: no tailwind.config.js or postcss.config.js', () => {
  const entries = readdirSync(frontend)
  const offenders = entries.filter((f) => /^(tailwind|postcss)\.config\./.test(f))
  assert.deepEqual(offenders, [], `unexpected v3 config files: ${offenders.join(', ')}`)
})

test('AC4: npm run build produced frontend/dist/ with v4 output', () => {
  assert.ok(existsSync(join(frontend, 'dist', 'index.html')), 'dist/index.html exists')
  const assets = readdirSync(join(frontend, 'dist', 'assets'))
  const cssFile = assets.find((f) => f.endsWith('.css'))
  const jsFile = assets.find((f) => f.endsWith('.js'))
  assert.ok(cssFile, 'a built CSS asset exists')
  assert.ok(jsFile, 'a built JS asset exists')

  const builtCss = readFileSync(join(frontend, 'dist', 'assets', cssFile), 'utf8')
  assert.match(builtCss, /@layer theme/, 'Tailwind v4 theme layer emitted')
  assert.match(builtCss, /@layer base/, 'Tailwind v4 base layer emitted')
  assert.match(builtCss, /--color-/, 'v4 theme custom properties emitted')
  assert.ok(builtCss.includes('oklch('), 'v4 default palette (oklch) emitted')

  const builtHtml = readFileSync(join(frontend, 'dist', 'index.html'), 'utf8')
  assert.ok(builtHtml.includes(cssFile), 'index.html links the built CSS')
  assert.ok(builtHtml.includes(jsFile), 'index.html loads the built JS')

  const builtJs = readFileSync(join(frontend, 'dist', 'assets', jsFile), 'utf8')
  for (const marker of ['No items cataloged yet.', 'Could not load the catalog.', 'Missing photo', '/api/items']) {
    assert.ok(builtJs.includes(marker), `built bundle contains: ${marker}`)
  }
})

// --- read-only constraint -------------------------------------------------
test('read-only: no write/edit controls or non-GET requests', () => {
  for (const bad of ['method:', 'POST', 'PUT', 'DELETE', 'PATCH', '<button', '<form', 'onClick']) {
    assert.ok(!appSrc.includes(bad), `App must not contain write/control surface: ${bad}`)
  }
})

// @vitest-environment node
// ING-026 — one runnable check that the Tailwind v4 build/config still produces
// v4 CSS. Restores the structural build/config assertions ING-025 retired
// (ING-020 AC4) as a single Vitest file, run by the existing `npm test`.
//
// AC1: runs the REAL Vite + Tailwind build — the project's own vite.config.js,
//      so the @tailwindcss/vite plugin is what compiles src/index.css — into an
//      isolated outDir, then asserts the emitted stylesheet is non-empty and
//      carries Tailwind v4 output (an @layer at-rule + theme custom properties).
//      A missing plugin / empty stylesheet fails these assertions.
// AC2: reads the build config structurally (@tailwindcss/vite plugin present,
//      src/index.css still the single @import entry, no v3-era config files).
//      That is build configuration, not component behavior — the kind of
//      assertion the ticket explicitly allows by reading files.
// AC3: builds under <project root>/temp/ (gitignored scratch, AGENTS.md §10),
//      never frontend/dist, and asserts the tracked frontend/dist/index.html is
//      byte-for-byte unchanged. Scratch is removed in afterAll, which runs even
//      when a test fails.
// AC4: reuses the already-installed `vite` + `vitest`; no new dependency.
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { mkdir, readFile, readdir, rm } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { build } from 'vite'

// src/ -> frontend/ -> repo root.
const FRONTEND_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const PROJECT_ROOT = resolve(FRONTEND_ROOT, '..')
const TRACKED_PLACEHOLDER = join(FRONTEND_ROOT, 'dist', 'index.html')

// Project scratch dir; temp/ is gitignored (AGENTS.md §10). Deliberately not
// os.tmpdir() / OS /tmp.
const OUT_DIR = join(PROJECT_ROOT, 'temp', 'ing-026-vite-build')

// Vite names the stylesheet assets/index-<hash>.css; locate it wherever it is.
async function findCss(dir) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) {
      const found = await findCss(full)
      if (found) return found
    } else if (entry.name.endsWith('.css')) {
      return full
    }
  }
  return null
}

describe('ING-026 — the Tailwind v4 build produces v4 CSS', () => {
  let placeholderBefore
  let stylesheetPath

  beforeAll(async () => {
    placeholderBefore = await readFile(TRACKED_PLACEHOLDER, 'utf8')
    await rm(OUT_DIR, { recursive: true, force: true })
    await mkdir(OUT_DIR, { recursive: true })

    // Real project config so @tailwindcss/vite actually runs; output is
    // redirected to the scratch outDir, so frontend/dist is never written.
    await build({
      root: FRONTEND_ROOT,
      configFile: join(FRONTEND_ROOT, 'vite.config.js'),
      logLevel: 'error',
      build: { outDir: OUT_DIR, emptyOutDir: true },
    })

    stylesheetPath = await findCss(OUT_DIR)
  }, 120_000)

  afterAll(async () => {
    // Runs even when an assertion fails, so scratch output never lingers.
    await rm(OUT_DIR, { recursive: true, force: true })
  })

  it('AC1: the real build emits a non-empty stylesheet with Tailwind v4 output', async () => {
    expect(stylesheetPath).toBeTruthy()
    expect(stylesheetPath.startsWith(OUT_DIR)).toBe(true)

    const css = await readFile(stylesheetPath, 'utf8')
    expect(css.trim().length).toBeGreaterThan(0)
    // v4 emits its layers as at-rules and its theme as :root custom properties.
    // An empty or unprocessed stylesheet fails both.
    expect(css).toMatch(/@layer\s+(theme|base|utilities)/)
    expect(css).toMatch(/--[a-zA-Z0-9-]+\s*:/)
    // A utility the app actually uses (App.jsx `grid`). It exists only after
    // the plugin scans source content — the raw `tailwindcss` package contains
    // no `.grid` rule — so an unprocessed stylesheet fails this too.
    expect(css).toMatch(/\.grid\s*\{/)
  })

  it('AC3: the build does not overwrite the tracked frontend/dist/index.html', async () => {
    expect(await readFile(TRACKED_PLACEHOLDER, 'utf8')).toBe(placeholderBefore)
  })

  it('AC2: vite.config.js registers the @tailwindcss/vite plugin', async () => {
    const config = await readFile(join(FRONTEND_ROOT, 'vite.config.js'), 'utf8')
    expect(config).toContain('@tailwindcss/vite')
    expect(config).toMatch(/plugins:\s*\[[\s\S]*tailwindcss\(\)/)
  })

  it('AC2: src/index.css is the single @import "tailwindcss"; entry point', async () => {
    const css = await readFile(join(FRONTEND_ROOT, 'src', 'index.css'), 'utf8')
    expect(css.trim()).toBe('@import "tailwindcss";')
  })

  it('AC2: no v3-era tailwind/postcss config exists at the frontend root', async () => {
    // Project config files live at the package root; a recursive scan would
    // false-positive on postcss fixtures inside node_modules.
    const entries = await readdir(FRONTEND_ROOT)
    for (const banned of [
      'tailwind.config.js',
      'tailwind.config.cjs',
      'postcss.config.js',
      'postcss.config.cjs',
    ]) {
      expect(entries).not.toContain(banned)
    }
  })
})

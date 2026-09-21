// ING-026 — automated check that the Tailwind v4 build/config still produces v4 CSS.
//
// This test:
// 1. Runs `vite build` into a temp outDir (does not overwrite the tracked
//    placeholder at `frontend/dist/index.html` — AC3).
// 2. Checks the built CSS for Tailwind v4 markers (@layer, custom properties,
//    oklch() colors — AC1).
// 3. Checks the config files: @tailwindcss/vite plugin present, single
//    @import "tailwindcss"; entry, no v3-era config files (AC2).
//
// (07-architecture.md "Frontend"; ING-024/025 vitest setup; ING-020 AC4
// structural assertions restored as an automated check.)
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { execSync } from 'node:child_process'
import { mkdtemp, readdir, readFile, rm } from 'node:fs/promises'
import { join } from 'node:path'
import { tmpdir } from 'node:os'

const FRONTEND_ROOT = join(__dirname, '..')

// Tailwind v4 CSS markers — these must appear in the built CSS to prove the
// @tailwindcss/vite plugin is compiling correctly (not falling back to
// unprocessed styles or a v3-era output).
const V4_MARKERS = [
  '@layer theme',
  '@layer base',
  '@layer utilities',
  '--color-',
  'oklch(',
]

function hasV4Markers(css) {
  return V4_MARKERS.every((m) => css.includes(m))
}

// Recursively find the first .css file under a directory, returning its
// absolute path or null if none is found.
async function findCssFile(dir) {
  const entries = await readdir(dir, { withFileTypes: true })
  for (const entry of entries) {
    const fullPath = join(dir, entry.name)
    if (entry.isFile() && entry.name.endsWith('.css')) {
      return fullPath
    }
    if (entry.isDirectory()) {
      const found = await findCssFile(fullPath)
      if (found) return found
    }
  }
  return null
}

describe('ING-026 — Tailwind v4 build/config verification', () => {
  let tempDir

  beforeAll(async () => {
    // Create a temp directory for the build output so we don't overwrite the
    // tracked placeholder at frontend/dist/index.html (AC3).
    tempDir = await mkdtemp(join(tmpdir(), 'wardrobe-ing026-'))
  })

  afterAll(async () => {
    // Clean up the temp build output.
    if (tempDir) {
      await rm(tempDir, { recursive: true, force: true })
    }
  })

  it('the real Vite build produces v4 CSS markers', async () => {
    // Run the Vite build with a temp outDir so we don't touch the tracked
    // placeholder.
    const outDir = join(tempDir, 'dist')
    const buildCmd = `npx vite build --outDir "${outDir}"`
    execSync(buildCmd, { cwd: FRONTEND_ROOT, stdio: 'pipe' })

    // Find the CSS file in the build output (vite build puts CSS in assets/).
    const cssPath = await findCssFile(outDir)
    expect(cssPath).toBeTruthy()

    const cssContent = await readFile(cssPath, 'utf-8')
    expect(hasV4Markers(cssContent)).toBe(true)
  }, 30000)

  it('vite.config.js uses the @tailwindcss/vite plugin', async () => {
    const configContent = await readFile(join(FRONTEND_ROOT, 'vite.config.js'), 'utf-8')
    expect(configContent).toContain('tailwindcss')
    expect(configContent).toContain('@tailwindcss/vite')
  })

  it('src/index.css is the single @import "tailwindcss"; entry point', async () => {
    const cssContent = await readFile(join(FRONTEND_ROOT, 'src', 'index.css'), 'utf-8')
    const trimmed = cssContent.trim()
    expect(trimmed).toBe('@import "tailwindcss";')
  })

  it('no v3-era tailwind.config.js or postcss.config.js exists under frontend/', async () => {
    const files = await readdir(FRONTEND_ROOT)
    expect(files).not.toContain('tailwind.config.js')
    expect(files).not.toContain('tailwind.config.cjs')
    expect(files).not.toContain('postcss.config.js')
    expect(files).not.toContain('postcss.config.cjs')
  })
})

// UI/UX refresh — rendered-DOM tests for the interaction behavior the refresh
// added on top of the ticketed flows: add/edit as dialogs (Escape, focus
// return, no re-open after close), drag-and-drop photo pick, Enter-to-suggest,
// retry on a failed weather/catalog load, and the save confirmation. fetch is
// stubbed per "METHOD /url" with vi.stubGlobal (06-decisions.md "Testing
// tooling"), the same pattern as the ticket test files.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import App from './App.jsx'
import RecommendationPanel from './RecommendationPanel.jsx'
import WeatherPanel from './WeatherPanel.jsx'

const taxonomy = {
  categories: { top: ['shirt', 'hoodie'], bottom: ['jeans', 'shorts'] },
  colors: ['black', 'blue', 'olive', 'white'],
  patterns: ['solid', 'striped', 'plaid', 'print'],
  warmth_tiers: ['light', 'medium', 'heavy'],
  formality: ['casual', 'smart-casual', 'formal'],
}

const item = {
  id: 'item-1',
  photo_url: '/api/photos/item-1.jpg',
  category: 'top',
  subcategory: 'shirt',
  dominant_color: 'olive',
  secondary_colors: [],
  pattern: 'solid',
  warmth_tier: 'light',
  formality: 'casual',
  added_date: '2026-01-02',
  notes: 'linen',
}

const weather = {
  location: { name: 'Berlin', country: 'Germany' },
  current: { temperature_c: 18, apparent_temperature_c: 17, weather_code: 0 },
  today: { temperature_min_c: 9, temperature_max_c: 20, precipitation_probability_max: 10 },
}

const jsonResponse = (body, { ok = true, status = 200 } = {}) => ({
  ok,
  status,
  json: () => Promise.resolve(body),
})

// A route value is a response, or a function returning one (or a promise of
// one). An unregistered request rejects, so a stray call fails loudly.
function stubRoutes(routes) {
  const fetchMock = vi.fn((url, options = {}) => {
    const route = routes[`${options.method ?? 'GET'} ${url}`]
    if (!route) return Promise.reject(new Error(`unexpected request: ${url}`))
    return Promise.resolve(typeof route === 'function' ? route() : route)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const writes = (fetchMock) =>
  fetchMock.mock.calls.filter(([, options]) => (options?.method ?? 'GET') !== 'GET')

const originalCreateObjectURL = URL.createObjectURL
const originalRevokeObjectURL = URL.revokeObjectURL

beforeEach(() => {
  URL.createObjectURL = vi.fn(() => 'blob:preview')
  URL.revokeObjectURL = vi.fn()
})

afterEach(() => {
  URL.createObjectURL = originalCreateObjectURL
  URL.revokeObjectURL = originalRevokeObjectURL
  cleanup()
  vi.unstubAllGlobals()
})

describe('add and edit dialogs', () => {
  it('opens "Add garment" as a dialog that Escape closes, handing focus back to the button', async () => {
    stubRoutes({
      'GET /api/items': jsonResponse([]),
      'GET /api/weather': jsonResponse(weather),
      'GET /api/taxonomy': jsonResponse(taxonomy),
    })
    render(<App />)

    const addButton = await screen.findByRole('button', { name: 'Add garment' })
    addButton.focus()
    fireEvent.click(addButton)

    const dialog = await screen.findByRole('dialog', { name: 'Add a garment' })
    expect(within(dialog).getByRole('form', { name: 'Upload garment photo' })).toBeInTheDocument()

    fireEvent.keyDown(document.activeElement, { key: 'Escape' })

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    await waitFor(() => expect(addButton).toHaveFocus())
  })

  it('closes the edit dialog on Escape without writing anything', async () => {
    const fetchMock = stubRoutes({
      'GET /api/items': jsonResponse([item]),
      'GET /api/items/item-1': jsonResponse(item),
      'GET /api/weather': jsonResponse(weather),
      'GET /api/taxonomy': jsonResponse(taxonomy),
    })
    render(<App />)

    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog', { name: 'Edit garment' })
    await within(dialog).findByRole('form', { name: 'Edit item' })

    fireEvent.keyDown(document, { key: 'Escape' })

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(writes(fetchMock)).toHaveLength(0)
  })

  it('does not pop the edit dialog back open when it was closed while the item was loading', async () => {
    let resolveItem
    stubRoutes({
      'GET /api/items': jsonResponse([item]),
      'GET /api/items/item-1': () => new Promise((resolve) => (resolveItem = resolve)),
      'GET /api/weather': jsonResponse(weather),
      'GET /api/taxonomy': jsonResponse(taxonomy),
    })
    render(<App />)

    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog', { name: 'Edit garment' })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    await act(async () => {
      resolveItem(jsonResponse(item))
    })

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.queryByRole('form', { name: 'Edit item' })).not.toBeInTheDocument()
  })

  it('cannot be dismissed while a save is in flight, then confirms the save', async () => {
    let resolvePut
    stubRoutes({
      'GET /api/items': jsonResponse([item]),
      'GET /api/items/item-1': jsonResponse(item),
      'GET /api/weather': jsonResponse(weather),
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'PUT /api/items/item-1': () => new Promise((resolve) => (resolvePut = resolve)),
    })
    render(<App />)

    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
    const form = await screen.findByRole('form', { name: 'Edit item' })
    fireEvent.click(within(form).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(within(form).getByRole('button', { name: 'Cancel' })).toBeDisabled())

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.getByRole('dialog', { name: 'Edit garment' })).toBeInTheDocument()

    await act(async () => {
      resolvePut(jsonResponse({ ...item, notes: 'washed' }))
    })

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(screen.getByText('Changes saved')).toBeInTheDocument()
    expect(screen.getByText('washed')).toBeInTheDocument()
  })
})

describe('drag-and-drop photo pick', () => {
  const openAdd = async () => {
    stubRoutes({
      'GET /api/items': jsonResponse([]),
      'GET /api/weather': jsonResponse(weather),
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'POST /api/items/photo': jsonResponse({ item_id: 'item-9', photo_ref: 'item-9.png' }),
    })
    render(<App />)
    fireEvent.click(await screen.findByRole('button', { name: 'Add garment' }))
    const form = await screen.findByRole('form', { name: 'Upload garment photo' })
    const dropzone = within(form)
      .getByText('Drop a photo here, or click to choose')
      .closest('label')
    return { form, dropzone }
  }

  it('uses a dropped photo for the upload', async () => {
    const { form, dropzone } = await openAdd()
    const file = new File(['bytes'], 'Hoodie.PNG', { type: 'image/png' })

    fireEvent.drop(dropzone, { dataTransfer: { files: [file] } })

    expect(within(form).getByText('Hoodie.PNG')).toBeInTheDocument()
    expect(within(form).getByRole('img', { name: 'Selected photo' })).toBeInTheDocument()
    const upload = within(form).getByRole('button', { name: 'Upload & tag' })
    expect(upload).toBeEnabled()

    fireEvent.click(upload)
    await screen.findByRole('form', { name: 'Confirm garment' })
    const post = fetch.mock.calls.find(([url]) => url === '/api/items/photo')
    expect(post[1].body.get('photo')).toBe(file)
  })

  it('rejects a dropped file outside the accepted types without uploading', async () => {
    const { form, dropzone } = await openAdd()

    fireEvent.drop(dropzone, {
      dataTransfer: { files: [new File(['gif'], 'shirt.gif', { type: 'image/gif' })] },
    })

    expect(within(form).getByRole('alert')).toHaveTextContent('.jpg, .jpeg, .png, or .webp')
    expect(within(form).getByRole('button', { name: 'Upload & tag' })).toBeDisabled()
    expect(fetch.mock.calls.some(([url]) => url === '/api/items/photo')).toBe(false)
  })
})

describe('outfit ideas', () => {
  it('sends the request when Enter is pressed in the note field, once while busy', async () => {
    let resolvePost
    const fetchMock = stubRoutes({
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'POST /api/recommendations': () => new Promise((resolve) => (resolvePost = resolve)),
    })
    render(<RecommendationPanel />)

    const note = screen.getByLabelText('Note')
    fireEvent.change(note, { target: { value: 'wedding' } })
    fireEvent.keyDown(note, { key: 'Enter' })
    fireEvent.keyDown(note, { key: 'Enter' })

    expect(screen.getByRole('status')).toBeInTheDocument()
    const posts = fetchMock.mock.calls.filter(([, options]) => options?.method === 'POST')
    expect(posts).toHaveLength(1)
    expect(posts[0][1].body).toBe('{"note":"wedding"}')

    await act(async () => {
      resolvePost(jsonResponse({ outfits: [] }))
    })
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('caps the note at the 500 characters the server accepts', () => {
    stubRoutes({ 'GET /api/taxonomy': jsonResponse(taxonomy) })
    render(<RecommendationPanel />)

    expect(screen.getByLabelText('Note')).toHaveAttribute('maxLength', '500')
  })
})

describe('retry after a failed load', () => {
  it('reloads the weather from "Try again"', async () => {
    let calls = 0
    stubRoutes({
      'GET /api/weather': () =>
        ++calls === 1
          ? jsonResponse({ error: 'down' }, { ok: false, status: 502 })
          : jsonResponse(weather),
    })
    render(<WeatherPanel />)

    expect(await screen.findByText('Could not load the weather.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByText('Berlin')).toBeInTheDocument()
    expect(screen.queryByText('Could not load the weather.')).not.toBeInTheDocument()
  })

  it('reloads the catalog from "Try again" without touching the weather panel', async () => {
    let calls = 0
    const fetchMock = stubRoutes({
      'GET /api/items': () =>
        ++calls === 1
          ? jsonResponse({ error: 'db' }, { ok: false, status: 500 })
          : jsonResponse([item]),
      'GET /api/weather': jsonResponse(weather),
      'GET /api/taxonomy': jsonResponse(taxonomy),
    })
    render(<App />)

    expect(await screen.findByText('Could not load the catalog.')).toBeInTheDocument()
    await screen.findByText('Berlin')
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByRole('listitem')).toBeInTheDocument()
    expect(screen.getByText('linen')).toBeInTheDocument()
    expect(fetchMock.mock.calls.filter(([url]) => url === '/api/weather')).toHaveLength(1)
  })
})

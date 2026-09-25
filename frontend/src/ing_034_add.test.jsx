// ING-034 — add a garment (upload → local draft → correct → save),
// rendered-DOM tests.
//
// The grid page and the add flow are rendered in jsdom and driven through the
// DOM (no source-string grepping); fetch is stubbed with Vitest's
// vi.stubGlobal and routed per method+URL (06-decisions.md "Testing tooling").
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import App from './App.jsx'

// The GET /api/taxonomy shape (internal/tagging.Taxonomy): the closed enums
// the server validates against, so the form's choices cannot drift.
const taxonomy = {
  categories: {
    top: ['shirt', 'hoodie'],
    bottom: ['jeans', 'shorts'],
  },
  colors: ['black', 'blue', 'olive', 'white'],
  patterns: ['solid', 'striped', 'plaid', 'print'],
  warmth_tiers: ['light', 'medium', 'heavy'],
  formality: ['casual', 'smart-casual', 'formal'],
}

// The POST /api/items/photo success body (internal/api.draftResponse): the
// generated item id, the staged-photo basename, and the seven tagging fields.
const draft = {
  item_id: 'item-9',
  photo_ref: 'item-9.jpg',
  category: 'top',
  subcategory: 'shirt',
  dominant_color: 'olive',
  secondary_colors: ['white'],
  pattern: 'solid',
  warmth_tier: 'light',
  formality: 'casual',
}

const makeItem = (overrides = {}) => ({
  id: 'item-1',
  photo_url: '/api/photos/item-1.jpg',
  category: 'top',
  subcategory: 'shirt',
  dominant_color: 'olive',
  secondary_colors: ['white'],
  pattern: 'solid',
  warmth_tier: 'light',
  formality: 'casual',
  added_date: '2026-01-02',
  notes: 'linen',
  ...overrides,
})

const jsonResponse = (body, { ok = true, status = 200 } = {}) => ({
  ok,
  status,
  json: () => Promise.resolve(body),
})

// Routes requests by "METHOD /url". An unregistered request rejects, so a
// stray call (e.g. an early write) fails the test loudly.
function stubRoutes(routes) {
  const fetchMock = vi.fn((url, options = {}) => {
    const key = `${options.method ?? 'GET'} ${url}`
    const route = routes[key]
    if (!route) return Promise.reject(new Error(`unexpected request: ${key}`))
    return Promise.resolve(route)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function deferred() {
  let resolve
  const promise = new Promise((r) => {
    resolve = r
  })
  return { promise, resolve }
}

const makeFile = (name = 'shirt.jpg') =>
  new File(['photo-bytes'], name, { type: 'image/jpeg' })

async function openAdd() {
  fireEvent.click(await screen.findByRole('button', { name: 'Add garment' }))
  return screen.findByRole('form', { name: 'Upload garment photo' })
}

async function upload(file, form) {
  fireEvent.change(within(form).getByLabelText('Photo'), { target: { files: [file] } })
  fireEvent.click(within(form).getByRole('button', { name: 'Upload & tag' }))
}

// Object URLs are not implemented in jsdom; the component uses one to preview
// the chosen file, so stub them for these tests.
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

describe('ING-034 — add a garment', () => {
  it('AC1: the add control opens a form whose file input accepts jpg/jpeg/png/webp', async () => {
    const fetchMock = stubRoutes({
      'GET /api/items': jsonResponse([]),
      'GET /api/taxonomy': jsonResponse(taxonomy),
    })

    render(<App />)
    const form = await openAdd()

    const input = within(form).getByLabelText('Photo')
    expect(input).toHaveAttribute('type', 'file')
    const accept = input.getAttribute('accept')
    for (const ext of ['.jpg', '.jpeg', '.png', '.webp']) {
      expect(accept).toContain(ext)
    }

    // The enum vocabulary is fetched from the server route, not bundled.
    expect(fetchMock).toHaveBeenCalledWith('/api/taxonomy')
  })

  it('AC2: submitting POSTs multipart "photo" to /api/items/photo and shows a loading state', async () => {
    const pending = deferred()
    const fetchMock = stubRoutes({
      'GET /api/items': jsonResponse([]),
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'POST /api/items/photo': pending.promise,
    })

    render(<App />)
    const form = await openAdd()
    const file = makeFile()
    await upload(file, form)

    // Loading state while the local VLM tags.
    expect(await screen.findByRole('status')).toHaveTextContent(/tagging/i)

    const call = fetchMock.mock.calls.find(([url]) => url === '/api/items/photo')
    expect(call[1].method).toBe('POST')
    expect(call[1].body).toBeInstanceOf(FormData)
    expect(call[1].body.get('photo')).toBe(file)

    await act(async () => {
      pending.resolve(jsonResponse(draft))
    })
    await screen.findByRole('form', { name: 'Confirm garment' })
  })

  it('AC3: a successful draft pre-fills the seven fields + notes and shows a preview, with nothing persisted', async () => {
    const fetchMock = stubRoutes({
      'GET /api/items': jsonResponse([]),
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'POST /api/items/photo': jsonResponse(draft),
    })

    render(<App />)
    const form = await openAdd()
    await upload(makeFile(), form)

    const confirm = await screen.findByRole('form', { name: 'Confirm garment' })
    expect(within(confirm).getByLabelText('Category')).toHaveValue('top')
    expect(within(confirm).getByLabelText('Subcategory')).toHaveValue('shirt')
    expect(within(confirm).getByLabelText('Dominant color')).toHaveValue('olive')
    expect(within(confirm).getByLabelText('Pattern')).toHaveValue('solid')
    expect(within(confirm).getByLabelText('Warmth')).toHaveValue('light')
    expect(within(confirm).getByLabelText('Formality')).toHaveValue('casual')
    expect(within(confirm).getByLabelText('white')).toBeChecked()
    expect(within(confirm).getByLabelText('Notes')).toHaveValue('')

    // Preview of the uploaded photo.
    expect(within(confirm).getByRole('img', { name: 'Uploaded photo' })).toHaveAttribute(
      'src',
      'blob:preview',
    )

    // Nothing persisted: no POST /api/items yet.
    const itemPosts = fetchMock.mock.calls.filter(
      ([url, options]) => url === '/api/items' && options?.method === 'POST',
    )
    expect(itemPosts).toHaveLength(0)
  })

  it('AC4: the draft form offers the server taxonomy choices, not a bundled copy', async () => {
    stubRoutes({
      'GET /api/items': jsonResponse([]),
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'POST /api/items/photo': jsonResponse(draft),
    })

    render(<App />)
    const form = await openAdd()
    await upload(makeFile(), form)
    const confirm = await screen.findByRole('form', { name: 'Confirm garment' })

    const categorySelect = within(confirm).getByLabelText('Category')
    expect(within(categorySelect).getByRole('option', { name: 'bottom' })).toBeInTheDocument()
    expect(within(confirm).queryByRole('option', { name: 'dress-pants' })).not.toBeInTheDocument()

    // Subcategory choices follow the category, also from the server enums.
    const subcategorySelect = within(confirm).getByLabelText('Subcategory')
    expect(
      within(subcategorySelect).getByRole('option', { name: 'shirt' }),
    ).toBeInTheDocument()
  })

  it('AC5: Save POSTs /api/items with the item id + corrected fields and the grid shows the new item', async () => {
    const created = makeItem({
      id: 'item-9',
      photo_url: '/api/photos/item-9.jpg',
      category: 'bottom',
      subcategory: 'jeans',
      pattern: 'striped',
      notes: 'gift',
    })
    const fetchMock = stubRoutes({
      'GET /api/items': jsonResponse([]),
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'POST /api/items/photo': jsonResponse(draft),
      'POST /api/items': jsonResponse(created, { status: 201 }),
    })

    render(<App />)
    const form = await openAdd()
    await upload(makeFile(), form)
    const confirm = await screen.findByRole('form', { name: 'Confirm garment' })

    fireEvent.change(within(confirm).getByLabelText('Category'), { target: { value: 'bottom' } })
    fireEvent.change(within(confirm).getByLabelText('Subcategory'), { target: { value: 'jeans' } })
    fireEvent.change(within(confirm).getByLabelText('Pattern'), { target: { value: 'striped' } })
    fireEvent.change(within(confirm).getByLabelText('Notes'), { target: { value: 'gift' } })
    fireEvent.click(within(confirm).getByRole('button', { name: 'Save' }))

    // The POST carries the draft identity plus the corrected mutable fields.
    await waitFor(() =>
      expect(screen.queryByRole('form', { name: 'Confirm garment' })).not.toBeInTheDocument(),
    )
    const post = fetchMock.mock.calls.find(
      ([url, options]) => url === '/api/items' && options?.method === 'POST',
    )
    const body = JSON.parse(post[1].body)
    expect(body).toMatchObject({
      item_id: 'item-9',
      photo_ref: 'item-9.jpg',
      category: 'bottom',
      subcategory: 'jeans',
      pattern: 'striped',
      notes: 'gift',
    })
    expect(body).not.toHaveProperty('id')
    expect(body).not.toHaveProperty('added_date')
    expect(body).not.toHaveProperty('photo_path')

    // The grid now shows the created item.
    const card = await screen.findByRole('listitem')
    for (const text of ['bottom', 'jeans', 'striped', 'gift']) {
      expect(within(card).getByText(text)).toBeInTheDocument()
    }
  })

  it('AC6: a tagging failure after retry is surfaced and lets the user retry or pick another photo, persisting nothing', async () => {
    const fetchMock = stubRoutes({
      'GET /api/items': jsonResponse([]),
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'POST /api/items/photo': jsonResponse(
        { error: 'tagging failed validation after retry; retry or choose another photo' },
        { ok: false, status: 422 },
      ),
    })

    render(<App />)
    const form = await openAdd()
    await upload(makeFile(), form)

    expect(await screen.findByRole('alert')).toHaveTextContent(/retry or choose another photo/i)

    // Still on the upload form: the file can be resubmitted (retry) or changed.
    const retryForm = screen.getByRole('form', { name: 'Upload garment photo' })
    expect(within(retryForm).getByRole('button', { name: 'Upload & tag' })).toBeEnabled()
    expect(within(retryForm).getByLabelText('Photo')).toBeInTheDocument()

    expect(
      fetchMock.mock.calls.filter(
        ([url, options]) => url === '/api/items' && options?.method === 'POST',
      ),
    ).toHaveLength(0)
  })

  it('AC7: a save 400 naming a field is surfaced and the draft stays open', async () => {
    stubRoutes({
      'GET /api/items': jsonResponse([]),
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'POST /api/items/photo': jsonResponse(draft),
      'POST /api/items': jsonResponse(
        { error: 'invalid tagging result: invalid subcategory "hoodie" for category "bottom"' },
        { ok: false, status: 400 },
      ),
    })

    render(<App />)
    const form = await openAdd()
    await upload(makeFile(), form)
    const confirm = await screen.findByRole('form', { name: 'Confirm garment' })

    fireEvent.change(within(confirm).getByLabelText('Category'), { target: { value: 'bottom' } })
    fireEvent.change(within(confirm).getByLabelText('Subcategory'), { target: { value: 'shorts' } })
    fireEvent.click(within(confirm).getByRole('button', { name: 'Save' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/invalid subcategory "hoodie"/)
    // Not swallowed: the draft is still open with the user's input.
    expect(screen.getByRole('form', { name: 'Confirm garment' })).toBeInTheDocument()
    expect(within(confirm).getByLabelText('Category')).toHaveValue('bottom')
    // No false success in the grid.
    expect(screen.queryAllByRole('listitem')).toHaveLength(0)
  })

  // Edge (04-data-schema.md: secondary_colors is 0-or-more). A draft with none
  // must render every box unchecked and submit an empty list, not undefined.
  it('edge: a draft with no secondary colors submits an empty list', async () => {
    const created = makeItem({ id: 'item-9', photo_url: '/api/photos/item-9.jpg' })
    const fetchMock = stubRoutes({
      'GET /api/items': jsonResponse([]),
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'POST /api/items/photo': jsonResponse({ ...draft, secondary_colors: [] }),
      'POST /api/items': jsonResponse(created, { status: 201 }),
    })

    render(<App />)
    const form = await openAdd()
    await upload(makeFile(), form)
    const confirm = await screen.findByRole('form', { name: 'Confirm garment' })

    expect(within(confirm).getByLabelText('white')).not.toBeChecked()

    fireEvent.click(within(confirm).getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(screen.queryByRole('form', { name: 'Confirm garment' })).not.toBeInTheDocument(),
    )

    const post = fetchMock.mock.calls.find(
      ([url, options]) => url === '/api/items' && options?.method === 'POST',
    )
    expect(JSON.parse(post[1].body).secondary_colors).toEqual([])
  })

  // Edge (05-vlm-tagging-spec.md "Interactive tagging (web UI)"): a tagging
  // call that cannot reach the server is surfaced and the user can retry or
  // pick another photo, with nothing persisted.
  it('edge: a network failure on upload is surfaced with nothing persisted', async () => {
    const fetchMock = stubRoutes({
      'GET /api/items': jsonResponse([]),
      'GET /api/taxonomy': jsonResponse(taxonomy),
      // No POST /api/items/photo route registered: the upload call rejects.
    })

    render(<App />)
    const form = await openAdd()
    await upload(makeFile(), form)

    expect(await screen.findByRole('alert')).toHaveTextContent(/could not reach the server/i)
    expect(screen.getByRole('form', { name: 'Upload garment photo' })).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.filter(
        ([url, options]) => url === '/api/items' && options?.method === 'POST',
      ),
    ).toHaveLength(0)
  })
})

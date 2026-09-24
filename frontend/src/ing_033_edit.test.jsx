// ING-033 — edit an existing catalog item, rendered-DOM tests.
//
// The grid page and the edit form are rendered in jsdom and driven through
// the DOM (no source-string grepping); fetch is stubbed with Vitest's
// vi.stubGlobal and routed per method+URL (06-decisions.md "Testing tooling").
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
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
// stray call (e.g. a write on load) fails the test loudly.
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

function gridRoutes(item, extra = {}) {
  return {
    'GET /api/items': jsonResponse([item]),
    [`GET /api/items/${item.id}`]: jsonResponse(item),
    'GET /api/taxonomy': jsonResponse(taxonomy),
    ...extra,
  }
}

async function openEdit() {
  fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
  return screen.findByRole('form', { name: 'Edit item' })
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('ING-033 — edit an existing catalog item', () => {
  it('AC1/AC2: activating edit GETs /api/items/:id and opens a pre-filled form whose choices come from GET /api/taxonomy', async () => {
    const item = makeItem()
    const fetchMock = stubRoutes(gridRoutes(item))

    render(<App />)
    await screen.findAllByRole('listitem')

    const form = await openEdit()

    // Edit activation re-reads the one item and loads the enum vocabulary.
    expect(fetchMock).toHaveBeenCalledWith('/api/items/item-1')
    expect(fetchMock).toHaveBeenCalledWith('/api/taxonomy')

    // Every mutable field is prefilled with the item's current value.
    expect(within(form).getByLabelText('Category')).toHaveValue('top')
    expect(within(form).getByLabelText('Subcategory')).toHaveValue('shirt')
    expect(within(form).getByLabelText('Dominant color')).toHaveValue('olive')
    expect(within(form).getByLabelText('Pattern')).toHaveValue('solid')
    expect(within(form).getByLabelText('Warmth')).toHaveValue('light')
    expect(within(form).getByLabelText('Formality')).toHaveValue('casual')
    expect(within(form).getByLabelText('Notes')).toHaveValue('linen')
    expect(within(form).getByLabelText('white')).toBeChecked()

    // id, added_date, and the photo are shown read-only.
    expect(within(form).getByText('item-1')).toBeInTheDocument()
    expect(within(form).getByText('2026-01-02')).toBeInTheDocument()
    expect(within(form).getByRole('img', { name: 'Current photo' })).toHaveAttribute(
      'src',
      item.photo_url,
    )
    // ...and there is no editable control for any of them.
    expect(within(form).queryByLabelText('ID')).not.toBeInTheDocument()
    expect(within(form).queryByLabelText('Added')).not.toBeInTheDocument()

    // Choices are the server enums, not a bundled copy: an option from the
    // response is offered; one that is not in it is absent.
    const categorySelect = within(form).getByLabelText('Category')
    expect(within(categorySelect).getByRole('option', { name: 'bottom' })).toBeInTheDocument()
    expect(within(form).queryByRole('option', { name: 'dress-pants' })).not.toBeInTheDocument()
  })

  it('AC3/AC5: submitting PUTs only the mutable fields and, on 2xx, the grid shows the updated values', async () => {
    const item = makeItem()
    const updated = {
      ...item,
      category: 'bottom',
      subcategory: 'jeans',
      pattern: 'striped',
      notes: 'hemmed',
    }
    const fetchMock = stubRoutes(
      gridRoutes(item, { [`PUT /api/items/${item.id}`]: jsonResponse(updated) }),
    )

    render(<App />)
    await screen.findAllByRole('listitem')
    const form = await openEdit()

    fireEvent.change(within(form).getByLabelText('Category'), { target: { value: 'bottom' } })
    fireEvent.change(within(form).getByLabelText('Subcategory'), { target: { value: 'jeans' } })
    fireEvent.change(within(form).getByLabelText('Pattern'), { target: { value: 'striped' } })
    fireEvent.change(within(form).getByLabelText('Notes'), { target: { value: 'hemmed' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(screen.queryByRole('form', { name: 'Edit item' })).not.toBeInTheDocument(),
    )

    // The PUT carried exactly the mutable fields — never id/added_date/photo_path.
    const putCall = fetchMock.mock.calls.find(([, options]) => options?.method === 'PUT')
    expect(putCall[0]).toBe('/api/items/item-1')
    const body = JSON.parse(putCall[1].body)
    expect(Object.keys(body).sort()).toEqual([
      'category',
      'dominant_color',
      'formality',
      'notes',
      'pattern',
      'secondary_colors',
      'subcategory',
      'warmth_tier',
    ])
    expect(body).not.toHaveProperty('id')
    expect(body).not.toHaveProperty('added_date')
    expect(body).not.toHaveProperty('photo_path')
    expect(body).toMatchObject({
      category: 'bottom',
      subcategory: 'jeans',
      pattern: 'striped',
      notes: 'hemmed',
    })

    // The grid now shows the values the server returned.
    const card = await screen.findByRole('listitem')
    expect(within(card).getByText('bottom')).toBeInTheDocument()
    expect(within(card).getByText('jeans')).toBeInTheDocument()
    expect(within(card).getByText('striped')).toBeInTheDocument()
    expect(within(card).getByText('hemmed')).toBeInTheDocument()
  })

  it('AC4: a server 400 naming a field is surfaced to the user and the form stays open', async () => {
    const item = makeItem()
    stubRoutes(
      gridRoutes(item, {
        [`PUT /api/items/${item.id}`]: jsonResponse(
          { error: 'invalid tagging result: invalid subcategory "hoodie" for category "bottom"' },
          { ok: false, status: 400 },
        ),
      }),
    )

    render(<App />)
    await screen.findAllByRole('listitem')
    const form = await openEdit()

    fireEvent.change(within(form).getByLabelText('Category'), { target: { value: 'bottom' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Save' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/invalid subcategory/)
    // Not swallowed: the form is still open with the user's input.
    expect(screen.getByRole('form', { name: 'Edit item' })).toBeInTheDocument()
    expect(within(form).getByLabelText('Category')).toHaveValue('bottom')
    // No false success in the grid (the card still shows the original values).
    expect(
      within(screen.getByRole('listitem')).getByRole('heading', { name: 'top · shirt' }),
    ).toBeInTheDocument()
  })

  it('cancelling the form closes it without any write request', async () => {
    const item = makeItem()
    const fetchMock = stubRoutes(gridRoutes(item))

    render(<App />)
    await screen.findAllByRole('listitem')
    const form = await openEdit()

    fireEvent.click(within(form).getByRole('button', { name: 'Cancel' }))

    await waitFor(() =>
      expect(screen.queryByRole('form', { name: 'Edit item' })).not.toBeInTheDocument(),
    )
    expect(fetchMock).not.toHaveBeenCalledWith(
      expect.stringContaining('/api/items/item-1'),
      expect.objectContaining({ method: 'PUT' }),
    )
  })

  // Edge (04-data-schema.md "Write-path rules" + ticket context): secondary
  // colors are 0-or-more. An item with none must render all boxes unchecked
  // and submit an empty list, not a stale/undefined value.
  it('edge: an item with no secondary colors submits an empty list', async () => {
    const item = makeItem({ secondary_colors: [] })
    const updated = { ...item, notes: 'plain' }
    const fetchMock = stubRoutes(
      gridRoutes(item, { [`PUT /api/items/${item.id}`]: jsonResponse(updated) }),
    )

    render(<App />)
    await screen.findAllByRole('listitem')
    const form = await openEdit()

    expect(within(form).getByLabelText('white')).not.toBeChecked()
    expect(within(form).getByLabelText('black')).not.toBeChecked()

    fireEvent.click(within(form).getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(screen.queryByRole('form', { name: 'Edit item' })).not.toBeInTheDocument(),
    )

    const putCall = fetchMock.mock.calls.find(([, options]) => options?.method === 'PUT')
    expect(JSON.parse(putCall[1].body).secondary_colors).toEqual([])
  })

  // AC5 (07-architecture.md "Catalog write API" / 06-decisions.md "Interactive
  // catalog create/edit UI"): an edit is a PUT of mutable fields only — no
  // re-tagging, no photo replacement, no create.
  it('AC5: an edit submit makes no re-tag/photo-upload or create request, only the PUT', async () => {
    const item = makeItem()
    const updated = { ...item, notes: 'washed' }
    const fetchMock = stubRoutes(
      gridRoutes(item, { [`PUT /api/items/${item.id}`]: jsonResponse(updated) }),
    )

    render(<App />)
    await screen.findAllByRole('listitem')
    const form = await openEdit()

    fireEvent.click(within(form).getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(screen.queryByRole('form', { name: 'Edit item' })).not.toBeInTheDocument(),
    )

    // The only non-GET request is the single PUT; no POST means the VLM/upload
    // route (/api/items/photo) and the create route were never hit.
    const writeMethods = fetchMock.mock.calls
      .map(([, options]) => (options?.method ?? 'GET').toUpperCase())
      .filter((method) => method !== 'GET')
    expect(writeMethods).toEqual(['PUT'])

    const urls = fetchMock.mock.calls.map(([url]) => url)
    expect(urls).not.toContain('/api/items/photo')
    // Only GET /api/items (grid load) and GET /api/items/:id (edit load) touch
    // the items collection besides the PUT.
    expect(urls.filter((url) => url === '/api/items')).toHaveLength(1)
  })
})

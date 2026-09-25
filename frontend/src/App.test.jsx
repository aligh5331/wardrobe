// ING-024 — rendered-DOM component tests for the ING-020 grid page.
//
// These assert what the user sees (cards, fields, empty state, missing-photo
// placeholder) by rendering <App /> in jsdom, rather than grepping source
// strings. fetch is stubbed with Vitest's built-in vi.stubGlobal; no mock
// server dependency (06-decisions.md "Testing tooling").
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from '@testing-library/react'
import App from './App.jsx'

const makeItem = (overrides = {}) => ({
  id: 'item-1',
  photo_url: '/api/photos/item-1.jpg',
  category: 'Tops',
  subcategory: 'Shirt',
  dominant_color: 'olive',
  secondary_colors: ['white'],
  pattern: 'solid',
  warmth_tier: 'light',
  formality: 'casual',
  added_date: '2026-01-02',
  notes: 'linen',
  ...overrides,
})

function stubFetch(payload, { ok = true, status = 200 } = {}) {
  const fetchMock = vi.fn().mockResolvedValue({
    ok,
    status,
    json: () => Promise.resolve(payload),
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('App grid page (ING-020 behavior, rendered DOM)', () => {
  it('fetches GET /api/items and renders one card per item with its photo and tagged fields', async () => {
    const first = makeItem()
    const second = makeItem({
      id: 'item-2',
      photo_url: '/api/photos/item-2.jpg',
      category: 'Bottoms',
      subcategory: 'Jeans',
      dominant_color: 'blue',
      secondary_colors: [],
      pattern: 'denim',
      warmth_tier: 'medium',
      added_date: '2026-02-03',
      notes: '',
    })
    const fetchMock = stubFetch([first, second])

    render(<App />)

    expect(fetchMock).toHaveBeenCalledWith('/api/items')

    const cards = await screen.findAllByRole('listitem')
    expect(cards).toHaveLength(2)

    // Card 1 — photo_url and every tagged field actually rendered.
    const firstCard = within(cards[0])
    expect(firstCard.getByRole('img')).toHaveAttribute('src', first.photo_url)
    for (const text of [
      'Tops',
      'Shirt',
      'olive',
      'white', // secondary_colors joined
      'solid',
      'light',
      'casual',
      '2026-01-02',
      'linen',
    ]) {
      expect(firstCard.getByText(text)).toBeInTheDocument()
    }

    // Card 2 — still one card per item; empty secondary_colors/notes omitted.
    const secondCard = within(cards[1])
    expect(secondCard.getByRole('img')).toHaveAttribute('src', second.photo_url)
    for (const text of ['Bottoms', 'Jeans', 'blue', 'denim', 'medium', 'casual', '2026-02-03']) {
      expect(secondCard.getByText(text)).toBeInTheDocument()
    }

    expect(screen.queryByText('Could not load the catalog.')).not.toBeInTheDocument()
  })

  it('renders the empty-catalog state when the API returns []', async () => {
    stubFetch([])

    render(<App />)

    expect(
      await screen.findByText('No items cataloged yet. Run the ingestion pipeline to add garments.'),
    ).toBeInTheDocument()
    expect(screen.queryAllByRole('listitem')).toHaveLength(0)
    expect(screen.queryByText('Could not load the catalog.')).not.toBeInTheDocument()
  })

  it('shows the missing-photo placeholder but keeps the fields when a photo fails to load', async () => {
    stubFetch([makeItem()])

    render(<App />)

    const photo = await screen.findByRole('img', { name: 'olive Shirt' })
    fireEvent.error(photo)

    expect(await screen.findByRole('img', { name: 'Missing photo' })).toBeInTheDocument()
    expect(screen.queryByRole('img', { name: 'olive Shirt' })).not.toBeInTheDocument()
    // The card is still there with its fields; a broken photo cannot break the page.
    expect(screen.getByText('Tops')).toBeInTheDocument()
    expect(screen.getByText('Shirt')).toBeInTheDocument()
    expect(screen.getByText('linen')).toBeInTheDocument()
  })

  it('shows the missing-photo placeholder but keeps the fields when photo_url is absent', async () => {
    stubFetch([makeItem({ photo_url: '' })])

    render(<App />)

    expect(await screen.findByRole('img', { name: 'Missing photo' })).toBeInTheDocument()
    expect(screen.getByText('Tops')).toBeInTheDocument()
    expect(screen.getByText('Shirt')).toBeInTheDocument()
    expect(screen.getByText('linen')).toBeInTheDocument()
  })

  // ING-025 — the assertions below were ported from the retired structural
  // suite so removing it loses no coverage. They assert the same branches
  // through the rendered DOM.
  it.each([
    ['a non-OK HTTP response', () => stubFetch([], { ok: false, status: 500 })],
    [
      'a network failure',
      () => vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline'))),
    ],
  ])('shows the error state on %s', async (_label, arrange) => {
    arrange()

    render(<App />)

    expect(await screen.findByText('Could not load the catalog.')).toBeInTheDocument()
    expect(screen.queryAllByRole('listitem')).toHaveLength(0)
    expect(
      screen.queryByText('No items cataloged yet. Run the ingestion pipeline to add garments.'),
    ).not.toBeInTheDocument()
  })

  it('falls back to the empty state when a successful response body is not an array', async () => {
    stubFetch({ not: 'an array' })

    render(<App />)

    expect(
      await screen.findByText('No items cataloged yet. Run the ingestion pipeline to add garments.'),
    ).toBeInTheDocument()
    expect(screen.queryAllByRole('listitem')).toHaveLength(0)
    expect(screen.queryByText('Could not load the catalog.')).not.toBeInTheDocument()
  })

  // ING-033 added the edit entry point, so this is no longer "no write
  // controls at all": loading the grid still only GETs /api/items and renders
  // no write form; the edit form appears only after the user activates edit
  // (that flow is covered by ing_033_edit.test.jsx).
  it('loads read-only: only GETs /api/items and renders no write form until edit is activated', async () => {
    const fetchMock = stubFetch([makeItem()])

    const { container } = render(<App />)

    await screen.findAllByRole('listitem')

    // Exactly one call, with the bare URL only — no RequestInit means no
    // method/POST/PUT/DELETE/PATCH write request.
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledWith('/api/items')
    // The edit entry point exists, but no write form is rendered yet.
    expect(screen.getByRole('button', { name: 'Edit' })).toBeInTheDocument()
    expect(container.querySelectorAll('form, input, select, textarea')).toHaveLength(0)
  })
})

// ING-050 - "Suggest outfits" panel, rendered-DOM tests.
//
// One test per acceptance criterion. fetch is stubbed with vi.stubGlobal and
// routed per "METHOD /url" (06-decisions.md "Testing tooling").
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
import RecommendationPanel from './RecommendationPanel.jsx'

const jsonResponse = (body, { ok = true, status = 200 } = {}) => ({
  ok,
  status,
  json: () => Promise.resolve(body),
})

const taxonomy = { formality: ['casual', 'smart-casual', 'formal'] }

const weather = {
  location: { name: 'Berlin' },
  current: { temperature_c: 18, apparent_temperature_c: 17, weather_code: 0 },
  today: {
    temperature_min_c: 9,
    temperature_max_c: 20,
    precipitation_probability_max: 10,
  },
}

const catalogItem = {
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

const garment = (id, subcategory) => ({
  id,
  photo_url: `/api/photos/${id}.jpg`,
  category: 'top',
  subcategory,
  dominant_color: 'olive',
})

// A route value is a response, or a function returning a promise of one.
// An unregistered request rejects, so a stray call fails the test loudly.
function stubRoutes(routes) {
  const fetchMock = vi.fn((url, options = {}) => {
    const key = `${options.method ?? 'GET'} ${url}`
    const route = routes[key]
    if (!route) return Promise.reject(new Error(`unexpected request: ${key}`))
    return Promise.resolve(typeof route === 'function' ? route() : route)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const appRoutes = (recommend) => ({
  'GET /api/items': jsonResponse([catalogItem]),
  'GET /api/weather': jsonResponse(weather),
  'GET /api/taxonomy': jsonResponse(taxonomy),
  'POST /api/recommendations': recommend,
})

const posts = (fetchMock) =>
  fetchMock.mock.calls.filter(([, options]) => options?.method === 'POST')

const suggestButton = () => screen.getByRole('button', { name: 'Suggest outfits' })

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('ING-050 - recommendation panel', () => {
  it('AC1: shows the panel under the weather panel with its controls and does not POST on load', async () => {
    const fetchMock = stubRoutes(appRoutes(jsonResponse({ outfits: [] })))

    render(<App />)

    const weatherText = await screen.findByText('Berlin')
    await waitFor(() => expect(screen.getAllByRole('option')).toHaveLength(4))
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual([
      'any',
      'casual',
      'smart-casual',
      'formal',
    ])
    expect(screen.getByLabelText('Formality')).toBeInTheDocument()
    expect(screen.getByLabelText('Note')).toHaveAttribute('type', 'text')
    // The weather text comes before the panel's button in document order.
    expect(
      weatherText.compareDocumentPosition(suggestButton()) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(posts(fetchMock)).toHaveLength(0)
  })

  it('AC2: POSTs the chosen formality and note, and omits both for "any" with an empty note', async () => {
    const fetchMock = stubRoutes({
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'POST /api/recommendations': jsonResponse({ outfits: [] }),
    })

    render(<RecommendationPanel />)
    await waitFor(() => expect(screen.getAllByRole('option')).toHaveLength(4))

    fireEvent.click(suggestButton())
    await waitFor(() => expect(posts(fetchMock)).toHaveLength(1))
    const [url, options] = posts(fetchMock)[0]
    expect(url).toBe('/api/recommendations')
    expect(options.headers).toEqual({ 'Content-Type': 'application/json' })
    expect(JSON.parse(options.body)).toEqual({})
    await waitFor(() => expect(suggestButton()).toBeEnabled())

    fireEvent.change(screen.getByLabelText('Formality'), { target: { value: 'formal' } })
    fireEvent.change(screen.getByLabelText('Note'), { target: { value: 'wedding' } })
    fireEvent.click(suggestButton())
    await waitFor(() => expect(posts(fetchMock)).toHaveLength(2))
    expect(posts(fetchMock)[1][1].body).toBe('{"formality":"formal","note":"wedding"}')
  })

  it('ING-061: "Ignore weather" starts unchecked and sends ignore_weather only when checked', async () => {
    const fetchMock = stubRoutes({
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'POST /api/recommendations': jsonResponse({ outfits: [] }),
    })

    render(<RecommendationPanel />)
    const box = screen.getByLabelText('Ignore weather')
    expect(box).toHaveAttribute('type', 'checkbox')
    expect(box).not.toBeChecked()

    fireEvent.click(box)
    fireEvent.click(suggestButton())
    await waitFor(() => expect(posts(fetchMock)).toHaveLength(1))
    expect(JSON.parse(posts(fetchMock)[0][1].body)).toEqual({ ignore_weather: true })
    await waitFor(() => expect(suggestButton()).toBeEnabled())

    fireEvent.click(box)
    fireEvent.click(suggestButton())
    await waitFor(() => expect(posts(fetchMock)).toHaveLength(2))
    expect(JSON.parse(posts(fetchMock)[1][1].body)).toEqual({})
  })

  it('AC3: disables the button and shows a loading message while the request is in flight', async () => {
    let resolve
    const pending = new Promise((r) => {
      resolve = r
    })
    stubRoutes({
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'POST /api/recommendations': () => pending,
    })

    render(<RecommendationPanel />)
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(suggestButton()).toBeEnabled()

    fireEvent.click(suggestButton())

    expect(suggestButton()).toBeDisabled()
    expect(screen.getByRole('status')).toBeInTheDocument()

    resolve(jsonResponse({ outfits: [] }))
    await waitFor(() => expect(suggestButton()).toBeEnabled())
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('AC4: renders 3 outfit rows with photos in response order and the reason, with a placeholder for a broken photo', async () => {
    const outfits = [
      { items: [garment('a1', 'shirt'), garment('a2', 'jeans')], reason: 'Reason one.' },
      { items: [garment('b1', 'hoodie'), garment('b2', 'shorts')], reason: 'Reason two.' },
      { items: [garment('c1', 'blazer'), garment('c2', 'chinos')], reason: 'Reason three.' },
    ]
    stubRoutes({
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'POST /api/recommendations': jsonResponse({ outfits }),
    })

    render(<RecommendationPanel />)
    fireEvent.click(suggestButton())

    const rows = await screen.findAllByRole('listitem')
    expect(rows).toHaveLength(3)
    outfits.forEach((outfit, i) => {
      const row = within(rows[i])
      expect(row.getAllByRole('img').map((img) => img.getAttribute('src'))).toEqual(
        outfit.items.map((item) => item.photo_url),
      )
      expect(row.getByText(outfit.reason)).toBeInTheDocument()
    })

    // A photo that fails to load swaps to the same placeholder the grid uses.
    fireEvent.error(within(rows[1]).getByRole('img', { name: 'olive shorts' }))
    expect(await within(rows[1]).findByRole('img', { name: 'Missing photo' })).toBeInTheDocument()
    expect(within(rows[1]).getByRole('img', { name: 'olive hoodie' })).toBeInTheDocument()
    expect(within(rows[0]).queryByRole('img', { name: 'Missing photo' })).not.toBeInTheDocument()
  })

  it('AC5: shows the server error for 400, 422, and 502 and leaves the weather panel and catalog alone', async () => {
    const cases = [
      [400, 'invalid formality'],
      [422, 'not enough garments for an outfit'],
      [502, 'the language model is unavailable'],
    ]
    for (const [status, message] of cases) {
      stubRoutes(appRoutes(jsonResponse({ error: message }, { ok: false, status })))

      render(<App />)
      await screen.findByText('linen')
      await screen.findByText('Berlin')
      fireEvent.click(suggestButton())

      expect(await screen.findByRole('alert')).toHaveTextContent(message)
      expect(screen.queryAllByRole('img', { name: 'Missing photo' })).toHaveLength(0)
      expect(screen.getByText('Berlin')).toBeInTheDocument()
      expect(screen.getByText('linen')).toBeInTheDocument()
      expect(screen.getAllByRole('listitem')).toHaveLength(1)

      cleanup()
      vi.unstubAllGlobals()
    }
  })

  it('AC6: shows a generic message when the request fails with no response and leaves the weather panel and catalog alone', async () => {
    stubRoutes(appRoutes(() => Promise.reject(new Error('offline'))))

    render(<App />)
    await screen.findByText('linen')
    await screen.findByText('Berlin')
    fireEvent.click(suggestButton())

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not reach the server.')
    expect(suggestButton()).toBeEnabled()
    expect(screen.getByText('Berlin')).toBeInTheDocument()
    expect(screen.getByText('linen')).toBeInTheDocument()
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
  })

  it('AC7: replaces the old outfits when the user asks again', async () => {
    const responses = [
      { outfits: [{ items: [garment('a1', 'shirt')], reason: 'First reason.' }] },
      {
        outfits: [
          { items: [garment('b1', 'hoodie')], reason: 'Second reason.' },
          { items: [garment('c1', 'blazer')], reason: 'Third reason.' },
        ],
      },
    ]
    let call = 0
    stubRoutes({
      'GET /api/taxonomy': jsonResponse(taxonomy),
      'POST /api/recommendations': () => jsonResponse(responses[call++]),
    })

    render(<RecommendationPanel />)
    fireEvent.click(suggestButton())
    expect(await screen.findByText('First reason.')).toBeInTheDocument()
    expect(screen.getAllByRole('listitem')).toHaveLength(1)

    fireEvent.click(suggestButton())

    expect(await screen.findByText('Second reason.')).toBeInTheDocument()
    expect(screen.getByText('Third reason.')).toBeInTheDocument()
    expect(screen.queryByText('First reason.')).not.toBeInTheDocument()
    expect(screen.getAllByRole('listitem')).toHaveLength(2)
  })
})

// ING-050 - Tester edge cases for the recommendation panel.
// Covers spec edges the Coder's file does not prove. fetch is stubbed with
// vi.stubGlobal and routed per "METHOD /url".
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import RecommendationPanel from './RecommendationPanel.jsx'

const jsonResponse = (body, { ok = true, status = 200 } = {}) => ({
  ok,
  status,
  json: () => Promise.resolve(body),
})

const taxonomy = { formality: ['casual', 'smart-casual', 'formal'] }

function stubRoutes(routes) {
  const fetchMock = vi.fn((url, options = {}) => {
    const route = routes[`${options.method ?? 'GET'} ${url}`]
    if (!route) return Promise.reject(new Error('unexpected request'))
    return Promise.resolve(typeof route === 'function' ? route() : route)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const posts = (fetchMock) =>
  fetchMock.mock.calls.filter(([, options]) => options?.method === 'POST')
const suggestButton = () => screen.getByRole('button', { name: 'Suggest outfits' })
const withRecommend = (recommend) => ({
  'GET /api/taxonomy': jsonResponse(taxonomy),
  'POST /api/recommendations': recommend,
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('ING-050 - tester edge cases', () => {
  it('trims the note, omits a whitespace-only note, and passes no abort signal', async () => {
    const fetchMock = stubRoutes(withRecommend(jsonResponse({ outfits: [] })))
    render(<RecommendationPanel />)

    fireEvent.change(screen.getByLabelText('Note'), { target: { value: '   ' } })
    fireEvent.click(suggestButton())
    await waitFor(() => expect(posts(fetchMock)).toHaveLength(1))
    expect(posts(fetchMock)[0][1].body).toBe('{}')
    await waitFor(() => expect(suggestButton()).toBeEnabled())

    fireEvent.change(screen.getByLabelText('Note'), { target: { value: '  dinner  ' } })
    fireEvent.click(suggestButton())
    await waitFor(() => expect(posts(fetchMock)).toHaveLength(2))
    const options = posts(fetchMock)[1][1]
    expect(options.body).toBe('{"note":"dinner"}')
    expect(options.signal).toBeUndefined()
    expect(options.headers['Content-Type']).toBe('application/json')
  })

  it('stays busy while the response body is still being read', async () => {
    let resolveBody
    const body = new Promise((r) => {
      resolveBody = r
    })
    stubRoutes(withRecommend({ ok: true, status: 200, json: () => body }))
    render(<RecommendationPanel />)

    fireEvent.click(suggestButton())
    await new Promise((r) => setTimeout(r, 20))
    expect(suggestButton()).toBeDisabled()
    expect(screen.getByRole('status')).toBeInTheDocument()

    resolveBody({ outfits: [] })
    await waitFor(() => expect(suggestButton()).toBeEnabled())
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('shows the placeholder for an item with no photo_url and keeps the row order', async () => {
    const items = [
      { id: 'a', photo_url: '/p/a.jpg', subcategory: 'shirt', dominant_color: 'navy' },
      { id: 'b', subcategory: 'jeans', dominant_color: 'blue' },
      { id: 'c', photo_url: '/p/c.jpg', subcategory: 'boots', dominant_color: 'brown' },
    ]
    stubRoutes(withRecommend(jsonResponse({ outfits: [{ items, reason: 'R.' }] })))
    render(<RecommendationPanel />)
    fireEvent.click(suggestButton())

    const row = within(await screen.findByRole('listitem'))
    expect(row.getAllByRole('img').map((el) => el.getAttribute('src') ?? el.getAttribute('aria-label'))).toEqual([
      '/p/a.jpg',
      'Missing photo',
      '/p/c.jpg',
    ])
  })

  it.each([
    ['outfits missing', { weather: {} }],
    ['outfits is not an array', { outfits: 'nope' }],
    ['outfits is null', { outfits: null }],
    ['body is null', null],
  ])('does not crash on a 200 with %s', async (_name, body) => {
    stubRoutes(withRecommend(jsonResponse(body)))
    render(<RecommendationPanel />)
    fireEvent.click(suggestButton())

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not get recommendations.')
    expect(suggestButton()).toBeEnabled()
    expect(screen.queryByRole('listitem')).not.toBeInTheDocument()
  })

  it('does not crash on an outfit with no items array', async () => {
    stubRoutes(withRecommend(jsonResponse({ outfits: [{ reason: 'Only a reason.' }] })))
    render(<RecommendationPanel />)
    fireEvent.click(suggestButton())

    expect(await screen.findByText('Only a reason.')).toBeInTheDocument()
    expect(screen.queryAllByRole('img')).toHaveLength(0)
  })

  it('falls back to an HTTP status message when the error body is not JSON', async () => {
    stubRoutes(
      withRecommend({ ok: false, status: 500, json: () => Promise.reject(new SyntaxError('bad')) }),
    )
    render(<RecommendationPanel />)
    fireEvent.click(suggestButton())

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not get recommendations (HTTP 500).')
    expect(suggestButton()).toBeEnabled()
  })

  it('falls back to an HTTP status message when the error JSON has no error field', async () => {
    stubRoutes(withRecommend(jsonResponse({ detail: 'x' }, { ok: false, status: 503 })))
    render(<RecommendationPanel />)
    fireEvent.click(suggestButton())

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not get recommendations (HTTP 503).')
  })

  it.each([
    ['rejects', () => Promise.reject(new Error('down'))],
    ['is non-OK', () => jsonResponse({ error: 'x' }, { ok: false, status: 500 })],
    ['has the wrong shape', () => jsonResponse({ formality: 'casual' })],
  ])('keeps the select usable with "any" when the taxonomy fetch %s', async (_name, taxRoute) => {
    const fetchMock = stubRoutes({
      'GET /api/taxonomy': taxRoute,
      'POST /api/recommendations': jsonResponse({ outfits: [] }),
    })
    render(<RecommendationPanel />)
    await new Promise((r) => setTimeout(r, 20))

    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['any'])
    fireEvent.click(suggestButton())
    await waitFor(() => expect(posts(fetchMock)).toHaveLength(1))
    expect(posts(fetchMock)[0][1].body).toBe('{}')
  })

  it('clears old outfits when the next request fails, and re-enables the button', async () => {
    const answers = [
      jsonResponse({ outfits: [{ items: [], reason: 'First reason.' }] }),
      jsonResponse({ error: 'llm down' }, { ok: false, status: 502 }),
    ]
    let call = 0
    stubRoutes(withRecommend(() => answers[call++]))
    render(<RecommendationPanel />)

    fireEvent.click(suggestButton())
    expect(await screen.findByText('First reason.')).toBeInTheDocument()
    fireEvent.click(suggestButton())

    expect(await screen.findByRole('alert')).toHaveTextContent('llm down')
    expect(screen.queryByText('First reason.')).not.toBeInTheDocument()
    expect(suggestButton()).toBeEnabled()
  })

  it('clears the old error when the next request succeeds', async () => {
    const answers = [
      jsonResponse({ error: 'llm down' }, { ok: false, status: 502 }),
      jsonResponse({ outfits: [{ items: [], reason: 'Now fine.' }] }),
    ]
    let call = 0
    stubRoutes(withRecommend(() => answers[call++]))
    render(<RecommendationPanel />)

    fireEvent.click(suggestButton())
    expect(await screen.findByRole('alert')).toHaveTextContent('llm down')
    fireEvent.click(suggestButton())

    expect(await screen.findByText('Now fine.')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})

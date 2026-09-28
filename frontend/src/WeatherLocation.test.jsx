// ING-045 — "change location" city search on WeatherPanel: search,
// no-request-on-empty-text, empty-results, search failure, PUT on pick
// (admin1 dropped), reload on success, 400 keeps previous location, cancel
// sends no PUT. fetch stubbed per method+URL with Vitest's vi.stubGlobal
// (06-decisions.md "Testing tooling"), same pattern as
// ing_034_add.test.jsx/WeatherPanel.test.jsx.
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import WeatherPanel from './WeatherPanel.jsx'

const weatherBody = (overrides = {}) => ({
  location: { name: 'Tehran', country: 'Iran', latitude: 35.69439, longitude: 51.42151 },
  current: { temperature_c: 21.3, apparent_temperature_c: 20.1, weather_code: 3, precipitation_mm: 0.0 },
  today: { temperature_min_c: 14.2, temperature_max_c: 25.8, precipitation_probability_max: 10, weather_code: 3 },
  ...overrides,
})

const jsonResponse = (body, { ok = true, status = 200 } = {}) => ({
  ok,
  status,
  json: () => Promise.resolve(body),
})

// Routes by "METHOD url" so a GET and a PUT to the same path are distinct,
// and an unregistered request rejects loudly.
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

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

async function openChange() {
  fireEvent.click(await screen.findByRole('button', { name: 'Change location' }))
  return screen.findByRole('form', { name: 'Change location' })
}

describe('WeatherPanel — change location (ING-045)', () => {
  it('AC1: submitting search text GETs /api/weather/cities?q=<encoded text> and lists name/admin1/country', async () => {
    const fetchMock = stubRoutes({
      'GET /api/weather': jsonResponse(weatherBody()),
      'GET /api/weather/cities?q=new%20york': jsonResponse([
        { name: 'New York', country: 'United States', admin1: 'New York', latitude: 40.7128, longitude: -74.006 },
      ]),
    })

    render(<WeatherPanel />)
    const form = await openChange()
    fireEvent.change(within(form).getByLabelText('City'), { target: { value: 'new york' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Search' }))

    expect(await screen.findByText('New York, New York, United States')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/weather/cities?q=new%20york')
  })

  it('AC2: an empty result list shows "no cities found"', async () => {
    stubRoutes({
      'GET /api/weather': jsonResponse(weatherBody()),
      'GET /api/weather/cities?q=nowhere': jsonResponse([]),
    })

    render(<WeatherPanel />)
    const form = await openChange()
    fireEvent.change(within(form).getByLabelText('City'), { target: { value: 'nowhere' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Search' }))

    expect(await screen.findByText('No cities found.')).toBeInTheDocument()
  })

  it('AC3: empty or whitespace-only text sends no request on submit', async () => {
    const fetchMock = stubRoutes({ 'GET /api/weather': jsonResponse(weatherBody()) })

    render(<WeatherPanel />)
    const form = await openChange()
    fireEvent.change(within(form).getByLabelText('City'), { target: { value: '   ' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Search' }))

    // Give any accidental async work a tick, then assert no cities call happened.
    await new Promise((r) => setTimeout(r, 0))
    expect(fetchMock.mock.calls.some(([url]) => url.startsWith('/api/weather/cities'))).toBe(false)
  })

  it('AC4: a 502/failed search shows an error and the current location stays unchanged', async () => {
    stubRoutes({
      'GET /api/weather': jsonResponse(weatherBody()),
      'GET /api/weather/cities?q=paris': jsonResponse({ error: 'geocoding unavailable' }, { ok: false, status: 502 }),
    })

    render(<WeatherPanel />)
    await screen.findByText('Tehran')
    const form = await openChange()
    fireEvent.change(within(form).getByLabelText('City'), { target: { value: 'paris' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Search' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('geocoding unavailable')
    expect(screen.getByText('Tehran')).toBeInTheDocument()
  })

  it('AC5: picking a result PUTs {name,country,latitude,longitude} (admin1 dropped), then reloads and shows the new location', async () => {
    const city = { name: 'New York', country: 'United States', admin1: 'New York', latitude: 40.7128, longitude: -74.006 }
    const fetchMock = stubRoutes({
      'GET /api/weather': jsonResponse(weatherBody()),
      'GET /api/weather/cities?q=new%20york': jsonResponse([city]),
      'PUT /api/weather/location': jsonResponse({
        name: 'New York',
        country: 'United States',
        latitude: 40.7128,
        longitude: -74.006,
      }),
    })

    render(<WeatherPanel />)
    const form = await openChange()
    fireEvent.change(within(form).getByLabelText('City'), { target: { value: 'new york' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Search' }))
    fireEvent.click(await screen.findByRole('button', { name: 'New York, New York, United States' }))

    await waitFor(() => {
      const putCall = fetchMock.mock.calls.find(
        ([url, options]) => url === '/api/weather/location' && options?.method === 'PUT',
      )
      expect(putCall).toBeTruthy()
      const body = JSON.parse(putCall[1].body)
      expect(body).toEqual({
        name: 'New York',
        country: 'United States',
        latitude: 40.7128,
        longitude: -74.006,
      })
    })

    // GET /api/weather is called again after the PUT (reload).
    const weatherCalls = fetchMock.mock.calls.filter(([url]) => url === '/api/weather')
    expect(weatherCalls.length).toBe(2)
    // Search form closes after a successful save.
    await waitFor(() =>
      expect(screen.queryByRole('form', { name: 'Change location' })).not.toBeInTheDocument(),
    )
  })

  it('AC6: a PUT 400 shows the server\'s field-naming error and keeps the previous location', async () => {
    const city = { name: 'Nowhere', country: '', admin1: '', latitude: 0, longitude: 0 }
    stubRoutes({
      'GET /api/weather': jsonResponse(weatherBody()),
      'GET /api/weather/cities?q=nowhere': jsonResponse([city]),
      'PUT /api/weather/location': jsonResponse({ error: 'name is required' }, { ok: false, status: 400 }),
    })

    render(<WeatherPanel />)
    await screen.findByText('Tehran')
    const form = await openChange()
    fireEvent.change(within(form).getByLabelText('City'), { target: { value: 'nowhere' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Search' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Nowhere' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('name is required')
    expect(screen.getByText('Tehran')).toBeInTheDocument()
  })

  it('AC4b: a search network failure (fetch rejects, not just 502) shows an error and keeps the current location', async () => {
    const fetchMock = vi.fn((url) => {
      if (url === '/api/weather') return Promise.resolve(jsonResponse(weatherBody()))
      if (url.startsWith('/api/weather/cities')) return Promise.reject(new Error('network down'))
      return Promise.reject(new Error(`unexpected request: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<WeatherPanel />)
    await screen.findByText('Tehran')
    const form = await openChange()
    fireEvent.change(within(form).getByLabelText('City'), { target: { value: 'paris' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Search' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not reach the server.')
    expect(screen.getByText('Tehran')).toBeInTheDocument()
  })

  it('AC5b: after a successful save, the reloaded GET /api/weather location actually renders (not just the same old data)', async () => {
    const city = { name: 'Paris', country: 'France', admin1: 'Ile-de-France', latitude: 48.8566, longitude: 2.3522 }
    let weatherCalls = 0
    const fetchMock = vi.fn((url, options = {}) => {
      const key = `${options.method ?? 'GET'} ${url}`
      if (key === 'GET /api/weather') {
        weatherCalls += 1
        const body =
          weatherCalls === 1
            ? weatherBody()
            : weatherBody({ location: { name: 'Paris', country: 'France', latitude: 48.8566, longitude: 2.3522 } })
        return Promise.resolve(jsonResponse(body))
      }
      if (key === 'GET /api/weather/cities?q=paris') return Promise.resolve(jsonResponse([city]))
      if (key === 'PUT /api/weather/location') {
        return Promise.resolve(
          jsonResponse({ name: 'Paris', country: 'France', latitude: 48.8566, longitude: 2.3522 }),
        )
      }
      return Promise.reject(new Error(`unexpected request: ${key}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<WeatherPanel />)
    await screen.findByText('Tehran')
    const form = await openChange()
    fireEvent.change(within(form).getByLabelText('City'), { target: { value: 'paris' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Search' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Paris, Ile-de-France, France' }))

    expect(await screen.findByText('Paris')).toBeInTheDocument()
    expect(screen.queryByText('Tehran')).not.toBeInTheDocument()
  })

  it('AC7: cancel sends no PUT and leaves the panel unchanged', async () => {
    const fetchMock = stubRoutes({
      'GET /api/weather': jsonResponse(weatherBody()),
      'GET /api/weather/cities?q=paris': jsonResponse([
        { name: 'Paris', country: 'France', admin1: 'Ile-de-France', latitude: 48.8566, longitude: 2.3522 },
      ]),
    })

    render(<WeatherPanel />)
    const form = await openChange()
    fireEvent.change(within(form).getByLabelText('City'), { target: { value: 'paris' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Search' }))
    await screen.findByText('Paris, Ile-de-France, France')

    fireEvent.click(within(form).getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('form', { name: 'Change location' })).not.toBeInTheDocument()
    expect(screen.getByText('Tehran')).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(([url, options]) => url === '/api/weather/location' && options?.method === 'PUT'),
    ).toBe(false)
  })
})

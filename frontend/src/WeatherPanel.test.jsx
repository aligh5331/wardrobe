// ING-044 — weather panel: success render, WMO code → label, unmapped code,
// null fields render "-", and a 502/failure that doesn't touch the catalog.
// fetch stubbed per-URL with Vitest's vi.stubGlobal (06-decisions.md
// "Testing tooling"), same pattern as App.test.jsx / ing_034_add.test.jsx.
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import App from './App.jsx'
import WeatherPanel from './WeatherPanel.jsx'

const weatherBody = (overrides = {}) => ({
  location: { name: 'Tehran', country: 'Iran', latitude: 35.69439, longitude: 51.42151 },
  current: {
    temperature_c: 21.3,
    apparent_temperature_c: 20.1,
    weather_code: 3,
    precipitation_mm: 0.0,
  },
  today: {
    temperature_min_c: 14.2,
    temperature_max_c: 25.8,
    precipitation_probability_max: 10,
    weather_code: 3,
  },
  ...overrides,
})

const jsonResponse = (body, { ok = true, status = 200 } = {}) => ({
  ok,
  status,
  json: () => Promise.resolve(body),
})

function stubRoutes(routes) {
  const fetchMock = vi.fn((url) => {
    const route = routes[url]
    if (!route) return Promise.reject(new Error(`unexpected request: ${url}`))
    return Promise.resolve(route)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('WeatherPanel (ING-044)', () => {
  it('AC1: renders location, current temp/apparent/condition, and today min/max/precip on 200', async () => {
    stubRoutes({ '/api/weather': jsonResponse(weatherBody()) })

    render(<WeatherPanel />)
    const panel = await screen.findByText('Tehran')
    const section = panel.closest('section')

    for (const text of ['Tehran', '21.3°C', '20.1°C', '14.2°C', '25.8°C', '10%']) {
      expect(within(section).getByText(new RegExp(text.replace('.', '\\.')))).toBeInTheDocument()
    }
  })

  it('AC2: maps a known weather_code to its label', async () => {
    stubRoutes({ '/api/weather': jsonResponse(weatherBody({ current: { ...weatherBody().current, weather_code: 0 } })) })

    render(<WeatherPanel />)

    expect(await screen.findByText('Clear')).toBeInTheDocument()
  })

  it('AC3: an unmapped weather_code still renders the other values without crashing', async () => {
    stubRoutes({
      '/api/weather': jsonResponse(weatherBody({ current: { ...weatherBody().current, weather_code: 12345 } })),
    })

    render(<WeatherPanel />)

    expect(await screen.findByText('Tehran')).toBeInTheDocument()
    expect(screen.getByText(/12345/)).toBeInTheDocument()
    expect(screen.getByText(/21\.3/)).toBeInTheDocument()
  })

  it('AC4: null fields render "-" with unit, null weather_code renders "-", non-null values still render', async () => {
    stubRoutes({
      '/api/weather': jsonResponse({
        location: { name: 'Tehran', country: 'Iran', latitude: 35.69439, longitude: 51.42151 },
        current: {
          temperature_c: 21.3,
          apparent_temperature_c: null,
          weather_code: null,
          precipitation_mm: null,
        },
        today: {
          temperature_min_c: null,
          temperature_max_c: 25.8,
          precipitation_probability_max: null,
          weather_code: null,
        },
      }),
    })

    render(<WeatherPanel />)
    await screen.findByText('Tehran')

    // Non-null values still render, never "0" for null.
    expect(screen.getByText(/21\.3/)).toBeInTheDocument()
    expect(screen.getByText(/25\.8/)).toBeInTheDocument()
    expect(screen.queryByText('0°C')).not.toBeInTheDocument()
    expect(screen.queryByText('0%')).not.toBeInTheDocument()

    // Nulls: "- °C" / "- %" per the ticket's literal format (apparent temp
    // AND today min both null → two occurrences), condition "-".
    expect(screen.getAllByText((text) => text.includes('- °C')).length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText((text) => text.includes('- %'))).toBeInTheDocument()
    expect(screen.getByText('-', { selector: 'span' })).toBeInTheDocument()
  })

  it('AC5: a 502 shows a weather error message and the catalog grid + Add garment flow are unaffected', async () => {
    const fetchMock = stubRoutes({
      '/api/items': jsonResponse([]),
      '/api/weather': jsonResponse({ error: 'weather service unavailable' }, { ok: false, status: 502 }),
    })

    render(<App />)

    expect(await screen.findByText('Could not load the weather.')).toBeInTheDocument()
    // Catalog grid loaded fine, independent fetch.
    expect(
      await screen.findByText('No items cataloged yet. Run the ingestion pipeline to add garments.'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add garment' })).toBeEnabled()
    expect(fetchMock).toHaveBeenCalledWith('/api/items')
    expect(fetchMock).toHaveBeenCalledWith('/api/weather')
  })

  it('AC5b: a network failure (fetch rejects, no 502) shows the same weather error, catalog unaffected', async () => {
    const fetchMock = vi.fn((url) => {
      if (url === '/api/items') return Promise.resolve(jsonResponse([]))
      if (url === '/api/weather') return Promise.reject(new Error('network down'))
      return Promise.reject(new Error(`unexpected request: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<App />)

    expect(await screen.findByText('Could not load the weather.')).toBeInTheDocument()
    expect(
      await screen.findByText('No items cataloged yet. Run the ingestion pipeline to add garments.'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add garment' })).toBeEnabled()
  })
})

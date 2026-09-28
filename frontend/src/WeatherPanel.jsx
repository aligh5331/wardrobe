import { useEffect, useState } from 'react'

// ING-044 — small weather panel: current + today, independent of the items
// fetch so a weather failure never blocks the catalog grid or add flow
// (07-architecture.md "Weather (Phase 2)").

// Frontend-only WMO code → label map; the backend sends only the raw code
// ("weather_code is the raw WMO code; mapping it to a label/icon is a
// frontend concern"). Unmapped codes fall back to the raw code so the panel
// never crashes on a code outside this list.
const WMO_LABELS = {
  0: 'Clear',
  1: 'Mainly clear',
  2: 'Partly cloudy',
  3: 'Cloudy',
  45: 'Fog',
  48: 'Fog',
  51: 'Drizzle',
  53: 'Drizzle',
  55: 'Drizzle',
  56: 'Drizzle',
  57: 'Drizzle',
  61: 'Rain',
  63: 'Rain',
  65: 'Rain',
  66: 'Rain',
  67: 'Rain',
  71: 'Snow',
  73: 'Snow',
  75: 'Snow',
  77: 'Snow',
  80: 'Showers',
  81: 'Showers',
  82: 'Showers',
  95: 'Thunderstorm',
  96: 'Thunderstorm',
  99: 'Thunderstorm',
}

const conditionLabel = (code) => {
  if (code === null || code === undefined) return '-'
  return WMO_LABELS[code] ?? `Code ${code}`
}

// null/undefined → "- <unit>" (e.g. "- °C"), otherwise the number with its
// unit; never "0" for null.
const withUnit = (value, unit) =>
  value === null || value === undefined ? `- ${unit}` : `${value}${unit}`

// Pull the server's named-field error out of a non-OK body, or fall back —
// same convention as ItemAddForm.jsx.
async function errorMessage(res, fallback) {
  try {
    const body = await res.json()
    if (body?.error) return body.error
  } catch {
    // Non-JSON error body: keep the fallback.
  }
  return fallback
}

export default function WeatherPanel() {
  const [weather, setWeather] = useState(null)
  const [status, setStatus] = useState('loading')

  // ING-045 — "change location" city search, closed by default.
  const [changing, setChanging] = useState(false)
  const [query, setQuery] = useState('')
  const [searchStatus, setSearchStatus] = useState('idle') // idle | loading | done | error
  const [searchError, setSearchError] = useState('')
  const [results, setResults] = useState([])
  const [saveError, setSaveError] = useState('')
  const [saving, setSaving] = useState(false)

  const loadWeather = () =>
    fetch('/api/weather')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.json()
      })
      .then((data) => {
        setWeather(data)
        setStatus('ready')
      })

  useEffect(() => {
    let cancelled = false
    loadWeather().catch(() => {
      if (cancelled) return
      setStatus('error')
    })
    return () => {
      cancelled = true
    }
  }, [])

  const openChange = () => {
    setChanging(true)
    setQuery('')
    setSearchStatus('idle')
    setSearchError('')
    setResults([])
    setSaveError('')
  }

  const cancelChange = () => {
    setChanging(false)
    setQuery('')
    setSearchStatus('idle')
    setSearchError('')
    setResults([])
    setSaveError('')
  }

  const submitSearch = async (event) => {
    event.preventDefault()
    const text = query.trim()
    if (!text) return // empty/whitespace-only: no request (backend would 400)

    setSearchStatus('loading')
    setSearchError('')
    setSaveError('')

    let res
    try {
      res = await fetch(`/api/weather/cities?q=${encodeURIComponent(text)}`)
    } catch {
      setSearchStatus('error')
      setSearchError('Could not reach the server.')
      return
    }

    if (!res.ok) {
      setSearchStatus('error')
      setSearchError(await errorMessage(res, `Could not search cities (HTTP ${res.status}).`))
      return
    }

    const data = await res.json()
    setResults(Array.isArray(data) ? data : [])
    setSearchStatus('done')
  }

  const pickCity = async (city) => {
    setSaving(true)
    setSaveError('')

    let res
    try {
      res = await fetch('/api/weather/location', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: city.name,
          country: city.country,
          latitude: city.latitude,
          longitude: city.longitude,
        }),
      })
    } catch {
      setSaving(false)
      setSaveError('Could not reach the server.')
      return
    }

    if (!res.ok) {
      setSaving(false)
      setSaveError(await errorMessage(res, `Could not save location (HTTP ${res.status}).`))
      return
    }

    try {
      await loadWeather()
    } catch {
      // Save succeeded but the reload failed; leave the previous panel data
      // rather than blank it — the location is saved regardless.
    }
    setSaving(false)
    cancelChange()
  }

  if (status === 'loading') {
    return <p className="text-sm text-gray-500">Loading weather…</p>
  }

  if (status === 'error') {
    return <p className="text-sm text-red-600">Could not load the weather.</p>
  }

  const { location, current, today } = weather

  return (
    <section className="flex flex-col gap-2 rounded-lg border border-gray-200 bg-white px-4 py-2 text-sm">
      <div className="flex flex-wrap items-center gap-4">
        <span className="font-semibold text-gray-900">{location?.name ?? '-'}</span>
        <span className="text-gray-800">{withUnit(current?.temperature_c, '°C')}</span>
        <span className="text-gray-500">Feels like {withUnit(current?.apparent_temperature_c, '°C')}</span>
        <span className="text-gray-500">{conditionLabel(current?.weather_code)}</span>
        <span className="text-gray-500">
          {withUnit(today?.temperature_min_c, '°C')} / {withUnit(today?.temperature_max_c, '°C')}
        </span>
        <span className="text-gray-500">{withUnit(today?.precipitation_probability_max, '%')} rain</span>
        {!changing && (
          <button
            type="button"
            onClick={openChange}
            className="rounded border border-gray-300 px-2 py-1 text-sm text-gray-700 hover:bg-gray-50"
          >
            Change location
          </button>
        )}
      </div>

      {changing && (
        <div className="flex flex-col gap-2 border-t border-gray-200 pt-2">
          <form onSubmit={submitSearch} aria-label="Change location" className="flex gap-2">
            <input
              type="text"
              aria-label="City"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              className="rounded border border-gray-300 px-2 py-1 text-sm"
            />
            <button
              type="submit"
              disabled={searchStatus === 'loading' || saving}
              className="rounded bg-gray-900 px-3 py-1 text-sm text-white disabled:opacity-50"
            >
              {searchStatus === 'loading' ? 'Searching…' : 'Search'}
            </button>
            <button
              type="button"
              onClick={cancelChange}
              className="rounded border border-gray-300 px-3 py-1 text-sm"
            >
              Cancel
            </button>
          </form>

          {searchStatus === 'error' && (
            <p role="alert" className="text-sm text-red-600">
              {searchError}
            </p>
          )}
          {saveError && (
            <p role="alert" className="text-sm text-red-600">
              {saveError}
            </p>
          )}

          {searchStatus === 'done' && results.length === 0 && (
            <p className="text-sm text-gray-500">No cities found.</p>
          )}

          {searchStatus === 'done' && results.length > 0 && (
            <ul className="flex flex-col gap-1">
              {results.map((city, index) => (
                <li key={`${city.name}-${city.latitude}-${city.longitude}-${index}`}>
                  <button
                    type="button"
                    disabled={saving}
                    onClick={() => pickCity(city)}
                    className="rounded border border-gray-200 px-2 py-1 text-left text-sm hover:bg-gray-50 disabled:opacity-50"
                  >
                    {[city.name, city.admin1, city.country].filter(Boolean).join(', ')}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </section>
  )
}

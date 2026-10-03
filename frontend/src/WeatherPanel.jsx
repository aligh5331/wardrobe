import { useEffect, useState } from 'react'
import {
  Icon,
  Spinner,
  buttonGhost,
  buttonPrimary,
  buttonSecondary,
  cardClass,
  errorClass,
  inputClass,
} from './ui.jsx'

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
  85: 'Snow showers',
  86: 'Snow showers',
  95: 'Thunderstorm',
  96: 'Thunderstorm',
  99: 'Thunderstorm',
}

// Condition label → icon, so the icon always agrees with the label above.
// Anything unmapped (or a null code) gets the plain cloud in a muted color.
const CLOUD = 'M6.5 18a4.5 4.5 0 0 1-.4-8.98A6 6 0 0 1 17.6 9.5 4.25 4.25 0 0 1 17.5 18z'
const CLOUD_HIGH = 'M6.5 14a4.5 4.5 0 0 1-.4-8.98A6 6 0 0 1 17.6 5.5 4.25 4.25 0 0 1 17.5 14z'
const SUN_RAYS =
  'M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4'

const ICON_PARTS = {
  sun: [
    ['text-amber-500', <circle key="c" cx="12" cy="12" r="4" />],
    ['text-amber-500', <path key="r" d={SUN_RAYS} />],
  ],
  partly: [
    ['text-amber-500', <circle key="c" cx="8" cy="8" r="3" />],
    ['text-amber-500', <path key="r" d="M8 2.5v1M2.5 8h1M4.1 4.1l.7.7M11.9 4.1l-.7.7" />],
    [
      'fill-white text-stone-400',
      <path key="cl" d="M10 20a3.5 3.5 0 0 1-.3-6.98A4.6 4.6 0 0 1 18.6 13a3.5 3.5 0 0 1-.1 7z" />,
    ],
  ],
  cloud: [['text-stone-400', <path key="cl" d={CLOUD} />]],
  fog: [['text-stone-400', <path key="f" d="M4 8h16M3 12h18M5 16h14M8 20h8" />]],
  drizzle: [
    ['text-stone-400', <path key="cl" d={CLOUD_HIGH} />],
    ['text-sky-500', <path key="d" d="M8 17.5v.5M12 18.5v.5M16 17.5v.5M10 21v.5M14 21v.5" />],
  ],
  rain: [
    ['text-stone-400', <path key="cl" d={CLOUD_HIGH} />],
    ['text-sky-500', <path key="d" d="M8 17l-1 3M12 17l-1 3M16 17l-1 3" />],
  ],
  snow: [
    ['text-stone-400', <path key="cl" d={CLOUD_HIGH} />],
    ['text-sky-400', <path key="d" d="M8 18h.01M12 18h.01M16 18h.01M10 21h.01M14 21h.01" />],
  ],
  storm: [
    ['text-stone-400', <path key="cl" d={CLOUD_HIGH} />],
    ['text-amber-500', <path key="b" d="M12.5 15l-2.5 4h4l-2.5 4" />],
  ],
  unknown: [['text-stone-300', <path key="cl" d={CLOUD} />]],
}

const ICON_FOR_LABEL = {
  Clear: 'sun',
  'Mainly clear': 'partly',
  'Partly cloudy': 'partly',
  Cloudy: 'cloud',
  Fog: 'fog',
  Drizzle: 'drizzle',
  Rain: 'rain',
  Showers: 'rain',
  Snow: 'snow',
  'Snow showers': 'snow',
  Thunderstorm: 'storm',
}

function WeatherIcon({ code, className }) {
  const parts = ICON_PARTS[ICON_FOR_LABEL[conditionLabel(code)] ?? 'unknown']
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
    >
      {parts.map(([color, shape], index) => (
        <g key={index} className={color}>
          {shape}
        </g>
      ))}
    </svg>
  )
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

  const retry = () => {
    setStatus('loading')
    loadWeather().catch(() => setStatus('error'))
  }

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
    return (
      <section aria-label="Weather" aria-busy="true" className={`${cardClass} p-5 sm:p-6`}>
        <p className="sr-only">Loading weather…</p>
        <div aria-hidden="true" className="flex items-center gap-4 motion-safe:animate-pulse">
          <div className="size-14 rounded-full bg-stone-200" />
          <div className="flex flex-1 flex-col gap-2">
            <div className="h-7 w-28 rounded bg-stone-200" />
            <div className="h-4 w-44 rounded bg-stone-100" />
          </div>
        </div>
      </section>
    )
  }

  if (status === 'error') {
    return (
      <section
        aria-label="Weather"
        className={`${cardClass} flex flex-wrap items-center gap-3 p-5 sm:p-6`}
      >
        <p className="flex flex-1 items-center gap-2 text-sm text-red-700">
          <Icon name="alert" className="size-4" />
          Could not load the weather.
        </p>
        <button type="button" onClick={retry} className={buttonSecondary}>
          <Icon name="refresh" />
          Try again
        </button>
      </section>
    )
  }

  const { location, current, today } = weather

  return (
    <section aria-label="Weather" className={`${cardClass} flex flex-col gap-5 p-5 sm:p-6`}>
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-4">
        <div className="flex items-center gap-4">
          <WeatherIcon code={current?.weather_code} className="size-14 shrink-0" />
          <div className="flex flex-col">
            <p className="flex flex-wrap items-baseline gap-x-2">
              <span className="text-4xl font-semibold tracking-tight text-stone-900 tabular-nums">
                {withUnit(current?.temperature_c, '°C')}
              </span>
              <span className="text-base text-stone-600">
                {conditionLabel(current?.weather_code)}
              </span>
            </p>
            <p className="text-sm text-stone-500">
              Feels like {withUnit(current?.apparent_temperature_c, '°C')}
            </p>
          </div>
        </div>

        <div className="flex flex-col items-start gap-1 sm:items-end">
          <div className="flex items-center gap-1.5 text-stone-900">
            <Icon name="pin" className="size-4 text-stone-400" />
            <h2 className="font-semibold">{location?.name ?? '-'}</h2>
            {location?.country && (
              <span className="text-sm text-stone-500">{location.country}</span>
            )}
          </div>
          {!changing && (
            <button type="button" onClick={openChange} className={`${buttonGhost} -mx-2.5`}>
              Change location
            </button>
          )}
        </div>
      </div>

      <dl className="grid grid-cols-3 divide-x divide-stone-200 rounded-xl bg-stone-50 py-3 text-center">
        <div className="flex flex-col gap-0.5">
          <dt className="text-xs text-stone-500">Low</dt>
          <dd className="font-medium text-stone-900 tabular-nums">
            {withUnit(today?.temperature_min_c, '°C')}
          </dd>
        </div>
        <div className="flex flex-col gap-0.5">
          <dt className="text-xs text-stone-500">High</dt>
          <dd className="font-medium text-stone-900 tabular-nums">
            {withUnit(today?.temperature_max_c, '°C')}
          </dd>
        </div>
        <div className="flex flex-col gap-0.5">
          <dt className="text-xs text-stone-500">Chance of rain</dt>
          <dd className="font-medium text-stone-900 tabular-nums">
            {withUnit(today?.precipitation_probability_max, '%')}
          </dd>
        </div>
      </dl>

      {changing && (
        <div className="flex flex-col gap-3 border-t border-stone-200 pt-4">
          <form
            onSubmit={submitSearch}
            onKeyDown={(event) => {
              if (event.key === 'Escape') cancelChange()
            }}
            aria-label="Change location"
            className="flex flex-col gap-2 sm:flex-row"
          >
            <div className="relative flex-1">
              <span className="pointer-events-none absolute inset-y-0 left-3 flex items-center text-stone-400">
                <Icon name="search" />
              </span>
              <input
                type="text"
                aria-label="City"
                placeholder="Search for a city"
                autoFocus
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                className={`${inputClass} pl-9`}
              />
            </div>
            <div className="flex gap-2">
              <button
                type="submit"
                disabled={searchStatus === 'loading' || saving}
                className={`${buttonPrimary} flex-1 sm:flex-none`}
              >
                {searchStatus === 'loading' && <Spinner />}
                {searchStatus === 'loading' ? 'Searching…' : 'Search'}
              </button>
              <button
                type="button"
                onClick={cancelChange}
                className={`${buttonSecondary} flex-1 sm:flex-none`}
              >
                Cancel
              </button>
            </div>
          </form>

          {searchStatus === 'error' && (
            <p role="alert" className={errorClass}>
              <Icon name="alert" className="mt-0.5 size-4" />
              {searchError}
            </p>
          )}
          {saveError && (
            <p role="alert" className={errorClass}>
              <Icon name="alert" className="mt-0.5 size-4" />
              {saveError}
            </p>
          )}

          {searchStatus === 'done' && results.length === 0 && (
            <p className="text-sm text-stone-500">No cities found.</p>
          )}

          {searchStatus === 'done' && results.length > 0 && (
            <ul className="flex flex-col divide-y divide-stone-100 overflow-hidden rounded-xl border border-stone-200">
              {results.map((city, index) => (
                <li key={`${city.name}-${city.latitude}-${city.longitude}-${index}`}>
                  <button
                    type="button"
                    disabled={saving}
                    onClick={() => pickCity(city)}
                    className="flex w-full items-center gap-2 px-3 py-2.5 text-left text-sm text-stone-800 transition-colors hover:bg-stone-50 focus-visible:bg-stone-100 focus-visible:outline-none disabled:opacity-50"
                  >
                    <Icon name="pin" className="size-4 text-stone-400" />
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

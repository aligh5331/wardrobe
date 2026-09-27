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

export default function WeatherPanel() {
  const [weather, setWeather] = useState(null)
  const [status, setStatus] = useState('loading')

  useEffect(() => {
    let cancelled = false
    fetch('/api/weather')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.json()
      })
      .then((data) => {
        if (cancelled) return
        setWeather(data)
        setStatus('ready')
      })
      .catch(() => {
        if (cancelled) return
        setStatus('error')
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (status === 'loading') {
    return <p className="text-sm text-gray-500">Loading weather…</p>
  }

  if (status === 'error') {
    return <p className="text-sm text-red-600">Could not load the weather.</p>
  }

  const { location, current, today } = weather

  return (
    <section className="flex flex-wrap items-center gap-4 rounded-lg border border-gray-200 bg-white px-4 py-2 text-sm">
      <span className="font-semibold text-gray-900">{location?.name ?? '-'}</span>
      <span className="text-gray-800">{withUnit(current?.temperature_c, '°C')}</span>
      <span className="text-gray-500">Feels like {withUnit(current?.apparent_temperature_c, '°C')}</span>
      <span className="text-gray-500">{conditionLabel(current?.weather_code)}</span>
      <span className="text-gray-500">
        {withUnit(today?.temperature_min_c, '°C')} / {withUnit(today?.temperature_max_c, '°C')}
      </span>
      <span className="text-gray-500">{withUnit(today?.precipitation_probability_max, '%')} rain</span>
    </section>
  )
}

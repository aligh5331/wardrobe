import { useEffect, useState } from 'react'

// Read-only browse: one card per cataloged garment. No edit/delete controls.

function ItemCard({ item }) {
  const [photoFailed, setPhotoFailed] = useState(false)
  const showPhoto = Boolean(item.photo_url) && !photoFailed

  const fields = [
    ['Category', item.category],
    ['Subcategory', item.subcategory],
    ['Dominant color', item.dominant_color],
    ['Secondary colors', (item.secondary_colors ?? []).join(', ')],
    ['Pattern', item.pattern],
    ['Warmth', item.warmth_tier],
    ['Formality', item.formality],
    ['Added', item.added_date],
    ['Notes', item.notes],
  ].filter(([, value]) => value)

  return (
    <li className="flex flex-col overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm">
      {showPhoto ? (
        <img
          src={item.photo_url}
          alt={[item.dominant_color, item.subcategory].filter(Boolean).join(' ')}
          loading="lazy"
          onError={() => setPhotoFailed(true)}
          className="h-48 w-full bg-gray-100 object-cover"
        />
      ) : (
        <div
          role="img"
          aria-label="Missing photo"
          className="flex h-48 w-full items-center justify-center bg-gray-100 text-sm text-gray-400"
        >
          No photo
        </div>
      )}

      <div className="flex flex-1 flex-col gap-2 p-4">
        <h2 className="text-base font-semibold text-gray-900">
          {[item.category, item.subcategory].filter(Boolean).join(' · ')}
        </h2>
        <dl className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-1 text-sm">
          {fields.map(([label, value]) => (
            <div key={label} className="contents">
              <dt className="text-gray-500">{label}</dt>
              <dd className="text-gray-800">{value}</dd>
            </div>
          ))}
        </dl>
      </div>
    </li>
  )
}

export default function App() {
  const [items, setItems] = useState([])
  const [status, setStatus] = useState('loading')

  useEffect(() => {
    let cancelled = false
    fetch('/api/items')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.json()
      })
      .then((data) => {
        if (cancelled) return
        setItems(Array.isArray(data) ? data : [])
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

  return (
    <main className="mx-auto max-w-6xl p-6">
      <h1 className="mb-6 text-2xl font-bold text-gray-900">Wardrobe</h1>

      {status === 'loading' && <p className="text-gray-500">Loading…</p>}

      {status === 'error' && (
        <p className="text-red-600">Could not load the catalog.</p>
      )}

      {status === 'ready' && items.length === 0 && (
        <p className="text-gray-500">
          No items cataloged yet. Run the ingestion pipeline to add garments.
        </p>
      )}

      {status === 'ready' && items.length > 0 && (
        <ul className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {items.map((item) => (
            <ItemCard key={item.id} item={item} />
          ))}
        </ul>
      )}
    </main>
  )
}

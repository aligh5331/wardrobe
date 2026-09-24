import { useEffect, useState } from 'react'
import ItemAddForm from './ItemAddForm.jsx'
import ItemEditForm from './ItemEditForm.jsx'

// Grid of one card per cataloged garment, plus the ING-033 edit entry point.
// Loading the grid itself stays read-only; a write form only exists after the
// user activates edit on a card.

function ItemCard({ item, edit, onEdit, onSubmit, onCancel }) {
  const [photoFailed, setPhotoFailed] = useState(false)
  const showPhoto = Boolean(item.photo_url) && !photoFailed
  const editing = edit?.id === item.id

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
        <div className="flex items-start justify-between gap-2">
          <h2 className="text-base font-semibold text-gray-900">
            {[item.category, item.subcategory].filter(Boolean).join(' · ')}
          </h2>
          <button
            type="button"
            onClick={() => onEdit(item.id)}
            className="rounded border border-gray-300 px-2 py-1 text-sm text-gray-700 hover:bg-gray-50"
          >
            Edit
          </button>
        </div>
        <dl className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-1 text-sm">
          {fields.map(([label, value]) => (
            <div key={label} className="contents">
              <dt className="text-gray-500">{label}</dt>
              <dd className="text-gray-800">{value}</dd>
            </div>
          ))}
        </dl>

        {editing && (
          <div className="mt-2 border-t border-gray-200 pt-3">
            {edit.status === 'loading' && (
              <p className="text-sm text-gray-500">Loading…</p>
            )}
            {edit.status === 'error' && (
              <p className="text-sm text-red-600">Could not load the item.</p>
            )}
            {edit.status === 'ready' && (
              <ItemEditForm
                key={item.id}
                item={edit.item}
                taxonomy={edit.taxonomy}
                saving={edit.saving}
                error={edit.error}
                onSubmit={onSubmit}
                onCancel={onCancel}
              />
            )}
            {edit.status !== 'ready' && (
              <button
                type="button"
                onClick={onCancel}
                className="mt-2 text-sm text-gray-600 underline"
              >
                Cancel
              </button>
            )}
          </div>
        )}
      </div>
    </li>
  )
}

export default function App() {
  const [items, setItems] = useState([])
  const [status, setStatus] = useState('loading')
  // null, or { id, status: 'loading'|'ready'|'error', item, taxonomy, saving, error }.
  const [edit, setEdit] = useState(null)
  // ING-034 — the add flow (upload → draft → confirm) is open.
  const [adding, setAdding] = useState(false)

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

  // Activating edit re-reads the one item (GET /api/items/:id) and loads the
  // enum choices from GET /api/taxonomy, then opens the prefilled form.
  const openEdit = async (id) => {
    setEdit({ id, status: 'loading' })
    try {
      const [itemRes, taxRes] = await Promise.all([
        fetch(`/api/items/${id}`),
        fetch('/api/taxonomy'),
      ])
      if (!itemRes.ok || !taxRes.ok) throw new Error('failed to load')
      const [item, taxonomy] = await Promise.all([itemRes.json(), taxRes.json()])
      setEdit({ id, status: 'ready', item, taxonomy, saving: false, error: '' })
    } catch {
      setEdit({ id, status: 'error' })
    }
  }

  // Submit sends only the mutable fields; the form builds the body, so id,
  // added_date, and photo_path are never sent as changed values.
  const submitEdit = async (values) => {
    const id = edit.id
    setEdit((current) => ({ ...current, saving: true, error: '' }))

    let res
    try {
      res = await fetch(`/api/items/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(values),
      })
    } catch {
      setEdit((current) => ({
        ...current,
        saving: false,
        error: 'Could not reach the server.',
      }))
      return
    }

    if (res.ok) {
      const updated = await res.json()
      setItems((list) => list.map((item) => (item.id === updated.id ? updated : item)))
      setEdit(null)
      return
    }

    // Surface the server's named-field 400 (or any other body) instead of
    // swallowing it, and keep the form open so the user can correct it.
    let message = `Could not save (HTTP ${res.status}).`
    try {
      const body = await res.json()
      if (body?.error) message = body.error
    } catch {
      // Non-JSON error body: keep the status message.
    }
    setEdit((current) => ({ ...current, saving: false, error: message }))
  }

  const closeEdit = () => setEdit(null)

  // A confirmed draft was persisted: show the created item in the grid
  // (07-architecture.md "Catalog write API": POST /api/items returns the
  // created row in the same shape the grid renders).
  const saveNew = (created) => {
    setItems((list) => [...list, created])
    setStatus('ready')
    setAdding(false)
  }

  return (
    <main className="mx-auto max-w-6xl p-6">
      <div className="mb-6 flex items-center justify-between gap-4">
        <h1 className="text-2xl font-bold text-gray-900">Wardrobe</h1>
        <button
          type="button"
          onClick={() => setAdding((open) => !open)}
          className="rounded bg-gray-900 px-3 py-1 text-sm text-white"
        >
          Add garment
        </button>
      </div>

      {adding && <ItemAddForm onSaved={saveNew} onCancel={() => setAdding(false)} />}

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
            <ItemCard
              key={item.id}
              item={item}
              edit={edit}
              onEdit={openEdit}
              onSubmit={submitEdit}
              onCancel={closeEdit}
            />
          ))}
        </ul>
      )}
    </main>
  )
}

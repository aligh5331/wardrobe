import { useCallback, useEffect, useId, useState } from 'react'
import ItemAddForm from './ItemAddForm.jsx'
import ItemEditForm from './ItemEditForm.jsx'
import WeatherPanel from './WeatherPanel.jsx'
import RecommendationPanel from './RecommendationPanel.jsx'
import {
  GarmentPhoto,
  Icon,
  Modal,
  Spinner,
  Swatch,
  buttonPrimary,
  buttonSecondary,
  cardClass,
} from './ui.jsx'

// Grid of one card per cataloged garment, plus the ING-033 edit entry point.
// Loading the grid itself stays read-only; a write form only exists after the
// user activates edit on a card. Add and edit open in a dialog; the page behind
// it is inert until the dialog closes.

function ItemCard({ item, highlighted, onEdit }) {
  const headingId = useId()
  const title = [item.category, item.subcategory].filter(Boolean).join(' · ')
  const secondary = item.secondary_colors ?? []
  const attributes = [
    ['Pattern', item.pattern],
    ['Warmth', item.warmth_tier],
    ['Formality', item.formality],
  ].filter(([, value]) => value)

  return (
    // Phones get a compact row (thumbnail beside the details); wider screens get
    // a photo-first card.
    <li
      id={`item-${item.id}`}
      className={`flex scroll-mt-24 overflow-hidden rounded-2xl border bg-white shadow-sm transition-shadow hover:shadow-md sm:flex-col ${
        highlighted ? 'border-amber-400 ring-4 ring-amber-200' : 'border-stone-200'
      }`}
    >
      <div className="relative w-32 shrink-0 sm:w-full">
        <GarmentPhoto
          item={item}
          className="h-full min-h-44 w-full sm:aspect-[4/5] sm:h-auto sm:min-h-0"
        />
        <button
          type="button"
          onClick={() => onEdit(item.id)}
          aria-describedby={headingId}
          aria-haspopup="dialog"
          className="absolute top-2 right-2 inline-flex items-center gap-1.5 rounded-full bg-white/90 p-2 text-sm font-medium text-stone-800 shadow-sm ring-1 ring-stone-900/5 backdrop-blur transition-colors hover:bg-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-stone-900 sm:top-3 sm:right-3 sm:px-3 sm:py-1.5"
        >
          <Icon name="pencil" className="size-3.5" />
          <span className="sr-only sm:not-sr-only">Edit</span>
        </button>
      </div>

      <div className="flex min-w-0 flex-1 flex-col gap-3 p-4">
        {/* Category and subcategory sit on separate lines; the label keeps the
            name a screen reader hears as one phrase ("top · shirt"). */}
        <h3 id={headingId} aria-label={title} className="flex flex-col">
          <span className="text-xs font-medium tracking-wide text-stone-500 uppercase">
            {item.category}
          </span>
          <span className="text-base font-semibold text-stone-900 capitalize">
            {item.subcategory}
          </span>
        </h3>

        {(item.dominant_color || secondary.length > 0) && (
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-stone-700">
            {item.dominant_color && (
              <span className="inline-flex items-center gap-1.5" title="Dominant color">
                <Swatch color={item.dominant_color} className="size-4" />
                <span>{item.dominant_color}</span>
              </span>
            )}
            {secondary.map((color) => (
              <span
                key={color}
                className="inline-flex items-center gap-1.5 text-stone-500"
                title="Secondary color"
              >
                <Swatch color={color} className="size-3" />
                <span>{color}</span>
              </span>
            ))}
          </div>
        )}

        {attributes.length > 0 && (
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 border-t border-stone-100 pt-3 text-sm">
            {attributes.map(([label, value]) => (
              <div key={label} className="contents">
                <dt className="text-stone-500">{label}</dt>
                <dd className="font-medium break-words text-stone-800">{value}</dd>
              </div>
            ))}
          </dl>
        )}

        {item.notes && <p className="text-sm text-stone-600 italic">{item.notes}</p>}

        {item.added_date && (
          <p className="mt-auto text-xs text-stone-400">
            Added <time dateTime={item.added_date}>{item.added_date}</time>
          </p>
        )}
      </div>
    </li>
  )
}

function SkeletonGrid() {
  return (
    <div
      aria-hidden="true"
      className="grid grid-cols-1 gap-5 motion-safe:animate-pulse sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4"
    >
      {Array.from({ length: 4 }, (_, index) => (
        <div key={index} className={`${cardClass} overflow-hidden`}>
          <div className="aspect-[4/5] bg-stone-200" />
          <div className="flex flex-col gap-2 p-4">
            <div className="h-3 w-12 rounded bg-stone-200" />
            <div className="h-4 w-24 rounded bg-stone-200" />
            <div className="h-10 rounded-xl bg-stone-100" />
          </div>
        </div>
      ))}
    </div>
  )
}

export default function App() {
  const [items, setItems] = useState([])
  const [status, setStatus] = useState('loading')
  // null, or { id, status: 'loading'|'ready'|'error', item, taxonomy, saving, error }.
  const [edit, setEdit] = useState(null)
  // ING-034 — the add flow (upload → draft → confirm) is open.
  const [adding, setAdding] = useState(false)

  // A short confirmation after a save, announced politely to screen readers.
  const [toast, setToast] = useState('')
  // The just-created item, briefly outlined and scrolled into view.
  const [highlightId, setHighlightId] = useState(null)

  const loadItems = () =>
    fetch('/api/items').then((res) => {
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      return res.json()
    })

  useEffect(() => {
    let cancelled = false
    loadItems()
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

  const retryItems = () => {
    setStatus('loading')
    loadItems()
      .then((data) => {
        setItems(Array.isArray(data) ? data : [])
        setStatus('ready')
      })
      .catch(() => setStatus('error'))
  }

  useEffect(() => {
    if (!toast) return undefined
    const timer = setTimeout(() => setToast(''), 3000)
    return () => clearTimeout(timer)
  }, [toast])

  useEffect(() => {
    if (!highlightId) return undefined
    document
      .getElementById(`item-${highlightId}`)
      ?.scrollIntoView?.({ behavior: 'smooth', block: 'center' })
    const timer = setTimeout(() => setHighlightId(null), 2500)
    return () => clearTimeout(timer)
  }, [highlightId])

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
      // Only if the dialog is still open for this item: closing it while the
      // load was in flight must not pop it back open.
      setEdit((current) =>
        current?.id === id
          ? { id, status: 'ready', item, taxonomy, saving: false, error: '' }
          : current,
      )
    } catch {
      setEdit((current) => (current?.id === id ? { id, status: 'error' } : current))
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
      setToast('Changes saved')
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

  const closeEdit = useCallback(() => setEdit(null), [])
  const closeAdd = useCallback(() => setAdding(false), [])

  // A confirmed draft was persisted: show the created item in the grid
  // (07-architecture.md "Catalog write API": POST /api/items returns the
  // created row in the same shape the grid renders).
  const saveNew = (created) => {
    setItems((list) => [...list, created])
    setStatus('ready')
    setAdding(false)
    setHighlightId(created.id)
    setToast('Garment added')
  }

  const modalOpen = adding || edit !== null
  const today = new Date().toLocaleDateString(undefined, {
    weekday: 'long',
    month: 'long',
    day: 'numeric',
  })

  return (
    <div className="min-h-screen text-stone-900">
      <div inert={modalOpen}>
        <header className="sticky top-0 z-30 border-b border-stone-200 bg-white/85 backdrop-blur">
          <div className="mx-auto flex max-w-6xl items-center justify-between gap-4 px-4 py-3 sm:px-6">
            <div className="flex items-center gap-3">
              <span className="flex size-9 items-center justify-center rounded-xl bg-stone-900 text-white">
                <Icon name="hanger" className="size-5" />
              </span>
              <h1 className="text-lg font-semibold tracking-tight text-stone-900">Wardrobe</h1>
            </div>
            <button
              type="button"
              onClick={() => setAdding(true)}
              aria-haspopup="dialog"
              className={buttonPrimary}
            >
              <Icon name="plus" />
              Add garment
            </button>
          </div>
        </header>

        <main className="mx-auto flex max-w-6xl flex-col gap-10 px-4 py-6 sm:px-6 sm:py-8">
          <section aria-labelledby="today-heading" className="flex flex-col gap-4">
            <div className="flex items-baseline justify-between gap-4">
              <h2 id="today-heading" className="text-xl font-semibold tracking-tight">
                Today
              </h2>
              <p className="text-sm text-stone-500">{today}</p>
            </div>
            <WeatherPanel />
            <RecommendationPanel />
          </section>

          <section aria-labelledby="wardrobe-heading" className="flex flex-col gap-4">
            <div className="flex items-baseline justify-between gap-4">
              <h2 id="wardrobe-heading" className="text-xl font-semibold tracking-tight">
                Your wardrobe
              </h2>
              {status === 'ready' && items.length > 0 && (
                <p className="text-sm text-stone-500">
                  {items.length} {items.length === 1 ? 'item' : 'items'}
                </p>
              )}
            </div>

            {status === 'loading' && (
              <>
                <p className="sr-only">Loading…</p>
                <SkeletonGrid />
              </>
            )}

            {status === 'error' && (
              <div className={`${cardClass} flex flex-wrap items-center gap-3 p-5`}>
                <p className="flex flex-1 items-center gap-2 text-sm text-red-700">
                  <Icon name="alert" className="size-4" />
                  Could not load the catalog.
                </p>
                <button type="button" onClick={retryItems} className={buttonSecondary}>
                  <Icon name="refresh" />
                  Try again
                </button>
              </div>
            )}

            {status === 'ready' && items.length === 0 && (
              <div className="flex flex-col items-center gap-3 rounded-2xl border-2 border-dashed border-stone-300 bg-white/50 px-6 py-14 text-center">
                <span className="flex size-12 items-center justify-center rounded-full bg-stone-100 text-stone-500">
                  <Icon name="hanger" className="size-6" />
                </span>
                <p className="max-w-sm text-sm text-stone-600">
                  No items cataloged yet. Run the ingestion pipeline to add garments.
                </p>
                <button type="button" onClick={() => setAdding(true)} className={buttonSecondary}>
                  <Icon name="upload" />
                  Add one from a photo
                </button>
              </div>
            )}

            {status === 'ready' && items.length > 0 && (
              <ul className="grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
                {items.map((item) => (
                  <ItemCard
                    key={item.id}
                    item={item}
                    highlighted={item.id === highlightId}
                    onEdit={openEdit}
                  />
                ))}
              </ul>
            )}
          </section>
        </main>
      </div>

      {adding && <ItemAddForm onSaved={saveNew} onCancel={closeAdd} />}

      {edit && (
        <Modal title="Edit garment" onClose={closeEdit} dismissible={!edit.saving}>
          {edit.status === 'loading' && (
            <p className="flex items-center gap-2 py-8 text-sm text-stone-500">
              <Spinner />
              Loading…
            </p>
          )}
          {edit.status === 'error' && (
            <div className="flex flex-col items-start gap-3">
              <p className="text-sm text-red-700">Could not load the item.</p>
              <button type="button" onClick={closeEdit} className={buttonSecondary}>
                Cancel
              </button>
            </div>
          )}
          {edit.status === 'ready' && (
            <ItemEditForm
              key={edit.id}
              item={edit.item}
              taxonomy={edit.taxonomy}
              saving={edit.saving}
              error={edit.error}
              onSubmit={submitEdit}
              onCancel={closeEdit}
            />
          )}
        </Modal>
      )}

      <div
        aria-live="polite"
        className="pointer-events-none fixed inset-x-0 bottom-6 z-50 flex justify-center px-4"
      >
        {toast && (
          <p className="flex items-center gap-2 rounded-full bg-stone-900 px-4 py-2 text-sm font-medium text-white shadow-lg">
            <Icon name="check" className="size-4 text-emerald-400" />
            {toast}
          </p>
        )}
      </div>
    </div>
  )
}

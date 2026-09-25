import { useEffect, useRef, useState } from 'react'
import TaggingFields from './TaggingFields.jsx'

// ING-034 — add a garment: choose one photo, POST it to the local tagging
// route for a non-persisted draft, correct the draft, then persist it.
//
// Flow (06-decisions.md "Interactive catalog create/edit UI"):
//   1. file input (.jpg/.jpeg/.png/.webp) — the accepted set is the upload
//      trust boundary (06-decisions.md "Photo upload trust boundary").
//   2. POST multipart "photo" to /api/items/photo; the local VLM tags it
//      (05-vlm-tagging-spec.md "Interactive tagging (web UI)"), retry-once
//      policy included server-side.
//   3. the returned draft pre-fills the shared tagging fields; nothing is
//      persisted yet.
//   4. Save POSTs /api/items with the draft identity + corrected fields; only
//      then does the server move the photo and write the row.
//
// The enum choices come from GET /api/taxonomy, never a bundled copy
// (06-decisions.md "Taxonomy exported to the browser via GET /api/taxonomy").

const ACCEPT = '.jpg,.jpeg,.png,.webp'

// Pull the server's named-field error out of a non-OK body, or fall back.
async function errorMessage(res, fallback) {
  try {
    const body = await res.json()
    if (body?.error) return body.error
  } catch {
    // Non-JSON error body: keep the fallback.
  }
  return fallback
}

const emptyValues = (draft) => ({
  category: draft.category ?? '',
  subcategory: draft.subcategory ?? '',
  dominant_color: draft.dominant_color ?? '',
  secondary_colors: draft.secondary_colors ?? [],
  pattern: draft.pattern ?? '',
  warmth_tier: draft.warmth_tier ?? '',
  formality: draft.formality ?? '',
  notes: draft.notes ?? '',
})

export default function ItemAddForm({ onSaved, onCancel }) {
  const [taxonomy, setTaxonomy] = useState(null)
  const [loadError, setLoadError] = useState('')
  const [file, setFile] = useState(null)
  const [previewUrl, setPreviewUrl] = useState('')
  const [phase, setPhase] = useState('choose') // choose | tagging | draft | saving
  const [values, setValues] = useState(null)
  const [identity, setIdentity] = useState(null) // { item_id, photo_ref }
  const [error, setError] = useState('')
  const previewRef = useRef('')

  // Field choices come from the server route, not a bundled copy.
  useEffect(() => {
    let cancelled = false
    fetch('/api/taxonomy')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.json()
      })
      .then((data) => {
        if (!cancelled) setTaxonomy(data)
      })
      .catch(() => {
        if (!cancelled) setLoadError('Could not load the field choices.')
      })
    return () => {
      cancelled = true
    }
  }, [])

  // Keep one object URL for the chosen file and release it on change/unmount.
  const chooseFile = (chosen) => {
    if (previewRef.current) URL.revokeObjectURL(previewRef.current)
    previewRef.current = chosen ? URL.createObjectURL(chosen) : ''
    setPreviewUrl(previewRef.current)
    setFile(chosen)
    setError('')
  }
  useEffect(
    () => () => {
      if (previewRef.current) URL.revokeObjectURL(previewRef.current)
    },
    [],
  )

  const upload = async (event) => {
    event.preventDefault()
    if (!file) return
    setPhase('tagging')
    setError('')

    const form = new FormData()
    form.append('photo', file)

    let res
    try {
      res = await fetch('/api/items/photo', { method: 'POST', body: form })
    } catch {
      setPhase('choose')
      setError('Could not reach the server. Retry or choose another photo.')
      return
    }

    if (!res.ok) {
      // Tagging failed after the server's retry-once policy, or the upload was
      // rejected. Nothing was persisted; let the user retry or pick another.
      setPhase('choose')
      setError(await errorMessage(res, 'Tagging failed. Retry or choose another photo.'))
      return
    }

    const draft = await res.json()
    setIdentity({ item_id: draft.item_id, photo_ref: draft.photo_ref })
    setValues(emptyValues(draft))
    setPhase('draft')
  }

  const save = async (event) => {
    event.preventDefault()
    setPhase('saving')
    setError('')

    let res
    try {
      res = await fetch('/api/items', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          item_id: identity.item_id,
          photo_ref: identity.photo_ref,
          category: values.category,
          subcategory: values.subcategory,
          dominant_color: values.dominant_color,
          secondary_colors: values.secondary_colors,
          pattern: values.pattern,
          warmth_tier: values.warmth_tier,
          formality: values.formality,
          notes: values.notes,
        }),
      })
    } catch {
      setPhase('draft')
      setError('Could not reach the server.')
      return
    }

    if (res.ok) {
      onSaved(await res.json())
      return
    }

    // Surface the server's named-field 400 (or any other body) instead of
    // swallowing it, and keep the draft open so the user can correct it.
    setPhase('draft')
    setError(await errorMessage(res, `Could not save (HTTP ${res.status}).`))
  }

  const set = (name, value) => setValues((current) => ({ ...current, [name]: value }))

  return (
    <section className="mb-6 rounded-lg border border-gray-200 bg-white p-4">
      <h2 className="mb-3 text-lg font-semibold text-gray-900">Add a garment</h2>

      {loadError && (
        <p role="alert" className="text-sm text-red-600">
          {loadError}
        </p>
      )}

      {(phase === 'choose' || phase === 'tagging') && (
        <form
          onSubmit={upload}
          aria-label="Upload garment photo"
          className="flex flex-col gap-3"
        >
          <div className="flex flex-col gap-1">
            <label htmlFor="add-photo" className="text-sm text-gray-600">
              Photo
            </label>
            <input
              id="add-photo"
              type="file"
              accept={ACCEPT}
              onChange={(event) => chooseFile(event.target.files?.[0] ?? null)}
              className="text-sm"
            />
          </div>

          {previewUrl && (
            <img
              src={previewUrl}
              alt="Selected photo"
              className="h-32 w-32 rounded object-cover"
            />
          )}

          {phase === 'tagging' && (
            <p role="status" className="text-sm text-gray-500">
              Tagging with the local model…
            </p>
          )}

          {error && (
            <p role="alert" className="text-sm text-red-600">
              {error}
            </p>
          )}

          <div className="flex gap-2">
            <button
              type="submit"
              disabled={!file || phase === 'tagging'}
              className="rounded bg-gray-900 px-3 py-1 text-sm text-white disabled:opacity-50"
            >
              {phase === 'tagging' ? 'Tagging…' : 'Upload & tag'}
            </button>
            <button
              type="button"
              onClick={onCancel}
              className="rounded border border-gray-300 px-3 py-1 text-sm"
            >
              Cancel
            </button>
          </div>
        </form>
      )}

      {(phase === 'draft' || phase === 'saving') && values && (
        <form
          onSubmit={save}
          aria-label="Confirm garment"
          className="flex flex-col gap-3"
        >
          {previewUrl && (
            <img
              src={previewUrl}
              alt="Uploaded photo"
              className="h-32 w-32 rounded object-cover"
            />
          )}
          <p className="text-sm text-gray-600">
            Draft for <span className="text-gray-900">{identity.item_id}</span> — nothing is
            saved until you confirm.
          </p>

          <TaggingFields
            values={values}
            taxonomy={taxonomy ?? {}}
            onChange={set}
            idPrefix="add"
          />

          {error && (
            <p role="alert" className="text-sm text-red-600">
              {error}
            </p>
          )}

          <div className="flex gap-2">
            <button
              type="submit"
              disabled={phase === 'saving'}
              className="rounded bg-gray-900 px-3 py-1 text-sm text-white disabled:opacity-50"
            >
              {phase === 'saving' ? 'Saving…' : 'Save'}
            </button>
            <button
              type="button"
              onClick={onCancel}
              className="rounded border border-gray-300 px-3 py-1 text-sm"
            >
              Cancel
            </button>
          </div>
        </form>
      )}
    </section>
  )
}

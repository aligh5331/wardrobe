import { useEffect, useRef, useState } from 'react'
import TaggingFields from './TaggingFields.jsx'
import {
  Icon,
  Modal,
  Spinner,
  buttonPrimary,
  buttonSecondary,
  errorClass,
  formatElapsed,
  labelClass,
  useElapsedSeconds,
} from './ui.jsx'

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
//
// The flow lives in its own dialog. A photo can be picked or dropped onto the
// photo area; a dropped file skips the picker's extension filter, so the same
// ACCEPT list is checked here too (the server stays the trust boundary).

const ACCEPT = '.jpg,.jpeg,.png,.webp'
const ACCEPTED_EXTENSIONS = ACCEPT.split(',')
const isAccepted = (chosen) =>
  ACCEPTED_EXTENSIONS.some((ext) => chosen.name.toLowerCase().endsWith(ext))

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

// Not a list: the page's only list items are catalog cards and outfit rows.
function Steps({ current }) {
  const steps = ['Photo', 'Review tags']
  return (
    <div className="mb-5 flex items-center gap-2 text-sm">
      <p className="sr-only">
        Step {current} of {steps.length}: {steps[current - 1]}
      </p>
      {steps.map((label, index) => {
        const step = index + 1
        const active = step === current
        const done = step < current
        return (
          <div key={label} aria-hidden="true" className="flex items-center gap-2">
            {index > 0 && <span className="h-px w-6 bg-stone-300" />}
            <span
              className={`flex size-6 items-center justify-center rounded-full text-xs font-semibold ${
                active || done ? 'bg-stone-900 text-white' : 'bg-stone-200 text-stone-500'
              }`}
            >
              {done ? <Icon name="check" className="size-3.5" /> : step}
            </span>
            <span className={active ? 'font-medium text-stone-900' : 'text-stone-500'}>
              {label}
            </span>
          </div>
        )
      })}
    </div>
  )
}

export default function ItemAddForm({ onSaved, onCancel }) {
  const [taxonomy, setTaxonomy] = useState(null)
  const [loadError, setLoadError] = useState('')
  const [file, setFile] = useState(null)
  const [previewUrl, setPreviewUrl] = useState('')
  const [phase, setPhase] = useState('choose') // choose | tagging | draft | saving
  const [values, setValues] = useState(null)
  const [identity, setIdentity] = useState(null) // { item_id, photo_ref }
  const [error, setError] = useState('')
  const [dragging, setDragging] = useState(false)
  const previewRef = useRef('')
  const inputRef = useRef(null)
  const elapsed = useElapsedSeconds(phase === 'tagging')

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
    if (chosen && !isAccepted(chosen)) {
      setError('Choose a .jpg, .jpeg, .png, or .webp photo.')
      return
    }
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

  const dropFile = (event) => {
    event.preventDefault()
    setDragging(false)
    if (phase !== 'choose') return
    const dropped = event.dataTransfer?.files?.[0]
    if (!dropped) return
    // The input still holds any earlier pick; clear it so picking that same
    // file again later still fires onChange.
    if (inputRef.current) inputRef.current.value = ''
    chooseFile(dropped)
  }

  const choosing = phase === 'choose' || phase === 'tagging'

  return (
    <Modal
      title="Add a garment"
      description="One garment per photo, laid flat or on a hanger."
      onClose={onCancel}
      dismissible={phase !== 'saving'}
    >
      <Steps current={choosing ? 1 : 2} />

      {loadError && (
        <p role="alert" className={`${errorClass} mb-4`}>
          <Icon name="alert" className="mt-0.5 size-4" />
          {loadError}
        </p>
      )}

      {choosing && (
        <form onSubmit={upload} aria-label="Upload garment photo" className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="add-photo" className={labelClass}>
              Photo
            </label>
            <div className="relative">
              <label
                onDragOver={(event) => {
                  event.preventDefault()
                  if (phase === 'choose') setDragging(true)
                }}
                onDragLeave={() => setDragging(false)}
                onDrop={dropFile}
                className={`flex min-h-56 cursor-pointer flex-col items-center justify-center gap-3 overflow-hidden rounded-xl border-2 border-dashed p-6 text-center transition-colors has-focus-visible:outline-2 has-focus-visible:outline-offset-2 has-focus-visible:outline-stone-900 ${
                  dragging
                    ? 'border-stone-900 bg-stone-100'
                    : 'border-stone-300 bg-stone-50 hover:border-stone-400 hover:bg-stone-100'
                }`}
              >
                {previewUrl ? (
                  <img
                    src={previewUrl}
                    alt="Selected photo"
                    className="h-44 w-auto max-w-full rounded-lg object-contain shadow-sm"
                  />
                ) : (
                  <span className="flex size-12 items-center justify-center rounded-full bg-white text-stone-500 shadow-sm ring-1 ring-stone-200">
                    <Icon name="upload" className="size-6" />
                  </span>
                )}
                <span className="flex flex-col gap-0.5">
                  <span className="text-sm font-medium text-stone-800">
                    {file ? file.name : 'Drop a photo here, or click to choose'}
                  </span>
                  <span className="text-xs text-stone-500">
                    {file ? 'Click or drop to pick a different photo' : 'JPG, PNG, or WebP'}
                  </span>
                </span>
                <input
                  ref={inputRef}
                  id="add-photo"
                  type="file"
                  accept={ACCEPT}
                  data-autofocus
                  disabled={phase === 'tagging'}
                  onChange={(event) => chooseFile(event.target.files?.[0] ?? null)}
                  className="sr-only"
                />
              </label>
              {phase === 'tagging' && (
                <div
                  role="status"
                  className="absolute inset-0 flex flex-col items-center justify-center gap-2 rounded-xl bg-white/85 text-sm text-stone-700 backdrop-blur-sm"
                >
                  <Spinner className="size-6 text-stone-900" />
                  Tagging with the local model…
                  <span aria-hidden="true" className="text-xs text-stone-500 tabular-nums">
                    {formatElapsed(elapsed)}
                  </span>
                </div>
              )}
            </div>
          </div>

          {error && (
            <p role="alert" className={errorClass}>
              <Icon name="alert" className="mt-0.5 size-4" />
              {error}
            </p>
          )}

          <div className="flex flex-col-reverse gap-2 border-t border-stone-200 pt-4 sm:flex-row sm:justify-end">
            <button type="button" onClick={onCancel} className={buttonSecondary}>
              Cancel
            </button>
            <button type="submit" disabled={!file || phase === 'tagging'} className={buttonPrimary}>
              {phase === 'tagging' ? <Spinner /> : <Icon name="sparkles" />}
              {phase === 'tagging' ? 'Tagging…' : 'Upload & tag'}
            </button>
          </div>
        </form>
      )}

      {(phase === 'draft' || phase === 'saving') && values && (
        <form
          onSubmit={save}
          aria-label="Confirm garment"
          className="grid gap-6 sm:grid-cols-[minmax(0,200px)_1fr]"
        >
          <div className="flex items-start gap-4 sm:flex-col sm:gap-3">
            {previewUrl && (
              <img
                src={previewUrl}
                alt="Uploaded photo"
                className="aspect-[4/5] w-24 shrink-0 rounded-xl bg-stone-100 object-cover sm:w-full sm:max-w-60"
              />
            )}
            <p className="text-xs text-stone-500">Nothing is saved until you click Save.</p>
          </div>

          <div className="flex flex-col gap-5">
            <p className="flex items-start gap-2 rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-900">
              <Icon name="sparkles" className="mt-0.5 size-4" />
              The local model filled these in. Check them and fix anything it got wrong.
            </p>

            <div className="grid gap-4 sm:grid-cols-2">
              <TaggingFields
                values={values}
                taxonomy={taxonomy ?? {}}
                onChange={set}
                idPrefix="add"
              />
            </div>

            {error && (
              <p role="alert" className={errorClass}>
                <Icon name="alert" className="mt-0.5 size-4" />
                {error}
              </p>
            )}

            <div className="flex flex-col-reverse gap-2 border-t border-stone-200 pt-4 sm:flex-row sm:justify-end">
              <button
                type="button"
                onClick={onCancel}
                disabled={phase === 'saving'}
                className={buttonSecondary}
              >
                Cancel
              </button>
              <button type="submit" disabled={phase === 'saving'} className={buttonPrimary}>
                {phase === 'saving' && <Spinner />}
                {phase === 'saving' ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        </form>
      )}
    </Modal>
  )
}

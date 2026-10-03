import { useEffect, useState } from 'react'
import {
  GarmentPhoto,
  Icon,
  Spinner,
  buttonPrimary,
  cardClass,
  errorClass,
  formatElapsed,
  inputClass,
  labelClass,
  useElapsedSeconds,
} from './ui.jsx'

// ING-050 - "Suggest outfits" panel (07-architecture.md "Recommender (Phase 3)").
// It keeps its own state, so a failure here never touches the weather panel or
// the catalog. The LLM is only called on click, never on load.

const GENERIC_ERROR = 'Could not get recommendations.'

// The note's limit from 07-architecture.md "Recommender (Phase 3)"; capping the
// input here means the server's 400 for a longer note cannot be hit by typing.
const NOTE_MAX = 500

export default function RecommendationPanel() {
  const [formalities, setFormalities] = useState([])
  const [formality, setFormality] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [outfits, setOutfits] = useState([])
  const elapsed = useElapsedSeconds(busy)

  // Choices come from the server taxonomy, not a bundled copy. If the fetch
  // fails or returns an unexpected shape, the select keeps only "any".
  useEffect(() => {
    let cancelled = false
    fetch('/api/taxonomy')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.json()
      })
      .then((data) => {
        if (!cancelled && Array.isArray(data?.formality)) setFormalities(data.formality)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  const suggest = async () => {
    setBusy(true)
    setError('')
    setOutfits([])

    const body = {}
    if (formality) body.formality = formality
    if (note.trim()) body.note = note.trim()

    // ponytail: no AbortController. The server can take up to two 120 s LLM
    // calls, so the browser waits as long as the server does.
    let res
    try {
      res = await fetch('/api/recommendations', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
    } catch {
      setError('Could not reach the server.')
      setBusy(false)
      return
    }

    let data = null
    try {
      data = await res.json()
    } catch {
      // Non-JSON body: fall through to the generic messages below.
    }

    if (!res.ok) {
      setError(data?.error || `Could not get recommendations (HTTP ${res.status}).`)
    } else if (!Array.isArray(data?.outfits)) {
      setError(GENERIC_ERROR)
    } else {
      setOutfits(data.outfits)
    }
    setBusy(false)
  }

  // Enter in the note sends the request, like a search box. The panel is not a
  // <form>, so the page still loads with no write form (ING-020/ING-033).
  const submitOnEnter = (event) => {
    if (event.key === 'Enter' && !event.nativeEvent.isComposing && !busy) {
      event.preventDefault()
      suggest()
    }
  }

  return (
    <section aria-labelledby="rec-heading" className={`${cardClass} p-5 sm:p-6`}>
      <div className="mb-5 flex items-start gap-3">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-amber-100 text-amber-700">
          <Icon name="sparkles" className="size-5" />
        </span>
        <div>
          <h2 id="rec-heading" className="text-base font-semibold text-stone-900">
            Outfit ideas
          </h2>
          <p className="text-sm text-stone-500">
            Three outfits for today’s weather, picked from your wardrobe by the local model.
          </p>
        </div>
      </div>

      <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
        <div className="flex flex-col gap-1.5 sm:w-44">
          <label htmlFor="rec-formality" className={labelClass}>
            Formality
          </label>
          <div className="relative">
            <select
              id="rec-formality"
              value={formality}
              onChange={(event) => setFormality(event.target.value)}
              className={`${inputClass} appearance-none pr-9`}
            >
              <option value="">any</option>
              {formalities.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
            <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-stone-400">
              <Icon name="chevron" />
            </span>
          </div>
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          <label htmlFor="rec-note" className={labelClass}>
            Note
          </label>
          <input
            id="rec-note"
            type="text"
            value={note}
            maxLength={NOTE_MAX}
            placeholder="Optional — e.g. dinner with friends, walking there"
            onChange={(event) => setNote(event.target.value)}
            onKeyDown={submitOnEnter}
            className={inputClass}
          />
        </div>
        <button type="button" onClick={suggest} disabled={busy} className={buttonPrimary}>
          {busy ? <Spinner /> : <Icon name="sparkles" />}
          Suggest outfits
        </button>
      </div>

      {busy && (
        <div
          role="status"
          className="mt-5 flex items-center gap-3 rounded-xl bg-stone-50 px-4 py-3 text-sm text-stone-600"
        >
          <Spinner className="size-5 text-stone-900" />
          <span className="flex-1">Finding outfits… this can take a couple of minutes.</span>
          <span aria-hidden="true" className="text-xs text-stone-400 tabular-nums">
            {formatElapsed(elapsed)}
          </span>
        </div>
      )}
      {error && (
        <p role="alert" className={`${errorClass} mt-5`}>
          <Icon name="alert" className="mt-0.5 size-4" />
          {error}
        </p>
      )}

      {!busy && !error && outfits.length === 0 && (
        <p className="mt-5 rounded-xl border border-dashed border-stone-300 px-4 py-6 text-center text-sm text-stone-500">
          Pick a formality, add a note if you like, then ask for suggestions.
        </p>
      )}

      {outfits.length > 0 && (
        <ul className="mt-5 grid gap-4 md:grid-cols-3">
          {outfits.map((outfit, index) => (
            <li
              key={index}
              className="flex flex-col gap-3 rounded-xl border border-stone-200 bg-stone-50/60 p-3"
            >
              <p className="text-xs font-semibold tracking-wide text-stone-500 uppercase">
                Outfit {index + 1}
              </p>
              <div className="grid grid-cols-3 gap-2">
                {(outfit.items ?? []).map((item) => (
                  <GarmentPhoto
                    key={item.id}
                    item={item}
                    className="aspect-[3/4] w-full rounded-lg"
                  />
                ))}
              </div>
              <p className="text-sm leading-relaxed text-stone-700">{outfit.reason}</p>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

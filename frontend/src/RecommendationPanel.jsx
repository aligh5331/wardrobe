import { useEffect, useState } from 'react'

// ING-050 - "Suggest outfits" panel (07-architecture.md "Recommender (Phase 3)").
// It keeps its own state, so a failure here never touches the weather panel or
// the catalog. The LLM is only called on click, never on load.

const GENERIC_ERROR = 'Could not get recommendations.'

// Same markup as the catalog grid's missing-photo placeholder in App.jsx.
function Photo({ item }) {
  const [failed, setFailed] = useState(false)
  if (item.photo_url && !failed) {
    return (
      <img
        src={item.photo_url}
        alt={[item.dominant_color, item.subcategory].filter(Boolean).join(' ')}
        onError={() => setFailed(true)}
        className="h-32 w-24 shrink-0 rounded bg-gray-100 object-cover"
      />
    )
  }
  return (
    <div
      role="img"
      aria-label="Missing photo"
      className="flex h-32 w-24 shrink-0 items-center justify-center rounded bg-gray-100 text-sm text-gray-400"
    >
      No photo
    </div>
  )
}

export default function RecommendationPanel() {
  const [formalities, setFormalities] = useState([])
  const [formality, setFormality] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [outfits, setOutfits] = useState([])

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

  return (
    <section className="flex flex-col gap-3 rounded-lg border border-gray-200 bg-white px-4 py-3 text-sm">
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex flex-col gap-1">
          <label htmlFor="rec-formality" className="text-sm text-gray-600">
            Formality
          </label>
          <select
            id="rec-formality"
            value={formality}
            onChange={(event) => setFormality(event.target.value)}
            className="rounded border border-gray-300 px-2 py-1 text-sm"
          >
            <option value="">any</option>
            {formalities.map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </select>
        </div>
        <div className="flex min-w-48 flex-1 flex-col gap-1">
          <label htmlFor="rec-note" className="text-sm text-gray-600">
            Note
          </label>
          <input
            id="rec-note"
            type="text"
            value={note}
            onChange={(event) => setNote(event.target.value)}
            className="rounded border border-gray-300 px-2 py-1 text-sm"
          />
        </div>
        <button
          type="button"
          onClick={suggest}
          disabled={busy}
          className="rounded bg-gray-900 px-3 py-1 text-sm text-white disabled:opacity-50"
        >
          Suggest outfits
        </button>
      </div>

      {busy && (
        <p role="status" className="text-sm text-gray-500">
          Finding outfits… this can take a couple of minutes.
        </p>
      )}
      {error && (
        <p role="alert" className="text-sm text-red-600">
          {error}
        </p>
      )}

      {outfits.length > 0 && (
        <ul className="flex flex-col gap-3">
          {outfits.map((outfit, index) => (
            <li key={index} className="flex flex-col gap-2 border-t border-gray-200 pt-3">
              <div className="flex flex-wrap gap-2">
                {(outfit.items ?? []).map((item) => (
                  <Photo key={item.id} item={item} />
                ))}
              </div>
              <p className="text-gray-800">{outfit.reason}</p>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

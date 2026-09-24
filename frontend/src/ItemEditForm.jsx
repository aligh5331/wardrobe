import { useState } from 'react'
import TaggingFields from './TaggingFields.jsx'

// ING-033 — edit form for one existing catalog item. It edits exactly the
// mutable fields of 04-data-schema.md ("Write-path rules (interactive
// create/edit)"): the seven tagging fields plus notes, rendered by the shared
// TaggingFields. id, added_date and the photo are shown read-only and are
// never part of the submitted body, so the PUT cannot carry them as changed
// values (07-architecture.md "Catalog write API": "id, added_date, and the
// photo are immutable via PUT").
//
// The selectable values are passed in as `taxonomy` — loaded from
// GET /api/taxonomy by the caller, never a bundled client-side copy
// (06-decisions.md "Taxonomy exported to the browser via GET /api/taxonomy,
// not a bundled copy").

export default function ItemEditForm({ item, taxonomy, saving, error, onSubmit, onCancel }) {
  const [values, setValues] = useState({
    category: item.category ?? '',
    subcategory: item.subcategory ?? '',
    dominant_color: item.dominant_color ?? '',
    secondary_colors: item.secondary_colors ?? [],
    pattern: item.pattern ?? '',
    warmth_tier: item.warmth_tier ?? '',
    formality: item.formality ?? '',
    notes: item.notes ?? '',
  })

  const set = (name, value) => setValues((current) => ({ ...current, [name]: value }))

  const handleSubmit = (event) => {
    event.preventDefault()
    // Exactly the mutable fields — no id, added_date, or photo_path.
    onSubmit({
      category: values.category,
      subcategory: values.subcategory,
      dominant_color: values.dominant_color,
      secondary_colors: values.secondary_colors,
      pattern: values.pattern,
      warmth_tier: values.warmth_tier,
      formality: values.formality,
      notes: values.notes,
    })
  }

  return (
    <form
      onSubmit={handleSubmit}
      aria-label="Edit item"
      className="flex flex-col gap-3"
    >
      <div className="flex items-start gap-3 text-sm text-gray-600">
        {item.photo_url && (
          <img
            src={item.photo_url}
            alt="Current photo"
            className="h-16 w-16 rounded object-cover"
          />
        )}
        <div className="flex flex-col gap-1">
          <p>
            ID: <span className="text-gray-900">{item.id}</span>
          </p>
          <p>
            Added: <span className="text-gray-900">{item.added_date}</span>
          </p>
        </div>
      </div>

      <TaggingFields values={values} taxonomy={taxonomy} onChange={set} idPrefix="edit" />

      {error && (
        <p role="alert" className="text-sm text-red-600">
          {error}
        </p>
      )}

      <div className="flex gap-2">
        <button
          type="submit"
          disabled={saving}
          className="rounded bg-gray-900 px-3 py-1 text-sm text-white disabled:opacity-50"
        >
          {saving ? 'Saving…' : 'Save'}
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
  )
}

import { useState } from 'react'
import TaggingFields from './TaggingFields.jsx'
import { Icon, Spinner, buttonPrimary, buttonSecondary, errorClass } from './ui.jsx'

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
      className="grid gap-6 sm:grid-cols-[minmax(0,200px)_1fr]"
    >
      <div className="flex items-start gap-4 sm:flex-col sm:gap-3">
        {item.photo_url && (
          <img
            src={item.photo_url}
            alt="Current photo"
            className="aspect-[4/5] w-24 shrink-0 rounded-xl bg-stone-100 object-cover sm:w-full sm:max-w-60"
          />
        )}
        <div className="flex flex-col gap-3">
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
            <dt className="text-stone-500">ID</dt>
            <dd className="font-mono break-all text-stone-700">{item.id}</dd>
            <dt className="text-stone-500">Added</dt>
            <dd className="text-stone-700">{item.added_date}</dd>
          </dl>
          <p className="text-xs text-stone-400">The photo, ID, and date added can’t be changed.</p>
        </div>
      </div>

      <div className="flex flex-col gap-5">
        <div className="grid gap-4 sm:grid-cols-2">
          <TaggingFields values={values} taxonomy={taxonomy} onChange={set} idPrefix="edit" />
        </div>

        {error && (
          <p role="alert" className={errorClass}>
            <Icon name="alert" className="mt-0.5 size-4" />
            {error}
          </p>
        )}

        <div className="flex flex-col-reverse gap-2 border-t border-stone-200 pt-4 sm:flex-row sm:justify-end">
          <button type="button" onClick={onCancel} disabled={saving} className={buttonSecondary}>
            Cancel
          </button>
          <button type="submit" disabled={saving} className={buttonPrimary}>
            {saving && <Spinner />}
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    </form>
  )
}

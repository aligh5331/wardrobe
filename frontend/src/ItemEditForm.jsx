import { useState } from 'react'

// ING-033 — edit form for one existing catalog item. It edits exactly the
// mutable fields of 04-data-schema.md ("Write-path rules (interactive
// create/edit)"): the seven tagging fields plus notes. id, added_date and the
// photo are shown read-only and are never part of the submitted body, so the
// PUT cannot carry them as changed values (07-architecture.md "Catalog write
// API": "id, added_date, and the photo are immutable via PUT").
//
// The selectable values are passed in as `taxonomy` — loaded from
// GET /api/taxonomy by the caller, never a bundled client-side copy
// (06-decisions.md "Taxonomy exported to the browser via GET /api/taxonomy,
// not a bundled copy").

// Choice lists for the select-backed fields, read from the server taxonomy.
function optionsFor(field, taxonomy) {
  switch (field) {
    case 'category':
      return Object.keys(taxonomy.categories ?? {})
    case 'dominant_color':
      return taxonomy.colors ?? []
    case 'pattern':
      return taxonomy.patterns ?? []
    case 'warmth_tier':
      return taxonomy.warmth_tiers ?? []
    case 'formality':
      return taxonomy.formality ?? []
    default:
      return []
  }
}

function SelectField({ id, label, value, options, onChange }) {
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-sm text-gray-600">
        {label}
      </label>
      <select
        id={id}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="rounded border border-gray-300 px-2 py-1 text-sm"
      >
        <option value="">—</option>
        {options.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </select>
    </div>
  )
}

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

  const categories = taxonomy.categories ?? {}
  const subcategories = categories[values.category] ?? []

  const changeCategory = (category) => {
    setValues((current) => ({
      ...current,
      category,
      // Keep the current subcategory only while it stays valid for the
      // new category; otherwise clear it so the pair cannot be invalid.
      subcategory: (categories[category] ?? []).includes(current.subcategory)
        ? current.subcategory
        : '',
    }))
  }

  const toggleSecondary = (color, checked) => {
    setValues((current) => ({
      ...current,
      secondary_colors: checked
        ? [...current.secondary_colors, color]
        : current.secondary_colors.filter((existing) => existing !== color),
    }))
  }

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

      <SelectField
        id="edit-category"
        label="Category"
        value={values.category}
        options={optionsFor('category', taxonomy)}
        onChange={changeCategory}
      />
      <SelectField
        id="edit-subcategory"
        label="Subcategory"
        value={values.subcategory}
        options={subcategories}
        onChange={(value) => set('subcategory', value)}
      />
      <SelectField
        id="edit-dominant_color"
        label="Dominant color"
        value={values.dominant_color}
        options={optionsFor('dominant_color', taxonomy)}
        onChange={(value) => set('dominant_color', value)}
      />
      <SelectField
        id="edit-pattern"
        label="Pattern"
        value={values.pattern}
        options={optionsFor('pattern', taxonomy)}
        onChange={(value) => set('pattern', value)}
      />
      <SelectField
        id="edit-warmth_tier"
        label="Warmth"
        value={values.warmth_tier}
        options={optionsFor('warmth_tier', taxonomy)}
        onChange={(value) => set('warmth_tier', value)}
      />
      <SelectField
        id="edit-formality"
        label="Formality"
        value={values.formality}
        options={optionsFor('formality', taxonomy)}
        onChange={(value) => set('formality', value)}
      />

      <fieldset className="flex flex-col gap-1">
        <legend className="text-sm text-gray-600">Secondary colors</legend>
        <div className="flex flex-wrap gap-x-3 gap-y-1">
          {(taxonomy.colors ?? []).map((color) => (
            <label key={color} className="flex items-center gap-1 text-sm text-gray-800">
              <input
                type="checkbox"
                checked={values.secondary_colors.includes(color)}
                onChange={(event) => toggleSecondary(color, event.target.checked)}
              />
              {color}
            </label>
          ))}
        </div>
      </fieldset>

      <div className="flex flex-col gap-1">
        <label htmlFor="edit-notes" className="text-sm text-gray-600">
          Notes
        </label>
        <textarea
          id="edit-notes"
          value={values.notes}
          onChange={(event) => set('notes', event.target.value)}
          className="rounded border border-gray-300 px-2 py-1 text-sm"
        />
      </div>

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

// ING-034 — shared tagging-field controls for the create form (this ticket)
// and the ING-033 edit form. It renders exactly the mutable fields of
// 04-data-schema.md ("Write-path rules (interactive create/edit)"): the seven
// tagging fields plus optional notes. id/added_date/photo_path are not here —
// they are never editable on either path.
//
// The selectable values arrive as `taxonomy`, loaded from GET /api/taxonomy by
// the caller, never a bundled client-side copy (06-decisions.md "Taxonomy
// exported to the browser via GET /api/taxonomy, not a bundled copy").

// Choice lists for the select-backed fields, read from the server taxonomy.
export function optionsFor(field, taxonomy) {
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

export function SelectField({ id, label, value, options, onChange }) {
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

// values: { category, subcategory, dominant_color, secondary_colors, pattern,
//           warmth_tier, formality, notes }
// onChange(name, value): updates one field in the caller's state.
// idPrefix: keeps the two co-existing forms' control ids distinct.
export default function TaggingFields({ values, taxonomy, onChange, idPrefix }) {
  const categories = taxonomy.categories ?? {}
  const subcategories = categories[values.category] ?? []

  const changeCategory = (category) => {
    onChange('category', category)
    // Keep the current subcategory only while it stays valid for the new
    // category; otherwise clear it so the pair cannot be invalid.
    if (!(categories[category] ?? []).includes(values.subcategory)) {
      onChange('subcategory', '')
    }
  }

  const toggleSecondary = (color, checked) => {
    onChange(
      'secondary_colors',
      checked
        ? [...values.secondary_colors, color]
        : values.secondary_colors.filter((existing) => existing !== color),
    )
  }

  return (
    <>
      <SelectField
        id={`${idPrefix}-category`}
        label="Category"
        value={values.category}
        options={optionsFor('category', taxonomy)}
        onChange={changeCategory}
      />
      <SelectField
        id={`${idPrefix}-subcategory`}
        label="Subcategory"
        value={values.subcategory}
        options={subcategories}
        onChange={(value) => onChange('subcategory', value)}
      />
      <SelectField
        id={`${idPrefix}-dominant_color`}
        label="Dominant color"
        value={values.dominant_color}
        options={optionsFor('dominant_color', taxonomy)}
        onChange={(value) => onChange('dominant_color', value)}
      />
      <SelectField
        id={`${idPrefix}-pattern`}
        label="Pattern"
        value={values.pattern}
        options={optionsFor('pattern', taxonomy)}
        onChange={(value) => onChange('pattern', value)}
      />
      <SelectField
        id={`${idPrefix}-warmth_tier`}
        label="Warmth"
        value={values.warmth_tier}
        options={optionsFor('warmth_tier', taxonomy)}
        onChange={(value) => onChange('warmth_tier', value)}
      />
      <SelectField
        id={`${idPrefix}-formality`}
        label="Formality"
        value={values.formality}
        options={optionsFor('formality', taxonomy)}
        onChange={(value) => onChange('formality', value)}
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
        <label htmlFor={`${idPrefix}-notes`} className="text-sm text-gray-600">
          Notes
        </label>
        <textarea
          id={`${idPrefix}-notes`}
          value={values.notes}
          onChange={(event) => onChange('notes', event.target.value)}
          className="rounded border border-gray-300 px-2 py-1 text-sm"
        />
      </div>
    </>
  )
}

// ING-034 — shared tagging-field controls for the create form (this ticket)
// and the ING-033 edit form. It renders exactly the mutable fields of
// 04-data-schema.md ("Write-path rules (interactive create/edit)"): the seven
// tagging fields plus optional notes. id/added_date/photo_path are not here —
// they are never editable on either path.
//
// The selectable values arrive as `taxonomy`, loaded from GET /api/taxonomy by
// the caller, never a bundled client-side copy (06-decisions.md "Taxonomy
// exported to the browser via GET /api/taxonomy, not a bundled copy").
//
// The fields render as a flat list so the caller can lay them out in a grid;
// secondary colors and notes span the full row (sm:col-span-2).

import { Icon, Swatch, inputClass, labelClass } from './ui.jsx'

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

export function SelectField({
  id,
  label,
  value,
  options,
  onChange,
  disabled = false,
  swatch = false,
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className={labelClass}>
        {label}
      </label>
      <div className="relative">
        {swatch && value && (
          <span className="pointer-events-none absolute inset-y-0 left-3 flex items-center">
            <Swatch color={value} />
          </span>
        )}
        <select
          id={id}
          value={value}
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
          className={`${inputClass} appearance-none pr-9 ${swatch && value ? 'pl-8' : ''}`}
        >
          <option value="">—</option>
          {options.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </select>
        <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-stone-400">
          <Icon name="chevron" />
        </span>
      </div>
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
        disabled={subcategories.length === 0}
        onChange={(value) => onChange('subcategory', value)}
      />
      <SelectField
        id={`${idPrefix}-dominant_color`}
        label="Dominant color"
        value={values.dominant_color}
        options={optionsFor('dominant_color', taxonomy)}
        swatch
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

      <fieldset className="flex flex-col gap-2 sm:col-span-2">
        <legend className={`${labelClass} mb-1.5`}>Secondary colors</legend>
        <div className="flex flex-wrap gap-1.5">
          {(taxonomy.colors ?? []).map((color) => {
            const checked = values.secondary_colors.includes(color)
            return (
              <label
                key={color}
                className={`inline-flex cursor-pointer items-center gap-1.5 rounded-full border py-1 pr-2.5 pl-1.5 text-sm transition-colors select-none has-focus-visible:outline-2 has-focus-visible:outline-offset-2 has-focus-visible:outline-stone-900 ${
                  checked
                    ? 'border-stone-900 bg-stone-100 font-medium text-stone-900 ring-1 ring-stone-900'
                    : 'border-stone-300 bg-white text-stone-700 hover:border-stone-400'
                }`}
              >
                <input
                  type="checkbox"
                  checked={checked}
                  onChange={(event) => toggleSecondary(color, event.target.checked)}
                  className="sr-only"
                />
                <Swatch color={color} className="size-4" />
                {color}
                {checked && <Icon name="check" className="size-3.5" />}
              </label>
            )
          })}
        </div>
      </fieldset>

      <div className="flex flex-col gap-1.5 sm:col-span-2">
        <label htmlFor={`${idPrefix}-notes`} className={labelClass}>
          Notes
        </label>
        <textarea
          id={`${idPrefix}-notes`}
          value={values.notes}
          rows={2}
          placeholder="Optional — fit, fabric, where it came from…"
          onChange={(event) => onChange('notes', event.target.value)}
          className={`${inputClass} resize-y`}
        />
      </div>
    </>
  )
}

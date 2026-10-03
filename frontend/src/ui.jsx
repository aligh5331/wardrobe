import { useEffect, useId, useRef, useState } from 'react'

// Shared look-and-feel for the panels, forms, and dialogs, so every screen
// uses the same buttons, inputs, and surfaces. Presentation only: nothing here
// fetches or owns catalog data.

const focusRing =
  'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-stone-900'

export const buttonPrimary = `inline-flex items-center justify-center gap-2 rounded-lg bg-stone-900 px-4 py-2 text-sm font-medium text-white shadow-sm transition-colors hover:bg-stone-700 disabled:cursor-not-allowed disabled:opacity-50 ${focusRing}`

export const buttonSecondary = `inline-flex items-center justify-center gap-2 rounded-lg border border-stone-300 bg-white px-4 py-2 text-sm font-medium text-stone-700 shadow-sm transition-colors hover:bg-stone-50 disabled:cursor-not-allowed disabled:opacity-50 ${focusRing}`

export const buttonGhost = `inline-flex items-center justify-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm font-medium text-stone-600 transition-colors hover:bg-stone-100 hover:text-stone-900 disabled:cursor-not-allowed disabled:opacity-50 ${focusRing}`

export const inputClass =
  'w-full rounded-lg border border-stone-300 bg-white px-3 py-2 text-sm text-stone-900 shadow-xs placeholder:text-stone-400 focus:border-stone-500 focus:ring-2 focus:ring-stone-900/10 focus:outline-none disabled:cursor-not-allowed disabled:bg-stone-50 disabled:text-stone-400'

export const labelClass = 'text-sm font-medium text-stone-700'

export const cardClass = 'rounded-2xl border border-stone-200 bg-white shadow-sm'

export const errorClass =
  'flex items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700'

// Small inline icons (24×24, stroke = currentColor). Always decorative: the
// text beside them carries the meaning, so they are hidden from assistive tech.
const ICON_PATHS = {
  plus: 'M12 5v14M5 12h14',
  pencil: 'M4 20h4L18.5 9.5a2.12 2.12 0 0 0-3-3L5 17v3zM13.5 7.5l3 3',
  close: 'M6 6l12 12M18 6L6 18',
  pin: 'M12 21s-6.5-5.8-6.5-11a6.5 6.5 0 0 1 13 0c0 5.2-6.5 11-6.5 11zM12 12.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5z',
  search: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20 20l-4-4',
  sparkles:
    'M10 3l1.6 4.4L16 9l-4.4 1.6L10 15l-1.6-4.4L4 9l4.4-1.6L10 3zM18 14l.8 2.2L21 17l-2.2.8L18 20l-.8-2.2L15 17l2.2-.8L18 14z',
  upload: 'M12 16V4M7 9l5-5 5 5M4 16v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2',
  alert:
    'M12 8v5M12 16.5v.01M10.3 3.9L2.4 18a2 2 0 0 0 1.7 3h15.8a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z',
  check: 'M5 12.5l4.5 4.5L19 7.5',
  refresh: 'M20 11a8 8 0 0 0-14.9-3.5M4 4v4h4M4 13a8 8 0 0 0 14.9 3.5M20 20v-4h-4',
  photo:
    'M4 6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6zM4 16l4.5-4.5a1.5 1.5 0 0 1 2 0L16 17M14 14l1.5-1.5a1.5 1.5 0 0 1 2 0L20 15M15 8.5h.01',
  hanger:
    'M12 7.5a2 2 0 1 1 2-2M12 7.5v1.2L3.3 15a1.6 1.6 0 0 0 .9 2.9h15.6a1.6 1.6 0 0 0 .9-2.9L12 8.7',
  chevron: 'M6 9l6 6 6-6',
}

export function Icon({ name, className = 'size-4' }) {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.75"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={`shrink-0 ${className}`}
    >
      <path d={ICON_PATHS[name]} />
    </svg>
  )
}

export function Spinner({ className = 'size-4' }) {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      className={`shrink-0 animate-spin ${className}`}
    >
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeOpacity="0.25" strokeWidth="3" />
      <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
    </svg>
  )
}

// Seconds since `running` last became true, for "this can take a while"
// waits on the local models. Frozen at its last value once running stops.
export function useElapsedSeconds(running) {
  const [seconds, setSeconds] = useState(0)
  useEffect(() => {
    if (!running) return undefined
    const started = Date.now()
    setSeconds(0)
    const timer = setInterval(() => setSeconds(Math.floor((Date.now() - started) / 1000)), 1000)
    return () => clearInterval(timer)
  }, [running])
  return seconds
}

export const formatElapsed = (seconds) =>
  `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`

// Display-only swatch colors for the palette names, in the same spirit as the
// WMO code → label map in WeatherPanel.jsx. This is not a source of valid
// values — the forms still read the vocabulary from GET /api/taxonomy — and a
// name missing here simply renders a neutral, dashed swatch.
const SWATCHES = {
  black: '#1c1917',
  white: '#ffffff',
  gray: '#9ca3af',
  navy: '#1e3a5f',
  blue: '#3b82f6',
  red: '#dc2626',
  green: '#16a34a',
  olive: '#6b7c32',
  brown: '#7c4a2d',
  tan: '#d2b48c',
  beige: '#e8dcc4',
  burgundy: '#800020',
  pink: '#f9a8d4',
  purple: '#9333ea',
  yellow: '#facc15',
  orange: '#f97316',
}

export function Swatch({ color, className = 'size-3.5' }) {
  const hex = SWATCHES[color]
  return (
    <span
      aria-hidden="true"
      style={hex ? { backgroundColor: hex } : undefined}
      className={`inline-block shrink-0 rounded-full ${
        hex ? 'ring-1 ring-black/20 ring-inset' : 'border border-dashed border-stone-400'
      } ${className}`}
    />
  )
}

// One garment photo, swapping to the shared "Missing photo" placeholder when
// there is no photo_url or the image fails to load (ING-020, ING-050).
export function GarmentPhoto({ item, className = '' }) {
  const [failed, setFailed] = useState(false)
  if (item.photo_url && !failed) {
    const alt = [item.dominant_color, item.subcategory].filter(Boolean).join(' ')
    return (
      <img
        src={item.photo_url}
        alt={alt}
        title={alt || undefined}
        loading="lazy"
        onError={() => setFailed(true)}
        className={`bg-stone-100 object-cover ${className}`}
      />
    )
  }
  return (
    <div
      role="img"
      aria-label="Missing photo"
      className={`flex flex-col items-center justify-center gap-1 bg-stone-100 text-xs text-stone-400 ${className}`}
    >
      <Icon name="photo" className="size-6" />
      No photo
    </div>
  )
}

// Modal dialog for the add and edit flows. While it is open the rest of the
// page is made `inert` by the caller, so focus cannot wander behind it; on
// close focus returns to whatever opened it. Escape and the close button call
// onClose unless `dismissible` is false (a save is in flight).
export function Modal({ title, description, onClose, dismissible = true, children }) {
  const titleId = useId()
  const descriptionId = useId()
  const panelRef = useRef(null)
  // Captured on first render, before the page behind turns inert, so focus can
  // be handed back on close.
  const [opener] = useState(() => document.activeElement)

  useEffect(() => {
    const panel = panelRef.current
    const target = panel?.querySelector('[data-autofocus]') ?? panel
    target?.focus()
    const { overflow } = document.body.style
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = overflow
      if (opener instanceof HTMLElement) opener.focus()
    }
  }, [opener])

  useEffect(() => {
    const onKeyDown = (event) => {
      if (event.key === 'Escape' && dismissible) onClose()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [dismissible, onClose])

  return (
    <div className="fixed inset-0 z-40 flex items-end justify-center bg-stone-950/40 backdrop-blur-[2px] sm:items-center sm:p-6">
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
        tabIndex={-1}
        className="flex max-h-[92vh] w-full max-w-3xl flex-col overflow-hidden rounded-t-2xl bg-white shadow-2xl outline-none sm:max-h-[88vh] sm:rounded-2xl"
      >
        <div className="flex items-start justify-between gap-4 border-b border-stone-200 px-5 py-4 sm:px-6">
          <div className="flex flex-col gap-0.5">
            <h2 id={titleId} className="text-lg font-semibold text-stone-900">
              {title}
            </h2>
            {description && (
              <p id={descriptionId} className="text-sm text-stone-500">
                {description}
              </p>
            )}
          </div>
          <button
            type="button"
            onClick={onClose}
            disabled={!dismissible}
            aria-label="Close"
            className={`${buttonGhost} -mr-2 p-1.5`}
          >
            <Icon name="close" className="size-5" />
          </button>
        </div>
        <div className="overflow-y-auto px-5 py-5 sm:px-6">{children}</div>
      </div>
    </div>
  )
}

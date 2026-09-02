const STEP = 16

function readStored(key) {
  try {
    return parseInt(localStorage.getItem(key), 10)
  } catch {
    return 0
  }
}

function writeStored(key, value) {
  try {
    localStorage.setItem(key, String(value))
  } catch {}
}

export function wireResize(options) {
  const { handle, pane, edge, prop, key, min, fallback, onChange } = options
  const capOf = () => (typeof options.max === 'function' ? options.max() : options.max)
  const cap = () => Math.round(Math.max(min, capOf()))
  const clamp = (width) => Math.min(cap(), Math.max(min, Math.round(width)))

  function read() {
    return clamp(readStored(key) || fallback)
  }

  function apply(width, persist = true) {
    const next = clamp(width)
    document.documentElement.style.setProperty(prop, next + 'px')
    if (persist) writeStored(key, next)
    if (handle) {
      handle.setAttribute('aria-valuemin', String(min))
      handle.setAttribute('aria-valuemax', String(cap()))
      handle.setAttribute('aria-valuenow', String(next))
    }
    return next
  }

  function widthAt(clientX) {
    const rect = pane.getBoundingClientRect()
    return edge === 'right' ? rect.right - clientX : clientX - rect.left
  }

  if (handle && pane) {
    handle.addEventListener('pointerdown', (e) => {
      e.preventDefault()
      handle.setPointerCapture(e.pointerId)
      const move = (event) => {
        apply(widthAt(event.clientX))
        if (onChange) onChange()
      }
      const done = () => {
        handle.removeEventListener('pointermove', move)
        handle.removeEventListener('pointerup', done)
        handle.removeEventListener('pointercancel', done)
      }
      handle.addEventListener('pointermove', move)
      handle.addEventListener('pointerup', done)
      handle.addEventListener('pointercancel', done)
    })

    handle.addEventListener('keydown', (e) => {
      if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return
      e.preventDefault()
      const towards = e.key === 'ArrowRight' ? STEP : -STEP
      apply(read() + (edge === 'right' ? -towards : towards))
      if (onChange) onChange()
    })
  }

  apply(read(), false)
  return { read, apply }
}

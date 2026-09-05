import { el, icon, iconButton, drawIcons, textButton } from './dom.js'

const FOCUSABLE = 'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])'

let open = null

export function closeModal() {
  if (open) open.close()
}

export function isModalOpen() {
  return Boolean(open)
}

export function openModal(spec) {
  closeModal()
  const root = document.getElementById('modal-root')
  if (!root) return { close() {} }

  const restore = document.activeElement instanceof HTMLElement ? document.activeElement : null
  const scrim = el('div', 'fixed inset-0 flex justify-center overflow-y-auto bg-scrim p-4 backdrop-blur-sm sm:p-6')
  const panel = el('div', 'my-auto h-fit w-full ' + (spec.width || 'max-w-md') + ' overflow-hidden rounded-2xl bg-mantle shadow-pop ring-1 ring-edge')
  panel.setAttribute('role', 'dialog')
  panel.setAttribute('aria-modal', 'true')

  const heading = el('h2', 'min-w-0 flex-1 truncate text-message font-semibold ' + (spec.tone === 'danger' ? 'text-red' : 'text-text'), spec.title)
  heading.id = 'modal-title-' + Math.random().toString(36).slice(2)
  panel.setAttribute('aria-labelledby', heading.id)

  const head = el('div', 'flex h-12 shrink-0 items-center gap-2 px-4')
  if (spec.icon) head.appendChild(icon(spec.icon, 'h-5 w-5 shrink-0 ' + (spec.tone === 'danger' ? 'text-red' : 'text-overlay1')))
  head.appendChild(heading)
  head.appendChild(iconButton('x', 'Close', () => handle.close()))
  panel.appendChild(head)

  const body = el('div', 'px-4 pb-4')
  panel.appendChild(body)

  const error = el('p', 'hidden px-4 pb-2 text-sm text-red')
  error.setAttribute('role', 'alert')
  panel.appendChild(error)

  const footer = el('div', 'flex items-center justify-end gap-2 px-4 pb-4')
  panel.appendChild(footer)

  scrim.appendChild(panel)

  const onKey = (e) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      handle.close()
      return
    }
    if (e.key !== 'Tab') return
    const targets = Array.from(panel.querySelectorAll(FOCUSABLE)).filter((n) => n.offsetParent !== null)
    if (!targets.length) return
    const first = targets[0]
    const last = targets[targets.length - 1]
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault()
      last.focus()
      return
    }
    if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault()
      first.focus()
    }
  }

  const handle = {
    body,
    footer,
    close() {
      if (open !== handle) return
      open = null
      document.removeEventListener('keydown', onKey, true)
      scrim.remove()
      if (restore && restore.isConnected) restore.focus()
      if (typeof spec.onClose === 'function') spec.onClose()
    },
    fail(message) {
      error.textContent = message || 'Something went wrong.'
      error.classList.remove('hidden')
    },
    clearError() {
      error.classList.add('hidden')
    },
  }

  scrim.addEventListener('mousedown', (e) => {
    if (e.target === scrim) handle.close()
  })
  document.addEventListener('keydown', onKey, true)

  if (typeof spec.build === 'function') spec.build(body, handle)
  if (Array.isArray(spec.actions)) {
    for (const action of spec.actions) footer.appendChild(action)
  }
  if (!footer.childElementCount) footer.remove()

  root.replaceChildren(scrim)
  open = handle
  drawIcons(scrim)

  const focusTarget = spec.focus ? panel.querySelector(spec.focus) : panel.querySelector(FOCUSABLE)
  if (focusTarget) focusTarget.focus()
  return handle
}

export function confirmModal(spec) {
  return new Promise((resolve) => {
    let settled = false
    const finish = (value) => {
      if (settled) return
      settled = true
      resolve(value)
    }
    const confirm = textButton(spec.confirmLabel || 'Confirm', () => {
      finish(true)
      handle.close()
    }, spec.destructive ? 'danger' : 'primary')
    const handle = openModal({
      title: spec.title,
      icon: spec.icon || (spec.destructive ? 'triangle-alert' : 'info'),
      tone: spec.destructive ? 'danger' : 'default',
      build(body) {
        body.appendChild(el('p', 'text-sm text-subtext0', spec.message))
        if (spec.consequence) body.appendChild(el('p', 'mt-2 text-sm text-overlay1', spec.consequence))
      },
      actions: [confirm],
      onClose: () => finish(false),
    })
    confirm.focus()
  })
}

export function promptModal(spec) {
  return new Promise((resolve) => {
    let settled = false
    const finish = (value) => {
      if (settled) return
      settled = true
      resolve(value)
    }
    let input = null
    const submit = () => {
      const value = input.value.trim()
      if (spec.required && !value) return
      finish(value)
      handle.close()
    }
    const handle = openModal({
      title: spec.title,
      icon: spec.icon || 'pen-line',
      build(body) {
        const wrap = el('label', 'block')
        wrap.appendChild(el('span', 'mb-1 block text-xs font-medium text-subtext0', spec.label))
        input = el('input', 'h-9 w-full rounded-lg bg-surface0 px-3 text-sm pointer-coarse:text-[1rem] text-text placeholder:text-overlay1 focus:outline-none focus:ring-1 focus:ring-mauve')
        input.type = spec.type || 'text'
        input.value = spec.value || ''
        if (spec.placeholder) input.placeholder = spec.placeholder
        input.addEventListener('keydown', (e) => {
          if (e.key !== 'Enter') return
          e.preventDefault()
          submit()
        })
        wrap.appendChild(input)
        body.appendChild(wrap)
        if (spec.hint) body.appendChild(el('p', 'mt-2 text-xs text-overlay1', spec.hint))
      },
      actions: [textButton(spec.confirmLabel || 'Save', submit, 'primary')],
      onClose: () => finish(null),
    })
    input.focus()
    input.select()
  })
}

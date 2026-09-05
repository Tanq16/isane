import { state } from '../store.js'

const AVATAR_HUES = [
  'bg-blue', 'bg-mauve', 'bg-green', 'bg-peach', 'bg-pink', 'bg-teal',
  'bg-lavender', 'bg-maroon', 'bg-sky', 'bg-flamingo', 'bg-yellow', 'bg-sapphire',
]
const ICON_SCRIPT = '/static/vendor/lucide.min.js'
const BRAND_MARK = '/static/icons/icon.svg'

const pendingIcons = new Set()

let iconScript = null

export function el(tag, cls, text) {
  const node = document.createElement(tag)
  if (cls) node.className = cls
  if (text != null) node.textContent = text
  return node
}

export function brandMark(cls) {
  const node = el('img', 'shrink-0 ' + (cls || 'h-7 w-7'))
  node.src = BRAND_MARK
  node.alt = 'Isane'
  return node
}

export function icon(name, cls) {
  const node = document.createElement('i')
  node.dataset.lucide = name
  node.className = cls || 'h-4 w-4'
  node.setAttribute('aria-hidden', 'true')
  return node
}

function loadIcons() {
  if (iconScript) return
  iconScript = document.createElement('script')
  iconScript.src = ICON_SCRIPT
  iconScript.addEventListener('load', () => {
    for (const root of pendingIcons) drawIcons(root)
    pendingIcons.clear()
  })
  document.head.appendChild(iconScript)
}

export function drawIcons(root) {
  if (!root) return
  if (typeof lucide === 'undefined') {
    pendingIcons.add(root)
    loadIcons()
    return
  }
  lucide.createIcons({ root })
}

export function iconButton(name, title, onClick, extra) {
  const button = el('button', 'grid h-8 w-8 shrink-0 place-items-center rounded-md text-overlay1 transition-colors hover:bg-surface0 hover:text-text focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve ' + (extra || ''))
  button.type = 'button'
  button.title = title
  button.setAttribute('aria-label', title)
  button.appendChild(icon(name, 'h-5 w-5'))
  if (onClick) button.addEventListener('click', onClick)
  return button
}

export function textButton(label, onClick, variant) {
  const shapes = {
    primary: 'h-9 rounded-lg bg-mauve px-4 text-sm font-semibold text-crust transition-colors hover:brightness-110 disabled:bg-surface1 disabled:text-overlay1',
    danger: 'h-9 rounded-lg bg-red px-4 text-sm font-semibold text-crust transition-colors hover:brightness-110 disabled:bg-surface1 disabled:text-overlay1',
    ghost: 'h-9 rounded-lg px-3 text-sm text-subtext0 transition-colors hover:bg-surface0 hover:text-text',
    secondary: 'h-9 rounded-lg bg-surface0 px-3 text-sm text-subtext0 transition-colors hover:bg-surface1 hover:text-text',
  }
  const button = el('button', (shapes[variant] || shapes.secondary) + ' focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve', label)
  button.type = 'button'
  if (onClick) button.addEventListener('click', onClick)
  return button
}

export function field(labelText, opts = {}) {
  const wrap = el('label', 'block min-w-0')
  wrap.appendChild(el('span', 'mb-1 block text-xs font-medium text-subtext0', labelText))
  const input = el('input', 'h-9 w-full rounded-lg bg-surface0 px-3 text-sm pointer-coarse:text-[1rem] text-text transition-colors placeholder:text-overlay1 focus:outline-none focus:ring-1 focus:ring-mauve')
  input.type = opts.type || 'text'
  if (opts.placeholder) input.placeholder = opts.placeholder
  if (opts.autocomplete) input.autocomplete = opts.autocomplete
  if (opts.name) {
    input.name = opts.name
    input.id = 'field-' + opts.name
    wrap.htmlFor = input.id
  }
  if (opts.value != null) input.value = opts.value
  if (opts.readOnly) {
    input.readOnly = true
    input.classList.add('text-overlay1')
  }
  wrap.appendChild(input)
  return { wrap, input }
}

function hueFor(id) {
  const key = String(id || '')
  let sum = 0
  for (let i = 0; i < key.length; i++) sum += key.charCodeAt(i)
  return AVATAR_HUES[sum % AVATAR_HUES.length]
}

export function avatarNode(u, size, textSize) {
  const dimensions = size || 'h-10 w-10'
  if (u && u.avatar_id) {
    const img = el('img', dimensions + ' shrink-0 rounded-full object-cover ring-1 ring-edge')
    img.src = '/api/attachments/' + u.avatar_id + '/thumb'
    img.alt = ''
    return img
  }
  const node = el('div', dimensions + ' grid shrink-0 place-items-center rounded-full font-semibold text-crust ' + hueFor(u && u.id) + ' ' + (textSize || 'text-message'))
  node.textContent = ((u && u.display_name) || '?').trim().charAt(0).toUpperCase()
  node.setAttribute('aria-hidden', 'true')
  return node
}

export function presenceDot(userId, ringToken) {
  const online = state.presence.has(userId)
  return el('span', 'h-2.5 w-2.5 shrink-0 rounded-full ring-2 ' + (ringToken || 'ring-crust') + ' ' + (online ? 'bg-green' : 'bg-overlay0'))
}

export function sizeLabel(n) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = Number(n) || 0
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i += 1
  }
  return (i === 0 ? value : value.toFixed(1)) + ' ' + units[i]
}

function pad(n) {
  return String(n).padStart(2, '0')
}

export function clockLabel(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  return pad(d.getHours()) + ':' + pad(d.getMinutes())
}

export function stampLabel(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  const today = new Date()
  const yesterday = new Date(today.getTime() - 86400000)
  if (d.toDateString() === today.toDateString()) return 'Today at ' + clockLabel(iso)
  if (d.toDateString() === yesterday.toDateString()) return 'Yesterday at ' + clockLabel(iso)
  return pad(d.getDate()) + '/' + pad(d.getMonth() + 1) + '/' + d.getFullYear() + ' ' + clockLabel(iso)
}

export function dayLabel(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  const today = new Date()
  const yesterday = new Date(today.getTime() - 86400000)
  if (d.toDateString() === today.toDateString()) return 'Today'
  if (d.toDateString() === yesterday.toDateString()) return 'Yesterday'
  return d.toLocaleDateString([], { day: 'numeric', month: 'long', year: 'numeric' })
}

export function relativeLabel(iso) {
  if (!iso) return ''
  const seconds = (Date.now() - new Date(iso).getTime()) / 1000
  if (seconds < 60) return 'just now'
  if (seconds < 3600) return Math.floor(seconds / 60) + 'm ago'
  if (seconds < 86400) return Math.floor(seconds / 3600) + 'h ago'
  return Math.floor(seconds / 86400) + 'd ago'
}

export function dateLabel(iso) {
  if (!iso) return 'never'
  return new Date(iso).toLocaleString([], { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export function timeNode(iso, cls, label) {
  const node = el('time', cls, label)
  if (iso) node.dateTime = new Date(iso).toISOString()
  return node
}

export function conversationTitle(c, lookup) {
  const ids = (c.participants || []).filter((id) => id !== (state.me && state.me.id))
  if (!ids.length) return c.name || 'Conversation'
  return ids.map((id) => lookup(id).display_name).join(', ')
}

export function containerLabel(c, lookup) {
  if (!c) return ''
  if (c.kind === 'channel') return c.name || c.slug || 'Channel'
  return conversationTitle(c, lookup)
}

export function containerPath(c) {
  if (!c) return '/'
  return c.kind === 'channel' && c.slug ? '/c/' + c.slug : '/d/' + c.id
}

export function navigate(path) {
  if (location.pathname + location.search === path) return
  history.pushState(null, '', path)
  window.dispatchEvent(new CustomEvent('isane:navigate', { detail: { path } }))
}

export function emptyState(message) {
  return el('p', 'px-4 py-16 text-center text-message text-subtext0', message)
}

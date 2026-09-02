import * as api from '../api.js'
import { state, subscribe, notify, user } from '../store.js'
import { plainText } from '../render.js'
import { currentTheme, toggleTheme } from '../theme.js'
import {
  avatarNode, containerLabel, containerPath, conversationTitle, drawIcons, el, icon,
  iconButton, navigate, presenceDot,
} from './dom.js'
import { openPicker } from './picker.js'
import { mayManageChannels, openChannelSettings, openCreateChannel, openUserSettings } from './settings.js'
import { toast } from './toast.js'

const COLLAPSE_KEY = 'isane:sidebar-sections'

let rootEl = null
let panelEl = null
let searchInput = null
let searchResultsEl = null
let footerEl = null
let themeButton = null

const groups = new Map()
const rows = new Map()
const collapsed = new Set()

let searchTimer = 0
let searchTerm = ''
let searchResults = []
let searchBusy = false
let frame = 0

function scheduleRender() {
  if (frame) return
  frame = requestAnimationFrame(() => {
    frame = 0
    render()
  })
}

function readCollapsed() {
  try {
    for (const key of JSON.parse(localStorage.getItem(COLLAPSE_KEY) || '[]')) collapsed.add(key)
  } catch {}
}

function writeCollapsed() {
  try {
    localStorage.setItem(COLLAPSE_KEY, JSON.stringify(Array.from(collapsed)))
  } catch {}
}

function closeDrawer() {
  const app = document.getElementById('app')
  if (app) app.dataset.drawer = 'closed'
}

function toggleDrawer() {
  const app = document.getElementById('app')
  if (!app) return
  app.dataset.drawer = app.dataset.drawer === 'open' ? 'closed' : 'open'
}

function openContainer(c) {
  state.current.containerId = c.id
  state.current.threadRootId = null
  state.current.replyToId = null
  navigate(containerPath(c))
  notify('current')
  closeDrawer()
}

function containers(kind) {
  const out = []
  for (const c of state.containers.values()) {
    if (c.kind === kind) out.push(c)
  }
  return out
}

function group(key, title, trailing) {
  const wrap = el('section', 'mt-4 first:mt-0')
  const head = el('div', 'flex h-6 items-center gap-1 px-2')
  const toggle = el('button', 'flex min-w-0 flex-1 items-center gap-1 text-micro font-bold uppercase tracking-widest text-lavender transition-colors hover:brightness-125 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve')
  toggle.type = 'button'
  const chevron = icon(collapsed.has(key) ? 'chevron-right' : 'chevron-down', 'h-3 w-3 shrink-0')
  toggle.appendChild(chevron)
  toggle.appendChild(el('span', 'truncate', title))
  head.appendChild(toggle)
  if (trailing) head.appendChild(trailing)
  wrap.appendChild(head)

  const list = el('div', 'mt-0.5 space-y-0.5')
  list.id = 'sidebar-group-' + key
  toggle.setAttribute('aria-controls', list.id)
  wrap.appendChild(list)

  toggle.addEventListener('click', () => {
    if (collapsed.has(key)) collapsed.delete(key)
    else collapsed.add(key)
    writeCollapsed()
    render()
  })

  const entry = { wrap, list, toggle, chevron }
  groups.set(key, entry)
  return entry
}

function smallIconButton(name, title, onClick) {
  const button = el('button', 'grid h-5 w-5 shrink-0 place-items-center rounded text-overlay1 transition-colors hover:text-text focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve')
  button.type = 'button'
  button.title = title
  button.setAttribute('aria-label', title)
  button.appendChild(icon(name, 'h-4 w-4'))
  button.addEventListener('click', onClick)
  return button
}

function rowFor(c) {
  let row = rows.get(c.id)
  if (row) return row

  const node = el('button')
  node.type = 'button'
  const rail = el('span', 'absolute -left-2 h-2 w-1 rounded-r-full bg-text')
  const rule = el('span', 'absolute bottom-1 left-0 top-1 w-0.5 rounded-full bg-mauve')
  const lead = el('span', 'grid h-5 w-5 shrink-0 place-items-center')
  const label = el('span', 'min-w-0 flex-1 truncate')
  const trail = el('span', 'ml-auto flex shrink-0 items-center gap-1')
  node.append(rail, rule, lead, label, trail)
  node.addEventListener('click', () => openContainer(state.containers.get(c.id) || c))
  row = { node, rail, rule, lead, label, trail }
  rows.set(c.id, row)
  return row
}

function paintRow(c, text) {
  const row = rowFor(c)
  const active = state.current.containerId === c.id
  const muted = c.level === 'none'
  const unread = (c.unread || 0) > 0
  const mentions = c.mentions || 0

  row.node.className = 'relative flex h-8 w-full items-center gap-1.5 rounded-md px-2 text-left text-message focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve '
    + (active ? 'bg-surface0 text-text' : 'text-subtext0 hover:bg-surface0/60 hover:text-text')
  row.node.setAttribute('aria-current', active ? 'page' : 'false')
  row.rail.classList.toggle('hidden', active || (!unread && !mentions))
  row.rule.classList.toggle('hidden', !active)

  row.label.textContent = text
  row.label.className = 'min-w-0 flex-1 truncate'
  if (unread || mentions) row.label.classList.add('font-medium', 'text-text')
  else if (muted && !active) row.label.classList.add('text-overlay0')

  row.lead.replaceChildren()
  if (c.kind === 'channel') {
    row.lead.appendChild(icon('hash', 'h-5 w-5 text-overlay1'))
  } else {
    const partner = (c.participants || []).find((id) => id !== (state.me && state.me.id))
    const stack = el('span', 'relative')
    stack.appendChild(avatarNode(user(partner), 'h-5 w-5', 'text-micro'))
    const dot = presenceDot(partner, 'ring-crust')
    dot.classList.add('absolute', '-bottom-0.5', '-right-0.5', 'h-2', 'w-2')
    stack.appendChild(dot)
    row.lead.appendChild(stack)
  }

  row.trail.replaceChildren()
  const call = state.calls.get(c.id)
  if (call && !call.ended_at) row.trail.appendChild(icon('phone-call', 'h-4 w-4 text-teal'))
  if (c.archived_at) {
    row.trail.appendChild(el('span', 'text-micro uppercase tracking-widest text-overlay1', 'archived'))
  } else if (mentions > 0) {
    row.trail.appendChild(countPill(mentions))
  } else if (c.kind === 'conversation' && unread) {
    row.trail.appendChild(countPill(c.unread))
  }

  drawIcons(row.node)
  return row.node
}

function countPill(count) {
  return el(
    'span',
    'grid h-4 min-w-4 place-items-center rounded-full bg-red px-1 text-micro font-bold tabular-nums text-crust',
    count > 99 ? '99+' : String(count)
  )
}

function syncGroup(entry, key, items, textOf) {
  entry.chevron.dataset.lucide = collapsed.has(key) ? 'chevron-right' : 'chevron-down'
  entry.toggle.setAttribute('aria-expanded', collapsed.has(key) ? 'false' : 'true')
  entry.list.classList.toggle('hidden', collapsed.has(key))

  let cursor = entry.list.firstChild
  for (const c of items) {
    const node = paintRow(c, textOf(c))
    if (cursor === node) {
      cursor = cursor.nextSibling
      continue
    }
    entry.list.insertBefore(node, cursor)
  }
  while (cursor) {
    const next = cursor.nextSibling
    cursor.remove()
    cursor = next
  }
}

function byName(a, b) {
  return (a.name || a.slug || '').localeCompare(b.name || b.slug || '')
}

function renderGroups() {
  const channels = containers('channel')
  const live = channels.filter((c) => !c.archived_at).sort(byName)
  const archived = channels.filter((c) => c.archived_at).sort(byName)
  const conversations = containers('conversation').sort((a, b) => (b.last_seq || 0) - (a.last_seq || 0))

  syncGroup(groups.get('channels'), 'channels', live, (c) => c.name || c.slug)
  syncGroup(groups.get('conversations'), 'conversations', conversations, (c) => conversationTitle(c, user))
  syncGroup(groups.get('archived'), 'archived', archived, (c) => c.name || c.slug)

  groups.get('archived').wrap.classList.toggle('hidden', archived.length === 0)
  if (!conversations.length && !collapsed.has('conversations')) {
    groups.get('conversations').list.appendChild(el('p', 'px-2 py-1 text-xs text-overlay1', 'No direct messages yet.'))
  }
  const addChannel = document.getElementById('sidebar-add-channel')
  if (addChannel) addChannel.classList.toggle('hidden', !mayManageChannels())
  for (const entry of groups.values()) drawIcons(entry.toggle)
}

function renderSearchResults() {
  searchResultsEl.replaceChildren()
  searchResultsEl.classList.toggle('hidden', !searchTerm)
  if (!searchTerm) return

  if (searchBusy) {
    searchResultsEl.appendChild(el('p', 'px-2 py-2 text-xs text-overlay1', 'Searching'))
    return
  }
  if (!searchResults.length) {
    searchResultsEl.appendChild(el('p', 'px-2 py-2 text-xs text-overlay1', 'No matches.'))
    return
  }

  for (const hit of searchResults) {
    const m = hit && hit.message ? hit.message : hit
    const c = state.containers.get(m.container_id)
    const row = el('button', 'block w-full rounded-md px-2 py-1.5 text-left transition-colors hover:bg-surface0 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve')
    row.type = 'button'
    const head = el('div', 'flex items-baseline gap-2')
    head.appendChild(el('span', 'truncate text-xs font-semibold text-text', user(m.author_id).display_name))
    head.appendChild(el('span', 'truncate text-xs text-overlay1', c ? containerLabel(c, user) : ''))
    row.appendChild(head)
    row.appendChild(el('p', 'line-clamp-2 text-xs text-subtext0', plainText(m.body || '')))
    row.addEventListener('click', () => {
      if (!c) return
      state.current.containerId = c.id
      state.current.threadRootId = null
      navigate(containerPath(c) + '?m=' + m.seq)
      notify('current')
      closeDrawer()
    })
    searchResultsEl.appendChild(row)
  }
}

async function runSearch(q) {
  searchTerm = q
  if (!q) {
    searchResults = []
    searchBusy = false
    renderSearchResults()
    return
  }
  searchBusy = true
  renderSearchResults()
  try {
    const res = await api.get('/api/search', { q, limit: 25 })
    if (searchTerm !== q) return
    searchResults = Array.isArray(res) ? res : res.results || []
  } catch {
    searchResults = []
  }
  searchBusy = false
  renderSearchResults()
}

async function signOut() {
  try {
    await api.post('/api/auth/logout', {})
  } catch {}
  window.dispatchEvent(new CustomEvent('isane:signed-out'))
}

function renderFooter() {
  footerEl.replaceChildren()
  const me = state.me
  if (!me) return

  const identity = el('button', 'flex min-w-0 flex-1 items-center gap-2 rounded-md px-1 py-1 text-left transition-colors hover:bg-surface0 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve')
  identity.type = 'button'
  identity.title = 'Your account'
  identity.appendChild(avatarNode(me, 'h-8 w-8', 'text-xs'))
  const who = el('div', 'min-w-0 flex-1')
  who.appendChild(el('p', 'truncate text-sm font-semibold text-text', me.display_name))
  who.appendChild(el('p', 'truncate text-xs text-overlay1', '@' + me.handle))
  identity.appendChild(who)
  identity.addEventListener('click', () => openUserSettings(signOut))

  const actions = el('div', 'flex shrink-0 items-center')
  themeButton = iconButton(currentTheme() === 'dark' ? 'sun' : 'moon', 'Switch theme', () => {
    const next = toggleTheme()
    themeButton.replaceChildren(icon(next === 'dark' ? 'sun' : 'moon', 'h-5 w-5'))
    drawIcons(themeButton)
  })
  actions.appendChild(themeButton)
  if (me.is_admin) {
    actions.appendChild(iconButton('shield', 'Administration', () => {
      navigate('/admin')
      closeDrawer()
    }))
  }
  actions.appendChild(iconButton('log-out', 'Sign out', signOut))

  const row = el('div', 'flex items-center gap-1')
  row.append(identity, actions)
  footerEl.appendChild(row)
  drawIcons(footerEl)
}

function render() {
  renderGroups()
  renderFooter()
}

export function mount(root) {
  rootEl = root
  readCollapsed()

  panelEl = el('div', 'flex h-full w-full min-w-60 flex-col px-2 py-2')

  const head = el('div', 'flex h-8 shrink-0 items-center gap-1 px-2')
  head.appendChild(el('span', 'min-w-0 flex-1 truncate font-display text-lg font-bold text-text', 'Isane'))
  head.appendChild(iconButton('x', 'Close the channel list', closeDrawer, 'md:hidden'))
  panelEl.appendChild(head)

  const searchWrap = el('div', 'relative mt-2 shrink-0')
  const searchBox = el('div', 'flex h-8 items-center gap-1.5 rounded-lg bg-surface0 px-2 focus-within:ring-1 focus-within:ring-mauve')
  searchBox.appendChild(icon('search', 'h-4 w-4 shrink-0 text-overlay1'))
  searchInput = el('input', 'min-w-0 flex-1 bg-transparent text-sm text-text placeholder:text-overlay1 focus:outline-none')
  searchInput.type = 'search'
  searchInput.placeholder = 'Search messages'
  searchInput.setAttribute('aria-label', 'Search messages')
  searchBox.appendChild(searchInput)
  searchWrap.appendChild(searchBox)
  searchResultsEl = el('div', 'absolute inset-x-0 top-9 z-20 hidden max-h-80 space-y-0.5 overflow-y-auto rounded-lg bg-base p-1 shadow-pop')
  searchWrap.appendChild(searchResultsEl)
  panelEl.appendChild(searchWrap)

  const nav = el('nav', 'mt-2 min-h-0 flex-1 overflow-y-auto overflow-x-hidden')
  nav.setAttribute('aria-label', 'Conversations')

  const addChannel = smallIconButton('plus', 'Create a channel', () => openCreateChannel())
  addChannel.id = 'sidebar-add-channel'
  nav.appendChild(group('channels', 'Channels', addChannel).wrap)
  nav.appendChild(group('conversations', 'Direct messages', smallIconButton('square-pen', 'Start a conversation', () => openPicker())).wrap)
  nav.appendChild(group('archived', 'Archived').wrap)
  panelEl.appendChild(nav)

  footerEl = el('div', 'mt-2 shrink-0')
  panelEl.appendChild(footerEl)

  rootEl.replaceChildren(panelEl)
  drawIcons(panelEl)

  searchInput.addEventListener('input', () => {
    clearTimeout(searchTimer)
    const q = searchInput.value.trim()
    searchTimer = setTimeout(() => runSearch(q), 250)
  })
  searchInput.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return
    searchInput.value = ''
    runSearch('')
  })

  const scrim = document.getElementById('drawer-scrim')
  if (scrim) scrim.addEventListener('click', closeDrawer)
  window.addEventListener('isane:sidebar-toggle', toggleDrawer)
  window.addEventListener('isane:open-channel-settings', () => {
    const c = state.current.containerId ? state.containers.get(state.current.containerId) : null
    if (c) openChannelSettings(c)
  })
  window.addEventListener('isane:open-picker', () => openPicker())
  window.addEventListener('isane:settings-restricted', () => {
    toast('An administrator restricted channel creation', { severity: 'warning' })
  })

  subscribe(scheduleRender)
  render()
}

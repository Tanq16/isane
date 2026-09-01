import { state, subscribe, notify, user } from '../store.js'
import * as api from '../api.js'
import { plainText } from '../render.js'

const LAST_CONTAINER_KEY = 'isane:last-container'

let rootEl = null
let backdropEl = null
let panelEl = null
let channelsEl = null
let archivedEl = null
let archivedGroupEl = null
let conversationsEl = null
let footerEl = null
let searchInput = null
let searchResultsEl = null
let pickerEl = null
let pickerQuery = ''
let pickerSelection = new Set()
let pickerOpen = false
let searchTimer = 0
let searchTerm = ''
let searchResults = []
let searchBusy = false
let frame = 0
let lastPersisted = null

function el(tag, cls, text) {
  const node = document.createElement(tag)
  if (cls) node.className = cls
  if (text != null) node.textContent = text
  return node
}

function scheduleRender() {
  if (frame) return
  frame = requestAnimationFrame(() => {
    frame = 0
    render()
  })
}

function navigate(path) {
  if (location.pathname + location.search === path) return
  history.pushState(null, '', path)
  window.dispatchEvent(new CustomEvent('isane:navigate', { detail: { path } }))
}

function containerPath(c) {
  return c.kind === 'channel' ? '/c/' + c.slug : '/d/' + c.id
}

function openContainer(c) {
  state.current.containerId = c.id
  state.current.threadRootId = null
  state.current.replyToId = null
  navigate(containerPath(c))
  notify('current')
  closeDrawer()
}

function closeDrawer() {
  rootEl.classList.add('-translate-x-full')
  backdropEl.classList.add('hidden')
}

function openDrawer() {
  rootEl.classList.remove('-translate-x-full')
  backdropEl.classList.remove('hidden')
}

function containers(kind) {
  const out = []
  for (const c of state.containers.values()) {
    if (c.kind === kind) out.push(c)
  }
  return out
}

function conversationTitle(c) {
  const ids = (c.participants || []).filter((id) => id !== (state.me && state.me.id))
  if (!ids.length) return c.name || 'Conversation'
  return ids.map((id) => user(id).display_name).join(', ')
}

function conversationOnline(c) {
  const ids = (c.participants || []).filter((id) => id !== (state.me && state.me.id))
  return ids.some((id) => state.presence.has(id))
}

function badge(count, cls) {
  const node = el('span', cls)
  node.textContent = count > 99 ? '99+' : String(count)
  return node
}

function containerRow(c, label) {
  const active = state.current.containerId === c.id
  const row = el(
    'button',
    active
      ? 'flex w-full items-center gap-2 rounded-lg bg-surface0 px-2 py-1.5 text-left text-sm text-text'
      : 'flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm text-subtext0 hover:bg-surface0'
  )
  row.type = 'button'

  if (c.kind === 'channel') {
    row.appendChild(el('span', 'w-3 shrink-0 text-center text-overlay1', '#'))
  } else {
    row.appendChild(
      el('span', conversationOnline(c) ? 'h-2 w-2 shrink-0 rounded-full bg-green' : 'h-2 w-2 shrink-0 rounded-full bg-overlay0')
    )
  }

  const name = el('span', c.unread > 0 ? 'truncate font-semibold text-text' : 'truncate', label)
  row.appendChild(name)

  if (c.archived_at) {
    row.appendChild(el('span', 'ml-auto shrink-0 text-xs text-overlay1', 'archived'))
  } else if (c.mentions > 0) {
    row.appendChild(badge(c.mentions, 'ml-auto shrink-0 rounded-full bg-red px-1.5 py-0.5 text-xs font-semibold text-crust'))
  } else if (c.unread > 0) {
    row.appendChild(badge(c.unread, 'ml-auto shrink-0 rounded-full bg-surface1 px-1.5 py-0.5 text-xs text-subtext1'))
  }

  row.addEventListener('click', () => openContainer(c))
  return row
}

function renderChannels() {
  const list = containers('channel')
  const live = list.filter((c) => !c.archived_at).sort((a, b) => (a.name || a.slug || '').localeCompare(b.name || b.slug || ''))
  const archived = list.filter((c) => c.archived_at).sort((a, b) => (a.name || a.slug || '').localeCompare(b.name || b.slug || ''))

  channelsEl.replaceChildren()
  for (const c of live) channelsEl.appendChild(containerRow(c, c.name || c.slug))

  archivedEl.replaceChildren()
  for (const c of archived) archivedEl.appendChild(containerRow(c, c.name || c.slug))
  archivedGroupEl.classList.toggle('hidden', archived.length === 0)
}

function renderConversations() {
  const list = containers('conversation').sort((a, b) => (b.last_seq || 0) - (a.last_seq || 0))
  conversationsEl.replaceChildren()
  for (const c of list) conversationsEl.appendChild(containerRow(c, conversationTitle(c)))
  if (!list.length) {
    conversationsEl.appendChild(el('p', 'px-2 py-1 text-xs text-overlay1', 'No direct messages yet.'))
  }
}

function renderPickerList(listEl, startButton) {
  listEl.replaceChildren()
  const q = pickerQuery.trim().toLowerCase()
  const candidates = []
  for (const u of state.users.values()) {
    if (u.kind !== 'human' || u.deactivated_at) continue
    if (state.me && u.id === state.me.id) continue
    if (q && !u.handle.toLowerCase().includes(q) && !u.display_name.toLowerCase().includes(q)) continue
    candidates.push(u)
  }
  candidates.sort((a, b) => a.display_name.localeCompare(b.display_name))

  for (const u of candidates) {
    const row = el('label', 'flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-sm text-subtext0 hover:bg-surface0')
    const box = el('input', 'accent-mauve')
    box.type = 'checkbox'
    box.checked = pickerSelection.has(u.id)
    box.addEventListener('change', () => {
      if (box.checked) pickerSelection.add(u.id)
      else pickerSelection.delete(u.id)
      startButton.disabled = pickerSelection.size === 0
    })
    row.appendChild(box)
    row.appendChild(el('span', 'truncate text-text', u.display_name))
    row.appendChild(el('span', 'truncate text-xs text-overlay1', '@' + u.handle))
    listEl.appendChild(row)
  }
  if (!candidates.length) listEl.appendChild(el('p', 'px-2 py-1 text-xs text-overlay1', 'No matching people.'))
}

function renderPicker() {
  pickerEl.classList.toggle('hidden', !pickerOpen)
  if (!pickerOpen) {
    pickerEl.replaceChildren()
    return
  }

  const search = el('input', 'w-full rounded-lg border border-surface1 bg-surface0 px-2 py-1.5 text-sm text-text placeholder:text-overlay0')
  search.type = 'search'
  search.placeholder = 'Find people'
  search.value = pickerQuery

  const list = el('div', 'mt-2 max-h-56 space-y-0.5 overflow-y-auto')
  const error = el('p', 'mt-2 hidden text-xs text-red')
  const actions = el('div', 'mt-2 flex gap-2')

  const start = el('button', 'rounded-lg bg-mauve px-3 py-1.5 text-sm font-semibold text-crust disabled:opacity-50', 'Start')
  start.type = 'button'
  start.disabled = pickerSelection.size === 0
  start.addEventListener('click', async () => {
    start.disabled = true
    error.classList.add('hidden')
    try {
      const c = await api.post('/api/conversations', { user_ids: Array.from(pickerSelection) })
      pickerOpen = false
      pickerSelection = new Set()
      pickerQuery = ''
      const view = state.containers.get(c.id) || c
      state.containers.set(c.id, view)
      notify('containers')
      openContainer(view)
    } catch (err) {
      error.textContent = err.message || 'Could not start the conversation.'
      error.classList.remove('hidden')
      start.disabled = false
    }
  })

  const cancel = el('button', 'rounded-lg border border-surface1 px-3 py-1.5 text-sm text-subtext0 hover:bg-surface0', 'Cancel')
  cancel.type = 'button'
  cancel.addEventListener('click', () => {
    pickerOpen = false
    pickerSelection = new Set()
    pickerQuery = ''
    renderPicker()
  })

  search.addEventListener('input', () => {
    pickerQuery = search.value
    renderPickerList(list, start)
  })

  actions.appendChild(start)
  actions.appendChild(cancel)
  renderPickerList(list, start)
  pickerEl.replaceChildren(search, list, error, actions)
  search.focus()
}

function renderSearchResults() {
  searchResultsEl.replaceChildren()
  if (!searchTerm) {
    searchResultsEl.classList.add('hidden')
    return
  }
  searchResultsEl.classList.remove('hidden')

  if (searchBusy) {
    searchResultsEl.appendChild(el('p', 'px-2 py-2 text-xs text-overlay1', 'Searching'))
    return
  }
  if (!searchResults.length) {
    searchResultsEl.appendChild(el('p', 'px-2 py-2 text-xs text-overlay1', 'No matches.'))
    return
  }

  for (const m of searchResults) {
    const c = state.containers.get(m.container_id)
    const row = el('button', 'block w-full rounded-lg px-2 py-1.5 text-left hover:bg-surface0')
    row.type = 'button'
    const head = el('div', 'flex items-baseline gap-2')
    head.appendChild(el('span', 'truncate text-xs font-semibold text-text', user(m.author_id).display_name))
    head.appendChild(el('span', 'truncate text-xs text-overlay1', c ? (c.kind === 'channel' ? '#' + c.slug : conversationTitle(c)) : ''))
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

function renderFooter() {
  footerEl.replaceChildren()
  const me = state.me
  if (!me) return

  const row = el('div', 'flex items-center gap-2')
  row.appendChild(avatarNode(me, 'h-8 w-8'))
  const who = el('div', 'min-w-0 flex-1')
  who.appendChild(el('p', 'truncate text-sm font-semibold text-text', me.display_name))
  who.appendChild(el('p', 'truncate text-xs text-overlay1', '@' + me.handle))
  row.appendChild(who)
  footerEl.appendChild(row)

  const actions = el('div', 'mt-2 flex gap-2')
  if (me.is_admin) {
    const admin = el('button', 'rounded-lg border border-surface1 px-2 py-1 text-xs text-subtext0 hover:bg-surface0', 'Admin')
    admin.type = 'button'
    admin.addEventListener('click', () => {
      navigate('/admin')
      closeDrawer()
    })
    actions.appendChild(admin)
  }
  const logout = el('button', 'rounded-lg border border-surface1 px-2 py-1 text-xs text-subtext0 hover:bg-surface0', 'Sign out')
  logout.type = 'button'
  logout.addEventListener('click', async () => {
    logout.disabled = true
    try {
      await api.post('/api/auth/logout', {})
    } catch {
      logout.disabled = false
    }
    location.href = '/login'
  })
  actions.appendChild(logout)
  footerEl.appendChild(actions)
}

function avatarNode(u, size) {
  if (u.avatar_id) {
    const img = el('img', size + ' shrink-0 rounded-full object-cover')
    img.src = '/api/attachments/' + u.avatar_id + '/thumb'
    img.alt = ''
    return img
  }
  const node = el('div', size + ' flex shrink-0 items-center justify-center rounded-full bg-surface1 text-xs font-semibold text-subtext1')
  node.textContent = (u.display_name || '?').trim().charAt(0).toUpperCase()
  return node
}

function persistCurrent() {
  const id = state.current.containerId
  if (!id || id === lastPersisted) return
  lastPersisted = id
  try {
    localStorage.setItem(LAST_CONTAINER_KEY, id)
  } catch {
    lastPersisted = null
  }
}

function render() {
  persistCurrent()
  renderChannels()
  renderConversations()
  renderFooter()
}

export function mount(root) {
  rootEl = root
  rootEl.classList.add(
    'fixed',
    'inset-y-0',
    'left-0',
    'z-40',
    'w-72',
    'transform',
    'transition-transform',
    'duration-200',
    '-translate-x-full',
    'md:static',
    'md:z-auto',
    'md:translate-x-0'
  )

  backdropEl = el('div', 'fixed inset-0 hidden bg-crust/70 md:hidden')
  backdropEl.addEventListener('click', closeDrawer)

  panelEl = el('div', 'relative z-10 flex h-full w-72 flex-col border-r border-surface0 bg-crust')

  const header = el('div', 'flex items-center justify-between px-3 py-3')
  header.appendChild(el('span', 'font-display text-lg font-semibold text-text', 'Isane'))
  const close = el('button', 'rounded-lg px-2 py-1 text-sm text-subtext0 hover:bg-surface0 md:hidden', 'Close')
  close.type = 'button'
  close.addEventListener('click', closeDrawer)
  header.appendChild(close)
  panelEl.appendChild(header)

  const searchWrap = el('div', 'px-3 pb-2')
  searchInput = el('input', 'w-full rounded-lg border border-surface1 bg-surface0 px-2 py-1.5 text-sm text-text placeholder:text-overlay0')
  searchInput.type = 'search'
  searchInput.placeholder = 'Search messages'
  searchInput.addEventListener('input', () => {
    clearTimeout(searchTimer)
    const q = searchInput.value.trim()
    searchTimer = setTimeout(() => runSearch(q), 250)
  })
  searchWrap.appendChild(searchInput)
  searchResultsEl = el('div', 'mt-2 hidden max-h-64 space-y-0.5 overflow-y-auto rounded-lg border border-surface0 bg-mantle p-1')
  searchWrap.appendChild(searchResultsEl)
  panelEl.appendChild(searchWrap)

  const nav = el('nav', 'flex-1 space-y-4 overflow-y-auto px-3 pb-3')

  const channelsGroup = el('div')
  channelsGroup.appendChild(el('p', 'px-2 pb-1 text-xs font-semibold uppercase tracking-wide text-overlay1', 'Channels'))
  channelsEl = el('div', 'space-y-0.5')
  channelsGroup.appendChild(channelsEl)
  nav.appendChild(channelsGroup)

  const conversationsGroup = el('div')
  const convHead = el('div', 'flex items-center justify-between px-2 pb-1')
  convHead.appendChild(el('p', 'text-xs font-semibold uppercase tracking-wide text-overlay1', 'Direct messages'))
  const newConv = el('button', 'rounded px-1 text-sm text-overlay1 hover:bg-surface0 hover:text-text', '+')
  newConv.type = 'button'
  newConv.title = 'New conversation'
  newConv.addEventListener('click', () => {
    pickerOpen = !pickerOpen
    renderPicker()
  })
  convHead.appendChild(newConv)
  conversationsGroup.appendChild(convHead)
  pickerEl = el('div', 'mb-2 hidden rounded-lg border border-surface0 bg-mantle p-2')
  conversationsGroup.appendChild(pickerEl)
  conversationsEl = el('div', 'space-y-0.5')
  conversationsGroup.appendChild(conversationsEl)
  nav.appendChild(conversationsGroup)

  archivedGroupEl = el('div', 'hidden')
  archivedGroupEl.appendChild(el('p', 'px-2 pb-1 text-xs font-semibold uppercase tracking-wide text-overlay1', 'Archived'))
  archivedEl = el('div', 'space-y-0.5')
  archivedGroupEl.appendChild(archivedEl)
  nav.appendChild(archivedGroupEl)

  panelEl.appendChild(nav)

  footerEl = el('div', 'border-t border-surface0 px-3 py-3')
  panelEl.appendChild(footerEl)

  rootEl.replaceChildren(backdropEl, panelEl)

  window.addEventListener('isane:sidebar-toggle', () => {
    if (rootEl.classList.contains('-translate-x-full')) openDrawer()
    else closeDrawer()
  })

  subscribe(scheduleRender)
  render()
}

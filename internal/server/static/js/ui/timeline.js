import * as socket from '../socket.js'
import { state, subscribe, notify, user, loadMessages, containerUnread, watchSends } from '../store.js'
import {
  containerLabel, dayLabel, drawIcons, el, emptyState, icon, iconButton, navigate,
} from './dom.js'
import * as call from './call.js'
import { followBottom } from './follow.js'
import { GROUP_WINDOW, messageNode, messageSignature } from './message.js'
import { openChannelSettings } from './settings.js'

const PAGE = 50
const NEAR_TOP = 300
const NEAR_BOTTOM = 120
const DIVIDER_LINGER = 4500

let scrollEl = null
let headerEl = null
let listEl = null
let activityEl = null
let emptyEl = null

const nodes = new Map()
const signatures = new WeakMap()
const paging = new Map()
const editing = new Set()

let mountedContainer = null
let headerSignature = ''
let observer = null
let follower = null
let frame = 0
let landing = false
let readTimer = 0
let seenSeq = 0
let sentSeq = 0
let dividerSeq = -1
let dividerTimer = 0
let jumpTarget = 0
let handledJump = ''
let readSidebarWidth = () => 0

const sentHere = watchSends((p) => p.container_id === state.current.containerId && !p.thread_root_id)

const ctx = {
  showThread: true,
  onReply(m) {
    state.current.replyToId = m.id
    notify('current')
  },
  onThread(rootId) {
    state.current.threadRootId = rootId
    navigate('/t/' + rootId)
    notify('current')
  },
  onJump(seq) {
    jumpTo(seq)
  },
  isEditing: (id) => editing.has(id),
  startEdit(id) {
    editing.add(id)
    rebuild(id)
  },
  endEdit(id) {
    editing.delete(id)
    rebuild(id)
  },
}

function scheduleRender() {
  if (frame) return
  frame = requestAnimationFrame(() => {
    frame = 0
    render()
  })
}

function landingTop(bottom) {
  const divider = landing ? nodes.get('new') : null
  if (!divider || !divider.isConnected) return bottom
  const top = divider.getBoundingClientRect().top - scrollEl.getBoundingClientRect().top + scrollEl.scrollTop
  return Math.min(bottom, top)
}

function endLanding() {
  if (!landing) return
  landing = false
  follower.refresh()
}

function currentContainer() {
  const id = state.current.containerId
  return id ? state.containers.get(id) : null
}

function pageState(id) {
  let p = paging.get(id)
  if (!p) {
    p = { loading: false, hasOlder: true, hasNewer: true, loaded: false }
    paging.set(id, p)
  }
  return p
}

function rebuild(id) {
  const node = nodes.get(id)
  if (node) node.remove()
  nodes.delete(id)
  render()
}

function separatorNode(view) {
  const wrap = el('div', 'flex items-center gap-2 px-4 py-4')
  wrap.setAttribute('role', 'separator')
  wrap.appendChild(el('span', 'h-px flex-1 bg-surface0'))
  wrap.appendChild(el('span', 'text-micro font-bold uppercase tracking-widest text-overlay1', view.label))
  wrap.appendChild(el('span', 'h-px flex-1 bg-surface0'))
  return wrap
}

function dividerNode() {
  const wrap = el('div', 'flex items-center gap-0 px-4 pb-1 pt-2')
  wrap.setAttribute('role', 'separator')
  wrap.setAttribute('aria-label', 'New messages')
  wrap.appendChild(el('span', 'h-px flex-1 bg-red'))
  const pill = el('button', 'rounded-full bg-red px-1.5 text-micro font-bold uppercase tracking-widest text-crust transition-colors hover:brightness-110 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve', 'New')
  pill.type = 'button'
  pill.title = 'Mark everything here as read'
  pill.addEventListener('click', dismissDivider)
  wrap.appendChild(pill)
  return wrap
}

function signatureOf(view) {
  if (view.kind === 'date') return 'date:' + view.label
  if (view.kind === 'new') return 'new'
  return messageSignature(view, ctx)
}

function nodeFor(view, previous) {
  const signature = signatureOf(view)
  if (previous && signatures.get(previous) === signature) return previous
  let node
  if (view.kind === 'date') node = separatorNode(view)
  else if (view.kind === 'new') node = dividerNode()
  else node = messageNode(view, ctx)
  if (view.kind === 'message' && view.m.seq) node.dataset.seq = String(view.m.seq)
  node.dataset.key = view.key
  signatures.set(node, signature)
  return node
}

function sameDay(a, b) {
  return new Date(a).toDateString() === new Date(b).toDateString()
}

function visibleMessages() {
  const cid = state.current.containerId
  if (!cid) return []
  const stored = state.messages.get(cid) || []
  const seen = new Set()
  for (const m of stored) if (m.client_id) seen.add(m.client_id)
  const ordered = stored.filter((m) => !m.thread_root_id)
  for (const p of state.pending.values()) {
    if (p.container_id !== cid) continue
    if (p.thread_root_id) continue
    if (seen.has(p.client_id)) continue
    ordered.push(p)
  }

  const out = []
  let previous = null
  let broke = true
  let dividerDrawn = false
  for (const m of ordered) {
    if (!previous || !sameDay(previous.created_at, m.created_at)) {
      out.push({ kind: 'date', key: 'date:' + new Date(m.created_at).toDateString(), label: dayLabel(m.created_at) })
      broke = true
    }
    if (dividerSeq >= 0 && !dividerDrawn && (m.seq || 0) > dividerSeq) {
      out.push({ kind: 'new', key: 'new' })
      dividerDrawn = true
      broke = true
    }
    const head = Boolean(m.is_system)
      || broke
      || !previous
      || Boolean(previous.is_system)
      || previous.author_id !== m.author_id
      || Boolean(m.reply_to_id)
      || new Date(m.created_at) - new Date(previous.created_at) > GROUP_WINDOW
    out.push({ kind: 'message', key: m.id || 'pending:' + m.client_id, m, head })
    previous = m
    broke = false
  }
  return out
}

function reconcile(list) {
  let cursor = listEl.firstChild
  for (const view of list) {
    const previous = nodes.get(view.key)
    const node = nodeFor(view, previous && previous.isConnected ? previous : null)
    if (node !== previous) {
      if (previous && previous.isConnected) {
        if (observer) observer.unobserve(previous)
        if (cursor === previous) cursor = cursor.nextSibling
        previous.remove()
      }
      nodes.set(view.key, node)
      if (observer && node.dataset.seq) observer.observe(node)
    }
    if (cursor === node) {
      cursor = cursor.nextSibling
      continue
    }
    listEl.insertBefore(node, cursor)
  }
  while (cursor) {
    const next = cursor.nextSibling
    nodes.delete(cursor.dataset.key)
    if (observer) observer.unobserve(cursor)
    cursor.remove()
    cursor = next
  }
}

function headerSignatureOf(c) {
  if (!c) return 'none'
  const live = state.calls.get(c.id)
  const panel = call.panelState()
  return [
    c.id, c.kind, c.slug, c.name, c.topic, c.level, c.archived_at,
    live ? live.id : '', live ? live.recording_state : '',
    panel.joined, panel.hidden,
    (c.participants || []).join(','),
  ].join('|')
}

export function setSidebarWidth(read) {
  readSidebarWidth = read
}

function toggleSidebar(button) {
  const app = document.getElementById('app')
  if (!app) return
  const open = app.dataset.sidebar !== 'closed'
  app.dataset.sidebar = open ? 'closed' : 'open'
  document.documentElement.style.setProperty('--sidebar-width', open ? '0px' : readSidebarWidth() + 'px')
  button.title = open ? 'Show the channel list' : 'Hide the channel list'
  button.setAttribute('aria-label', button.title)
  button.replaceChildren(icon(open ? 'panel-left-open' : 'panel-left-close', 'h-5 w-5'))
  drawIcons(button)
}

function callControl(c) {
  const live = state.calls.get(c.id)
  if (!live) {
    return iconButton('phone', 'Start a call', () =>
      window.dispatchEvent(new CustomEvent('isane:call-start', { detail: { containerId: c.id } })))
  }
  const panel = call.panelState()
  if (panel.joined && panel.containerId === c.id) return iconButton('phone-call', 'Show the call', () => call.reopen())
  const indicator = iconButton('phone-call', 'A call is live', null)
  indicator.disabled = true
  indicator.classList.remove('text-overlay1', 'hover:bg-surface0', 'hover:text-text')
  indicator.classList.add('text-teal')
  return indicator
}

function renderHeader() {
  if (!headerEl) return
  const c = currentContainer()
  const signature = headerSignatureOf(c)
  if (signature === headerSignature) return
  headerSignature = signature
  headerEl.replaceChildren()

  headerEl.appendChild(iconButton('menu', 'Channels', () => window.dispatchEvent(new CustomEvent('isane:sidebar-toggle')), 'md:hidden'))
  const collapsed = document.getElementById('app').dataset.sidebar === 'closed'
  const collapse = iconButton(collapsed ? 'panel-left-open' : 'panel-left-close',
    collapsed ? 'Show the channel list' : 'Hide the channel list', null, 'hidden md:grid')
  collapse.addEventListener('click', () => toggleSidebar(collapse))
  headerEl.appendChild(collapse)

  if (!c) {
    headerEl.appendChild(el('p', 'text-sm text-overlay1', 'Select a conversation'))
    drawIcons(headerEl)
    return
  }

  headerEl.appendChild(icon(c.kind === 'channel' ? 'hash' : 'users', 'h-5 w-5 shrink-0 text-overlay1'))
  headerEl.appendChild(el('h1', 'min-w-0 shrink truncate text-message font-semibold text-text', containerLabel(c, user)))

  if (c.archived_at) {
    headerEl.appendChild(el('span', 'shrink-0 rounded bg-surface1 px-1.5 text-micro font-semibold uppercase tracking-widest text-peach', 'Archived'))
  }

  if (c.topic) {
    headerEl.appendChild(el('span', 'mx-1 hidden h-6 w-px shrink-0 bg-surface0 md:block'))
    headerEl.appendChild(el('p', 'hidden min-w-0 flex-1 truncate text-sm text-overlay1 md:block', c.topic))
  }

  const actions = el('div', 'ml-auto flex shrink-0 items-center')
  if (!c.archived_at) actions.appendChild(callControl(c))
  const channel = c.kind === 'channel'
  actions.appendChild(iconButton(channel ? 'settings' : 'users-round', channel ? 'Channel settings' : 'Members', () => openChannelSettings(c)))
  headerEl.appendChild(actions)
  drawIcons(headerEl)
}

function renderActivity() {
  const cid = state.current.containerId
  const jobs = []
  for (const job of state.jobs.values()) {
    if (job.container_id === cid) jobs.push(job)
  }
  activityEl.replaceChildren()
  activityEl.classList.toggle('hidden', jobs.length === 0)
  if (!jobs.length) return
  for (const job of jobs) {
    const row = el('div', 'flex items-center gap-2 px-4 py-1 text-xs text-overlay1')
    row.appendChild(icon('loader-circle', 'h-3.5 w-3.5 animate-spin text-yellow'))
    row.appendChild(el('span', '', user(job.agent_id).display_name + ' is working'))
    activityEl.appendChild(row)
  }
  drawIcons(activityEl)
}

function urlJump() {
  return new URLSearchParams(location.search).get('m') || ''
}

function syncJumpFromUrl() {
  const raw = urlJump()
  if (raw === handledJump) return
  handledJump = raw
  const seq = Number(raw) || 0
  if (seq) jumpTo(seq)
}

function render() {
  const cid = state.current.containerId
  if (cid !== mountedContainer) {
    handledJump = urlJump()
    mountedContainer = cid
    nodes.clear()
    editing.clear()
    listEl.replaceChildren()
    follower.unfollow()
    seenSeq = 0
    const c = cid ? state.containers.get(cid) : null
    sentSeq = c ? (c.last_read_seq || 0) : 0
    clearTimeout(dividerTimer)
    dividerTimer = 0
    if (autoRead()) dividerSeq = c && (c.unread || 0) > 0 ? sentSeq : -1
    else dividerSeq = c ? sentSeq : -1
    landing = dividerSeq >= 0
    renderHeader()
    if (cid) openContainer(cid)
    return
  }

  renderHeader()
  renderActivity()
  syncJumpFromUrl()
  const list = visibleMessages()
  const hasRows = list.some((view) => view.kind === 'message')
  emptyEl.textContent = cid ? 'No messages yet. Say something.' : 'Pick a channel to start reading.'
  emptyEl.classList.toggle('hidden', hasRows)
  reconcile(list)
  if (jumpTarget) highlight(jumpTarget)
  if (!readTimer && newestSeq(list) > seenSeq) scheduleRead()
}

function newestSeq(list) {
  for (let i = list.length - 1; i >= 0; i--) {
    const view = list[i]
    if (view.kind === 'message' && view.m.seq) return view.m.seq
  }
  return 0
}

function highlight(seq) {
  const target = listEl.querySelector('[data-seq="' + seq + '"]')
  if (!target) return
  jumpTarget = 0
  landing = false
  target.scrollIntoView({ block: 'center' })
  follower.refresh()
  target.classList.add('bg-yellow/10')
  setTimeout(() => target.classList.remove('bg-yellow/10'), 2000)
}

async function loadFirst(cid, around) {
  const p = pageState(cid)
  p.loading = true
  try {
    const page = await loadMessages(cid, around ? { around, limit: PAGE } : { limit: PAGE })
    p.hasOlder = Boolean(around) || page.length >= PAGE
    p.hasNewer = Boolean(around)
    p.loaded = true
  } catch {
    p.hasOlder = false
  }
  p.loading = false
}

async function openContainer(cid) {
  const jump = Number(urlJump()) || 0

  if (jump) {
    await loadFirst(cid, jump)
    if (cid !== mountedContainer) return
    if (pageState(cid).loaded) jumpTarget = jump
    notify('messages')
    markOnOpen(cid, jump)
    return
  }

  if (!pageState(cid).loaded) {
    const c = state.containers.get(cid)
    const unread = c && dividerSeq >= 0 ? (c.last_seq || 0) - dividerSeq : 0
    await loadFirst(cid, unread > PAGE ? dividerSeq + 1 : 0)
    if (cid !== mountedContainer) return
  }
  render()
  follower.follow()
  markOnOpen(cid)
}

async function loadOlder() {
  const cid = state.current.containerId
  if (!cid) return
  const p = pageState(cid)
  if (p.loading || !p.hasOlder || !p.loaded) return
  const list = state.messages.get(cid) || []
  if (!list.length) return

  p.loading = true
  try {
    const page = await loadMessages(cid, { before: list[0].seq, limit: PAGE })
    p.hasOlder = page.length >= PAGE
  } catch {
    p.hasOlder = false
  }
  p.loading = false
  if (cid !== mountedContainer) return
  const edge = scrollEl.getBoundingClientRect().top
  const anchor = Array.from(listEl.querySelectorAll('[data-seq]')).find((node) => node.getBoundingClientRect().bottom > edge)
  const key = anchor ? anchor.dataset.key : ''
  const top = anchor ? anchor.getBoundingClientRect().top : 0
  render()
  const moved = nodes.get(key)
  if (moved && moved.isConnected) scrollEl.scrollTop += moved.getBoundingClientRect().top - top
}

async function loadNewer() {
  const cid = state.current.containerId
  if (!cid) return
  const c = state.containers.get(cid)
  const list = state.messages.get(cid) || []
  if (!c || !list.length) return
  const newest = list[list.length - 1].seq
  if (newest >= (c.last_seq || 0)) return
  const p = pageState(cid)
  if (p.loading || !p.hasNewer) return

  p.loading = true
  try {
    const page = await loadMessages(cid, { after: newest, limit: PAGE })
    p.hasNewer = page.length >= PAGE
    if (page.length) follower.unfollow()
  } catch {
    p.hasNewer = false
  }
  p.loading = false
  render()
}

function jumpTo(seq) {
  const cid = state.current.containerId
  if (!cid) return
  const c = state.containers.get(cid)
  const path = c && c.kind === 'channel' ? '/c/' + c.slug : '/d/' + cid
  navigate(path + '?m=' + seq)
  if (listEl.querySelector('[data-seq="' + seq + '"]')) {
    highlight(seq)
    markOnOpen(cid, seq)
    return
  }
  mountedContainer = null
  render()
}

function markRead(cid, seq) {
  sentSeq = seq
  socket.send('read', { container_id: cid, seq })
  const c = state.containers.get(cid)
  if (!c) return
  c.last_read_seq = seq
  c.unread = containerUnread(c, seq)
  if (seq >= (c.last_seq || 0)) c.mentions = 0
  notify('containers')
}

function autoRead() {
  return !state.me || state.me.mark_read_on_open !== false
}

function markOnOpen(cid, upTo) {
  if (!autoRead() || dividerSeq < 0 || cid !== state.current.containerId) return
  const c = state.containers.get(cid)
  const last = c ? (c.last_seq || 0) : 0
  const seq = upTo && upTo < last ? upTo : last
  if (seq <= sentSeq) return
  markRead(cid, seq)
  if (seq < last) return
  clearTimeout(dividerTimer)
  dividerTimer = setTimeout(() => {
    dividerTimer = 0
    if (cid !== state.current.containerId) return
    endLanding()
    dividerSeq = -1
    render()
  }, DIVIDER_LINGER)
}

function scanVisible() {
  const box = scrollEl.getBoundingClientRect()
  for (const node of nodes.values()) {
    const seq = Number(node.dataset.seq) || 0
    if (seq <= seenSeq) continue
    const rect = node.getBoundingClientRect()
    if (!rect.height) continue
    const shown = Math.min(rect.bottom, box.bottom) - Math.max(rect.top, box.top)
    if (shown / rect.height >= 0.5) seenSeq = seq
  }
}

function flushRead() {
  readTimer = 0
  if (!autoRead()) return
  const cid = state.current.containerId
  if (!cid) return
  if (document.visibilityState !== 'visible') return
  scanVisible()
  if (seenSeq <= sentSeq) return
  markRead(cid, seenSeq)
}

function dismissDivider() {
  const cid = state.current.containerId
  if (!cid) return
  endLanding()
  const c = state.containers.get(cid)
  markRead(cid, c ? (c.last_seq || 0) : 0)
  dividerSeq = autoRead() ? -1 : sentSeq
  render()
}

function scheduleRead() {
  clearTimeout(readTimer)
  readTimer = setTimeout(flushRead, 600)
}

export function mount(root) {
  scrollEl = root
  headerEl = document.getElementById('timeline-header')

  listEl = el('div', 'flex flex-col pb-4')
  emptyEl = emptyState('No messages yet. Say something.')
  emptyEl.classList.add('hidden')
  activityEl = el('div', 'sticky bottom-0 hidden bg-mantle pb-1')
  scrollEl.replaceChildren(emptyEl, listEl, activityEl)

  observer = new IntersectionObserver(
    (entries) => {
      let changed = false
      for (const entry of entries) {
        if (!entry.isIntersecting) continue
        const seq = Number(entry.target.dataset.seq) || 0
        if (seq > seenSeq) {
          seenSeq = seq
          changed = true
        }
      }
      if (changed) scheduleRead()
    },
    { root: scrollEl, threshold: 0.5 }
  )

  follower = followBottom(scrollEl, { target: landingTop, onGesture: endLanding })
  follower.observe(listEl)
  follower.observe(activityEl)

  scrollEl.addEventListener('scroll', () => {
    if (scrollEl.scrollTop < NEAR_TOP) loadOlder()
    if (scrollEl.scrollHeight - scrollEl.scrollTop - scrollEl.clientHeight < NEAR_BOTTOM) loadNewer()
  })

  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') scheduleRead()
  })

  window.addEventListener('popstate', () => {
    handledJump = ''
    syncJumpFromUrl()
  })

  subscribe((keys) => {
    if (keys.includes('pending') && sentHere()) {
      landing = false
      follower.follow()
    }
    scheduleRender()
  })
  render()
}

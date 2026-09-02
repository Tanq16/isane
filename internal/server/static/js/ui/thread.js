import { state, subscribe, notify, user, findMessage, loadThread } from '../store.js'
import * as socket from '../socket.js'
import { renderInto } from '../render.js'

const DRAFT_PREFIX = 'isane:draft:thread:'
const PAGE = 100

let rootEl = null
let panelEl = null
let headerEl = null
let bodyEl = null
let rootBlockEl = null
let dividerEl = null
let listEl = null
let composerEl = null
let textarea = null
let sendButton = null
let noticeEl = null

const nodes = new Map()
const bodyText = new WeakMap()
const loaded = new Set()
const loading = new Set()

let mountedRoot = null
let frame = 0
let draftTimer = 0

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

function timeLabel(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  const now = new Date()
  if (d.toDateString() === now.toDateString()) {
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  return d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function sizeLabel(n) {
  const units = ['B', 'KB', 'MB', 'GB']
  let value = Number(n) || 0
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i += 1
  }
  return (i === 0 ? value : value.toFixed(1)) + ' ' + units[i]
}

function avatarNode(u) {
  if (u.avatar_id) {
    const img = el('img', 'h-7 w-7 shrink-0 rounded-lg object-cover')
    img.src = '/api/attachments/' + u.avatar_id + '/thumb'
    img.alt = ''
    return img
  }
  const node = el('div', 'flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-surface1 text-xs font-semibold text-subtext1')
  node.textContent = (u.display_name || '?').trim().charAt(0).toUpperCase()
  return node
}

function attachmentNode(a) {
  if (a.state !== 'ready') {
    const wrap = el('div', 'mt-2 flex items-center gap-2 rounded-lg border border-surface0 bg-base px-2 py-1 text-xs')
    wrap.appendChild(el('span', 'truncate text-subtext0', a.original_name || 'Attachment'))
    wrap.appendChild(el('span', a.state === 'failed' ? 'text-red' : 'text-overlay1', a.state === 'failed' ? 'failed' : 'processing'))
    return wrap
  }

  const href = '/api/attachments/' + a.id

  if (a.kind === 'image') {
    const link = el('a', 'mt-2 block overflow-hidden rounded-lg border border-surface0')
    link.href = href
    link.target = '_blank'
    link.rel = 'noreferrer'
    const img = el('img', 'block h-auto w-full')
    img.src = href + '/thumb'
    img.alt = a.original_name || ''
    img.loading = 'lazy'
    link.appendChild(img)
    return link
  }

  if (a.kind === 'video') {
    const video = el('video', 'mt-2 block w-full rounded-lg border border-surface0')
    video.controls = true
    video.preload = 'metadata'
    video.poster = href + '/thumb'
    video.src = href
    return video
  }

  if (a.kind === 'audio') {
    const audio = el('audio', 'mt-2 w-full')
    audio.controls = true
    audio.preload = 'metadata'
    audio.src = href
    return audio
  }

  const row = el('a', 'mt-2 flex items-center gap-2 rounded-lg border border-surface0 bg-base px-2 py-1 hover:border-surface1')
  row.href = href
  row.download = a.original_name || ''
  const meta = el('div', 'min-w-0 flex-1')
  meta.appendChild(el('p', 'truncate text-xs text-text', a.original_name || 'File'))
  meta.appendChild(el('p', 'text-xs text-overlay1', sizeLabel(a.size_bytes)))
  row.appendChild(meta)
  row.appendChild(el('span', 'shrink-0 text-xs text-blue', 'Download'))
  return row
}

function messageNode(m) {
  const article = el('article', 'px-3 py-1.5')
  article.dataset.key = m.id || 'pending:' + m.client_id

  if (m.deleted_at) {
    article.appendChild(el('p', 'pl-10 text-sm italic text-overlay1', 'Message deleted'))
    return article
  }

  const row = el('div', 'flex gap-2')
  const author = user(m.author_id)
  row.appendChild(avatarNode(author))

  const main = el('div', 'min-w-0 flex-1')
  const head = el('div', 'flex flex-wrap items-baseline gap-2')
  head.appendChild(el('span', 'text-sm font-semibold text-text', author.display_name))
  if (author.kind === 'agent') {
    head.appendChild(el('span', 'rounded bg-surface1 px-1.5 py-0.5 text-xs text-lavender', 'agent'))
  }
  head.appendChild(el('time', 'text-xs text-overlay1', timeLabel(m.created_at)))
  if (m.edited_at) head.appendChild(el('span', 'text-xs text-overlay1', 'edited'))
  if (m.pending) head.appendChild(el('span', 'text-xs text-yellow', 'sending'))
  if (m.failed) head.appendChild(el('span', 'text-xs text-red', 'failed to send'))
  main.appendChild(head)

  const body = el('div', 'markdown-body text-sm text-subtext0')
  main.appendChild(body)
  renderInto(body, m.body || '')

  for (const a of m.attachments || []) main.appendChild(attachmentNode(a))

  row.appendChild(main)
  article.appendChild(row)
  return article
}

function nodeFor(m, previous) {
  const signature = [m.body, m.edited_at, m.deleted_at, m.pending, m.failed, (m.attachments || []).map((a) => a.id + ':' + a.state).join(',')].join('|')
  if (previous && bodyText.get(previous) === signature) return previous
  const node = messageNode(m)
  bodyText.set(node, signature)
  return node
}

function replies(rootId) {
  const stored = state.threads.get(rootId) || []
  const seen = new Set()
  for (const m of stored) if (m.client_id) seen.add(m.client_id)
  const out = stored.slice()
  for (const p of state.pending.values()) {
    if (p.thread_root_id !== rootId) continue
    if (seen.has(p.client_id)) continue
    out.push(p)
  }
  return out
}

function renderRootBlock(rootId) {
  const m = findMessage(rootId)
  if (!m) {
    if (nodes.has('root')) nodes.delete('root')
    rootBlockEl.replaceChildren(el('p', 'px-3 py-3 text-sm text-overlay1', 'The parent message is not loaded. Open its channel to see it.'))
    return
  }
  const previous = nodes.get('root')
  const node = nodeFor(m, previous && previous.isConnected ? previous : null)
  if (node === previous) return
  nodes.set('root', node)
  rootBlockEl.replaceChildren(node)
}

function reconcile(list) {
  let cursor = listEl.firstChild
  for (const m of list) {
    const key = m.id || 'pending:' + m.client_id
    const previous = nodes.get(key)
    const node = nodeFor(m, previous && previous.isConnected ? previous : null)
    if (node !== previous) {
      if (previous && previous.isConnected) {
        if (cursor === previous) cursor = cursor.nextSibling
        previous.remove()
      }
      nodes.set(key, node)
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
    cursor.remove()
    cursor = next
  }
}

function draftKey(rootId) {
  return DRAFT_PREFIX + rootId
}

function loadDraft(rootId) {
  try {
    return localStorage.getItem(draftKey(rootId)) || ''
  } catch {
    return ''
  }
}

function saveDraft(rootId, value) {
  try {
    if (value) localStorage.setItem(draftKey(rootId), value)
    else localStorage.removeItem(draftKey(rootId))
  } catch {}
}

function close() {
  const rootId = state.current.threadRootId
  if (rootId) saveDraft(rootId, textarea.value)
  state.current.threadRootId = null
  const cid = state.current.containerId
  const c = cid ? state.containers.get(cid) : null
  if (c) navigate(c.kind === 'channel' ? '/c/' + c.slug : '/d/' + c.id)
  notify('current')
}

function toggleMute(rootId) {
  const next = state.threadSubs.get(rootId) === 'muted' ? 'subscribed' : 'muted'
  state.threadSubs.set(rootId, next)
  socket.send('thread_sub', { thread_root_id: rootId, state: next })
  renderHeader(rootId)
}

function renderHeader(rootId) {
  headerEl.replaceChildren()
  const bar = el('div', 'flex items-center gap-2 border-b border-surface0 px-3 py-3')
  bar.appendChild(el('h2', 'flex-1 font-display text-sm font-semibold text-text', 'Thread'))

  const muteButton = el('button', 'rounded-lg border border-surface1 px-2 py-1 text-xs text-subtext0 hover:bg-surface0', '')
  muteButton.type = 'button'
  muteButton.textContent = state.threadSubs.get(rootId) === 'muted' ? 'Unmute' : 'Mute'
  muteButton.addEventListener('click', () => toggleMute(rootId))
  bar.appendChild(muteButton)

  const closeButton = el('button', 'rounded-lg px-2 py-1 text-xs text-subtext0 hover:bg-surface0 hover:text-text', 'Close')
  closeButton.type = 'button'
  closeButton.addEventListener('click', close)
  bar.appendChild(closeButton)

  headerEl.appendChild(bar)
}

async function load(rootId) {
  if (loading.has(rootId) || loaded.has(rootId)) return
  loading.add(rootId)
  let after = 0
  try {
    for (;;) {
      const page = await loadThread(rootId, { after, limit: PAGE })
      if (page.messages.length < PAGE) break
      after = page.messages[page.messages.length - 1].seq
    }
    loaded.add(rootId)
  } catch (err) {
    console.error('thread load failed', rootId, err)
  }
  loading.delete(rootId)
  notify('threads')
}

function submit() {
  const rootId = state.current.threadRootId
  if (!rootId) return
  const root = findMessage(rootId)
  const cid = (root && root.container_id) || state.current.containerId
  const c = cid ? state.containers.get(cid) : null
  if (!c || c.archived_at) return
  const body = textarea.value.trim()
  if (!body) return

  const clientId = crypto.randomUUID()
  state.pending.set(clientId, {
    id: 'pending:' + clientId,
    container_id: cid,
    client_id: clientId,
    author_id: state.me ? state.me.id : null,
    body,
    reply_to_id: null,
    thread_root_id: rootId,
    thread_reply_count: 0,
    created_at: new Date().toISOString(),
    attachments: [],
    pending: true,
  })

  socket.send('send', { container_id: cid, client_id: clientId, body, thread_root_id: rootId })

  textarea.value = ''
  textarea.style.height = 'auto'
  saveDraft(rootId, '')
  sendButton.disabled = true
  notify('pending')
}

function render() {
  const rootId = state.current.threadRootId
  if (!rootId) {
    if (mountedRoot) saveDraft(mountedRoot, textarea.value)
    mountedRoot = null
    rootEl.classList.add('hidden')
    return
  }
  rootEl.classList.remove('hidden')

  if (rootId !== mountedRoot) {
    if (mountedRoot) saveDraft(mountedRoot, textarea.value)
    mountedRoot = rootId
    nodes.clear()
    listEl.replaceChildren()
    textarea.value = loadDraft(rootId)
    sendButton.disabled = !textarea.value.trim()
    renderHeader(rootId)
    load(rootId)
  }

  renderRootBlock(rootId)
  const list = replies(rootId)
  dividerEl.textContent = list.length === 1 ? '1 reply' : list.length + ' replies'
  reconcile(list)

  const root = findMessage(rootId)
  const cid = (root && root.container_id) || state.current.containerId
  const c = cid ? state.containers.get(cid) : null
  const archived = Boolean(c && c.archived_at)
  noticeEl.classList.toggle('hidden', !archived)
  composerEl.classList.toggle('hidden', archived)
}

export function mount(root) {
  rootEl = root
  rootEl.classList.add('hidden')

  panelEl = el('div', 'fixed inset-0 z-30 flex flex-col bg-mantle md:static md:z-auto md:w-96 md:shrink-0 md:border-l md:border-surface0')

  headerEl = el('div')
  panelEl.appendChild(headerEl)

  bodyEl = el('div', 'flex-1 overflow-y-auto')
  rootBlockEl = el('div', 'border-b border-surface0 pb-2 pt-2')
  bodyEl.appendChild(rootBlockEl)
  dividerEl = el('p', 'px-3 py-2 text-xs font-semibold uppercase tracking-wide text-overlay1')
  bodyEl.appendChild(dividerEl)
  listEl = el('div', 'flex flex-col pb-4')
  bodyEl.appendChild(listEl)
  panelEl.appendChild(bodyEl)

  noticeEl = el(
    'p',
    'hidden border-t border-surface0 px-3 py-3 text-sm text-overlay1',
    'This channel is archived. History stays readable and new replies are blocked.'
  )
  panelEl.appendChild(noticeEl)

  composerEl = el('div', 'border-t border-surface0 p-3')
  const box = el('div', 'rounded-xl border border-surface1 bg-surface0 focus-within:border-mauve')
  textarea = el('textarea', 'block max-h-48 w-full resize-none bg-transparent px-3 pt-3 text-sm text-text placeholder:text-overlay0 focus:outline-none')
  textarea.rows = 1
  textarea.placeholder = 'Reply in thread'
  box.appendChild(textarea)
  const bar = el('div', 'flex items-center gap-2 px-3 pb-2 pt-1')
  bar.appendChild(el('span', 'flex-1 text-xs text-overlay1', 'Enter sends'))
  sendButton = el('button', 'rounded-lg bg-mauve px-3 py-1.5 text-xs font-semibold text-crust disabled:opacity-40', 'Send')
  sendButton.type = 'button'
  sendButton.disabled = true
  sendButton.addEventListener('click', submit)
  bar.appendChild(sendButton)
  box.appendChild(bar)
  composerEl.appendChild(box)
  panelEl.appendChild(composerEl)

  rootEl.replaceChildren(panelEl)

  textarea.addEventListener('input', () => {
    textarea.style.height = 'auto'
    textarea.style.height = Math.min(textarea.scrollHeight, 192) + 'px'
    sendButton.disabled = !textarea.value.trim()
    clearTimeout(draftTimer)
    const rootId = state.current.threadRootId
    draftTimer = setTimeout(() => saveDraft(rootId, textarea.value), 300)
  })

  textarea.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      submit()
      return
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      close()
    }
  })

  window.addEventListener('pagehide', () => {
    if (mountedRoot) saveDraft(mountedRoot, textarea.value)
  })

  socket.on('message', (m) => {
    if (!m.client_id) return
    if (state.pending.delete(m.client_id)) notify('pending')
  })

  subscribe(scheduleRender)
  render()
}

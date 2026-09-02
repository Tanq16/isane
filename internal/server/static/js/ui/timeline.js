import { state, subscribe, notify, user, findMessage, loadMessages } from '../store.js'
import * as api from '../api.js'
import * as socket from '../socket.js'
import { renderInto, plainText } from '../render.js'

const PAGE = 50
const NEAR_TOP = 300
const NEAR_BOTTOM = 120

let scrollEl = null
let headerEl = null
let listEl = null
let emptyEl = null

const nodes = new Map()
const signatures = new WeakMap()
const bodyText = new WeakMap()
const paging = new Map()
const editing = new Set()

let mountedContainer = null
let headerSignature = ''
let observer = null
let frame = 0
let atBottom = true
let readTimer = 0
let seenSeq = 0
let sentSeq = 0
let jumpTarget = 0

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

function relativeLabel(iso) {
  if (!iso) return ''
  const seconds = (Date.now() - new Date(iso).getTime()) / 1000
  if (seconds < 60) return 'just now'
  if (seconds < 3600) return Math.floor(seconds / 60) + 'm ago'
  if (seconds < 86400) return Math.floor(seconds / 3600) + 'h ago'
  return Math.floor(seconds / 86400) + 'd ago'
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

function currentContainer() {
  const id = state.current.containerId
  return id ? state.containers.get(id) : null
}

function conversationTitle(c) {
  const ids = (c.participants || []).filter((id) => id !== (state.me && state.me.id))
  if (!ids.length) return c.name || 'Conversation'
  return ids.map((id) => user(id).display_name).join(', ')
}

function pageState(id) {
  let p = paging.get(id)
  if (!p) {
    p = { loading: false, hasOlder: true, hasNewer: true, loaded: false }
    paging.set(id, p)
  }
  return p
}

function avatarNode(u, size) {
  if (u.avatar_id) {
    const img = el('img', size + ' shrink-0 rounded-lg object-cover')
    img.src = '/api/attachments/' + u.avatar_id + '/thumb'
    img.alt = ''
    return img
  }
  const node = el('div', size + ' flex shrink-0 items-center justify-center rounded-lg bg-surface1 text-sm font-semibold text-subtext1')
  node.textContent = (u.display_name || '?').trim().charAt(0).toUpperCase()
  return node
}

function attachmentNode(a) {
  if (a.state !== 'ready') {
    const wrap = el('div', 'mt-2 flex items-center gap-2 rounded-lg border border-surface0 bg-base px-3 py-2 text-xs')
    wrap.appendChild(el('span', 'truncate text-subtext0', a.original_name || 'Attachment'))
    wrap.appendChild(el('span', a.state === 'failed' ? 'text-red' : 'text-overlay1', a.state === 'failed' ? 'failed' : 'processing'))
    return wrap
  }

  const href = '/api/attachments/' + a.id

  if (a.kind === 'image') {
    const link = el('a', 'mt-2 block max-w-md overflow-hidden rounded-lg border border-surface0')
    link.href = href
    link.target = '_blank'
    link.rel = 'noreferrer'
    const img = el('img', 'block h-auto w-full')
    img.src = href + '/thumb'
    img.alt = a.original_name || ''
    img.loading = 'lazy'
    if (a.width && a.height) {
      img.width = a.width
      img.height = a.height
    }
    link.appendChild(img)
    return link
  }

  if (a.kind === 'video') {
    const video = el('video', 'mt-2 block w-full max-w-md rounded-lg border border-surface0')
    video.controls = true
    video.preload = 'metadata'
    video.poster = href + '/thumb'
    video.src = href
    return video
  }

  if (a.kind === 'audio') {
    const wrap = el('div', 'mt-2 max-w-md rounded-lg border border-surface0 bg-base p-2')
    wrap.appendChild(el('p', 'truncate text-xs text-subtext0', a.original_name || 'Audio'))
    const audio = el('audio', 'mt-1 w-full')
    audio.controls = true
    audio.preload = 'metadata'
    audio.src = href
    wrap.appendChild(audio)
    return wrap
  }

  const row = el('a', 'mt-2 flex max-w-md items-center gap-3 rounded-lg border border-surface0 bg-base px-3 py-2 hover:border-surface1')
  row.href = href
  row.download = a.original_name || ''
  const meta = el('div', 'min-w-0 flex-1')
  meta.appendChild(el('p', 'truncate text-sm text-text', a.original_name || 'File'))
  meta.appendChild(el('p', 'text-xs text-overlay1', sizeLabel(a.size_bytes)))
  row.appendChild(meta)
  row.appendChild(el('span', 'shrink-0 text-xs text-blue', 'Download'))
  return row
}

function parentOf(m) {
  if (!m.reply_to_id) return null
  return findMessage(m.reply_to_id)
}

function replyQuoteNode(m) {
  const parent = parentOf(m)
  const wrap = el('div', 'mb-1 flex items-baseline gap-2 border-l-2 border-surface1 pl-2 text-xs')
  if (!parent) {
    wrap.appendChild(el('span', 'text-overlay1', 'Reply to an earlier message'))
    return wrap
  }
  wrap.appendChild(el('span', 'shrink-0 font-semibold text-subtext1', user(parent.author_id).display_name))
  const excerpt = plainText(parent.body || '').replace(/\s+/g, ' ').trim()
  wrap.appendChild(el('span', 'truncate text-overlay1', excerpt.length > 140 ? excerpt.slice(0, 140) + '...' : excerpt))
  if (parent.seq) {
    wrap.classList.add('cursor-pointer')
    wrap.addEventListener('click', () => jumpTo(parent.seq))
  }
  return wrap
}

function threadSummaryNode(m) {
  const button = el('button', 'mt-2 flex items-center gap-2 rounded-lg border border-surface0 bg-mantle px-3 py-1.5 text-xs hover:border-surface1')
  button.type = 'button'
  const count = m.thread_reply_count
  button.appendChild(el('span', 'font-semibold text-blue', count === 1 ? '1 reply' : count + ' replies'))
  if (m.thread_last_reply_at) {
    button.appendChild(el('span', 'text-overlay1', 'last ' + relativeLabel(m.thread_last_reply_at)))
  }
  button.addEventListener('click', () => openThread(m.id))
  return button
}

function openThread(rootId) {
  state.current.threadRootId = rootId
  navigate('/t/' + rootId)
  notify('current')
}

function startReply(m) {
  state.current.replyToId = m.id
  notify('current')
}

function rebuild(id) {
  const node = nodes.get(id)
  if (node) node.remove()
  nodes.delete(id)
  render()
}

function actionsNode(m) {
  const wrap = el('div', 'absolute right-3 top-1 hidden gap-1 rounded-lg border border-surface0 bg-mantle p-0.5 group-hover:flex')
  const mine = state.me && m.author_id === state.me.id
  const admin = state.me && state.me.is_admin

  const add = (label, fn, cls) => {
    const button = el('button', cls || 'rounded px-2 py-0.5 text-xs text-subtext0 hover:bg-surface0 hover:text-text', label)
    button.type = 'button'
    button.addEventListener('click', fn)
    wrap.appendChild(button)
  }

  add('Reply', () => startReply(m))
  add('Thread', () => openThread(m.thread_root_id || m.id))
  if (mine) {
    add('Edit', () => {
      editing.add(m.id)
      rebuild(m.id)
    })
  }
  if (mine || admin) {
    add(
      'Delete',
      () => {
        if (window.confirm('Delete this message?')) socket.send('delete', { message_id: m.id })
      },
      'rounded px-2 py-0.5 text-xs text-red hover:bg-surface0'
    )
  }
  return wrap
}

function editorNode(m) {
  const wrap = el('div', 'mt-1')
  const box = el(
    'textarea',
    'w-full resize-y rounded-lg border border-surface1 bg-surface0 px-3 py-2 font-mono text-sm text-text focus:border-mauve focus:outline-none'
  )
  box.value = m.body || ''
  box.rows = Math.min(12, (m.body || '').split('\n').length + 1)

  const commit = () => {
    const body = box.value.trim()
    if (body && body !== m.body) socket.send('edit', { message_id: m.id, body })
    editing.delete(m.id)
    rebuild(m.id)
  }
  const abandon = () => {
    editing.delete(m.id)
    rebuild(m.id)
  }

  const actions = el('div', 'mt-1 flex gap-2')
  const save = el('button', 'rounded-lg bg-mauve px-3 py-1 text-xs font-semibold text-crust', 'Save')
  save.type = 'button'
  save.addEventListener('click', commit)
  const cancel = el('button', 'rounded-lg border border-surface1 px-3 py-1 text-xs text-subtext0 hover:bg-surface0', 'Cancel')
  cancel.type = 'button'
  cancel.addEventListener('click', abandon)
  actions.appendChild(save)
  actions.appendChild(cancel)

  box.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      abandon()
    }
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      commit()
    }
  })

  wrap.appendChild(box)
  wrap.appendChild(actions)
  queueMicrotask(() => box.focus())
  return wrap
}

function signatureOf(m) {
  return [
    m.body,
    m.edited_at,
    m.deleted_at,
    m.thread_reply_count,
    m.thread_last_reply_at,
    m.pending,
    m.failed,
    editing.has(m.id),
    (m.attachments || []).map((a) => a.id + ':' + a.state).join(','),
  ].join('|')
}

function messageNode(m, reusableBody) {
  const article = el('article', 'group relative px-4 py-1.5 hover:bg-mantle')
  article.dataset.key = m.id || 'pending:' + m.client_id
  if (m.seq) article.dataset.seq = String(m.seq)

  if (m.deleted_at) {
    article.appendChild(el('p', 'pl-11 text-sm italic text-overlay1', 'Message deleted'))
    return article
  }

  if (m.reply_to_id) article.appendChild(replyQuoteNode(m))

  const row = el('div', 'flex gap-3')
  const author = user(m.author_id)
  row.appendChild(avatarNode(author, 'h-8 w-8'))

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

  if (editing.has(m.id)) {
    main.appendChild(editorNode(m))
  } else if (reusableBody) {
    main.appendChild(reusableBody)
  } else {
    const body = el('div', 'markdown-body text-sm text-subtext0')
    main.appendChild(body)
    renderInto(body, m.body || '')
  }

  const attachments = m.attachments || []
  if (attachments.length) {
    const wrap = el('div', 'flex flex-col items-start')
    for (const a of attachments) wrap.appendChild(attachmentNode(a))
    main.appendChild(wrap)
  }

  if (m.thread_reply_count > 0) main.appendChild(threadSummaryNode(m))

  row.appendChild(main)
  article.appendChild(row)
  if (!m.pending) article.appendChild(actionsNode(m))
  return article
}

function nodeFor(m, previous) {
  const signature = signatureOf(m)
  if (previous && signatures.get(previous) === signature) return previous
  const body = previous && bodyText.get(previous) === (m.body || '') ? previous.querySelector('.markdown-body') : null
  const node = messageNode(m, body)
  signatures.set(node, signature)
  bodyText.set(node, m.body || '')
  return node
}

function visibleMessages() {
  const cid = state.current.containerId
  if (!cid) return []
  const stored = state.messages.get(cid) || []
  const seen = new Set()
  for (const m of stored) if (m.client_id) seen.add(m.client_id)
  const out = stored.slice()
  for (const p of state.pending.values()) {
    if (p.container_id !== cid) continue
    if (p.thread_root_id) continue
    if (seen.has(p.client_id)) continue
    out.push(p)
  }
  return out
}

function reconcile(list) {
  let cursor = listEl.firstChild
  for (const m of list) {
    const key = m.id || 'pending:' + m.client_id
    const previous = nodes.get(key)
    const node = nodeFor(m, previous && previous.isConnected ? previous : null)
    if (node !== previous) {
      if (previous && previous.isConnected) {
        if (observer) observer.unobserve(previous)
        if (cursor === previous) cursor = cursor.nextSibling
        previous.remove()
      }
      nodes.set(key, node)
      if (observer && m.seq) observer.observe(node)
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
  return [c.id, c.kind, c.slug, c.name, c.topic, c.level, c.archived_at, (c.participants || []).join(',')].join('|')
}

function renderHeader() {
  if (!headerEl) return
  const c = currentContainer()
  const signature = headerSignatureOf(c)
  if (signature === headerSignature) return
  headerSignature = signature
  headerEl.replaceChildren()

  const bar = el('div', 'flex items-center gap-3 border-b border-surface0 bg-crust px-4 py-3')

  const menu = el('button', 'rounded-lg px-2 py-1 text-sm text-subtext0 hover:bg-surface0 md:hidden', 'Menu')
  menu.type = 'button'
  menu.addEventListener('click', () => window.dispatchEvent(new CustomEvent('isane:sidebar-toggle')))
  bar.appendChild(menu)

  const title = el('div', 'min-w-0 flex-1')
  if (!c) {
    title.appendChild(el('p', 'text-sm text-overlay1', 'Select a conversation'))
  } else {
    const line = el('div', 'flex items-baseline gap-2')
    line.appendChild(
      el('h1', 'truncate font-display text-sm font-semibold text-text', c.kind === 'channel' ? '#' + c.slug : conversationTitle(c))
    )
    if (c.archived_at) line.appendChild(el('span', 'shrink-0 rounded bg-surface1 px-1.5 py-0.5 text-xs text-peach', 'archived'))
    title.appendChild(line)
    if (c.topic) title.appendChild(el('p', 'truncate text-xs text-overlay1', c.topic))
  }
  bar.appendChild(title)

  if (c && c.kind === 'channel') {
    const select = el('select', 'rounded-lg border border-surface1 bg-surface0 px-2 py-1 text-xs text-subtext0')
    for (const pair of [['all', 'All messages'], ['mentions', 'Mentions only'], ['none', 'Nothing']]) {
      const option = el('option', null, pair[1])
      option.value = pair[0]
      select.appendChild(option)
    }
    select.value = c.level || 'mentions'
    select.addEventListener('change', async () => {
      const level = select.value
      try {
        await api.put('/api/containers/' + c.id + '/notification-pref', { level })
        c.level = level
        notify('containers')
      } catch {
        select.value = c.level || 'mentions'
      }
    })
    bar.appendChild(select)
  }

  if (c && !c.archived_at) {
    const call = el('button', 'rounded-lg border border-surface1 px-3 py-1 text-xs text-subtext0 hover:bg-surface0 hover:text-text', 'Call')
    call.type = 'button'
    call.addEventListener('click', () =>
      window.dispatchEvent(new CustomEvent('isane:call-start', { detail: { containerId: c.id } }))
    )
    bar.appendChild(call)
  }

  headerEl.appendChild(bar)
}

function render() {
  const cid = state.current.containerId
  if (cid !== mountedContainer) {
    mountedContainer = cid
    nodes.clear()
    editing.clear()
    listEl.replaceChildren()
    atBottom = true
    seenSeq = 0
    const c = cid ? state.containers.get(cid) : null
    sentSeq = c ? Math.max(0, (c.last_seq || 0) - (c.unread || 0)) : 0
    renderHeader()
    if (cid) openContainer(cid)
    return
  }

  renderHeader()
  const list = visibleMessages()
  emptyEl.classList.toggle('hidden', list.length > 0 || !cid)
  const stick = atBottom
  reconcile(list)
  if (stick) scrollEl.scrollTop = scrollEl.scrollHeight
  if (jumpTarget) highlight(jumpTarget)
}

function highlight(seq) {
  const target = listEl.querySelector('[data-seq="' + seq + '"]')
  if (!target) return
  jumpTarget = 0
  target.scrollIntoView({ block: 'center' })
  target.classList.add('rounded-lg', 'ring-2', 'ring-mauve')
  setTimeout(() => target.classList.remove('rounded-lg', 'ring-2', 'ring-mauve'), 2000)
}

async function openContainer(cid) {
  const p = pageState(cid)
  const jump = Number(new URLSearchParams(location.search).get('m')) || 0

  if (jump) {
    p.loading = true
    try {
      const page = await loadMessages(cid, { around: jump, limit: PAGE })
      p.hasOlder = page.length >= PAGE
      p.loaded = true
      jumpTarget = jump
      atBottom = false
    } catch {
      p.hasOlder = false
    }
    p.loading = false
    notify('messages')
    return
  }

  if (p.loaded) {
    render()
    scrollEl.scrollTop = scrollEl.scrollHeight
    return
  }

  p.loading = true
  try {
    const page = await loadMessages(cid, { limit: PAGE })
    p.hasOlder = page.length >= PAGE
    p.loaded = true
  } catch {
    p.hasOlder = false
  }
  p.loading = false
  atBottom = true
  render()
  scrollEl.scrollTop = scrollEl.scrollHeight
}

async function loadOlder() {
  const cid = state.current.containerId
  if (!cid) return
  const p = pageState(cid)
  if (p.loading || !p.hasOlder || !p.loaded) return
  const list = state.messages.get(cid) || []
  if (!list.length) return

  p.loading = true
  const previousHeight = scrollEl.scrollHeight
  try {
    const page = await loadMessages(cid, { before: list[0].seq, limit: PAGE })
    p.hasOlder = page.length >= PAGE
  } catch {
    p.hasOlder = false
  }
  p.loading = false
  render()
  scrollEl.scrollTop += scrollEl.scrollHeight - previousHeight
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
    await loadMessages(cid, { after: newest, limit: PAGE })
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
    return
  }
  mountedContainer = null
  render()
}

function flushRead() {
  const cid = state.current.containerId
  if (!cid || seenSeq <= sentSeq) return
  if (document.visibilityState !== 'visible') return
  sentSeq = seenSeq
  socket.send('read', { container_id: cid, seq: sentSeq })
  const c = state.containers.get(cid)
  if (c) {
    c.unread = Math.max(0, (c.last_seq || 0) - sentSeq)
    if (c.unread === 0) c.mentions = 0
    notify('containers')
  }
}

function scheduleRead() {
  clearTimeout(readTimer)
  readTimer = setTimeout(flushRead, 600)
}

export function mount(root) {
  scrollEl = root
  headerEl = document.getElementById('timeline-header')
  scrollEl.classList.add('relative', 'flex-1', 'overflow-y-auto', 'bg-crust')

  listEl = el('div', 'flex flex-col pb-4')
  emptyEl = el('p', 'hidden px-4 py-8 text-center text-sm text-overlay1', 'No messages yet.')
  scrollEl.replaceChildren(emptyEl, listEl)

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

  scrollEl.addEventListener('scroll', () => {
    atBottom = scrollEl.scrollHeight - scrollEl.scrollTop - scrollEl.clientHeight < NEAR_BOTTOM
    if (scrollEl.scrollTop < NEAR_TOP) loadOlder()
    if (atBottom) loadNewer()
  })

  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') scheduleRead()
  })

  window.addEventListener('popstate', () => {
    const seq = Number(new URLSearchParams(location.search).get('m')) || 0
    if (seq) jumpTo(seq)
  })

  socket.on('message', (m) => {
    if (m.container_id === state.current.containerId && m.author_id === (state.me && state.me.id)) atBottom = true
  })

  subscribe(scheduleRender)
  render()
}

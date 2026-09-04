import * as socket from '../socket.js'
import { state, subscribe, notify, user, findMessage, loadThread } from '../store.js'
import { containerLabel, containerPath, drawIcons, el, icon, iconButton, navigate } from './dom.js'
import { createComposer } from './composer.js'
import { GROUP_WINDOW, messageNode, messageSignature } from './message.js'

const PAGE = 100

let rootEl = null
let headerEl = null
let bodyEl = null
let rootBlockEl = null
let dividerEl = null
let listEl = null
let composerEl = null
let noticeEl = null
let composer = null

const nodes = new Map()
const signatures = new WeakMap()
const loaded = new Set()
const loading = new Set()
const editing = new Set()

let mountedRoot = null
let headerSignature = ''
let frame = 0

const ctx = {
  showThread: false,
  onReply: null,
  onThread: null,
  onJump: null,
  isEditing: (id) => editing.has(id),
  startEdit(id) {
    editing.add(id)
    nodes.delete(id)
    render()
  },
  endEdit(id) {
    editing.delete(id)
    nodes.delete(id)
    render()
  },
}

function scheduleRender() {
  if (frame) return
  frame = requestAnimationFrame(() => {
    frame = 0
    render()
  })
}

function rootContainer(rootId) {
  const root = findMessage(rootId)
  const cid = (root && root.container_id) || state.current.containerId
  return cid ? state.containers.get(cid) : null
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

  const views = []
  let previous = null
  for (const m of out) {
    const head = Boolean(m.is_system)
      || !previous
      || previous.author_id !== m.author_id
      || new Date(m.created_at) - new Date(previous.created_at) > GROUP_WINDOW
    views.push({ kind: 'message', key: m.id || 'pending:' + m.client_id, m, head })
    previous = m
  }
  return views
}

function nodeFor(view, previous) {
  const signature = messageSignature(view, ctx)
  if (previous && signatures.get(previous) === signature) return previous
  const node = messageNode(view, ctx)
  node.dataset.key = view.key
  signatures.set(node, signature)
  return node
}

function renderRootBlock(rootId) {
  const m = findMessage(rootId)
  if (!m) {
    nodes.delete('root')
    rootBlockEl.replaceChildren(el('p', 'px-3 py-3 text-sm text-overlay1', 'Loading the message this thread hangs from.'))
    return
  }
  const view = { kind: 'message', key: 'root', m, head: true }
  const previous = nodes.get('root')
  const node = nodeFor(view, previous && previous.isConnected ? previous : null)
  if (node === previous) return
  nodes.set('root', node)
  rootBlockEl.replaceChildren(node)
}

function reconcile(list) {
  let cursor = listEl.firstChild
  for (const view of list) {
    const previous = nodes.get(view.key)
    const node = nodeFor(view, previous && previous.isConnected ? previous : null)
    if (node !== previous) {
      if (previous && previous.isConnected) {
        if (cursor === previous) cursor = cursor.nextSibling
        previous.remove()
      }
      nodes.set(view.key, node)
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

function close() {
  const rootId = state.current.threadRootId
  const opener = document.querySelector('#timeline [data-key="' + rootId + '"]')
  if (opener) opener.focus({ preventScroll: true })
  state.current.threadRootId = null
  const cid = state.current.containerId
  const c = cid ? state.containers.get(cid) : null
  if (c) navigate(containerPath(c))
  notify('current')
}

function toggleMute(rootId, button) {
  const next = state.threadSubs.get(rootId) === 'muted' ? 'subscribed' : 'muted'
  state.threadSubs.set(rootId, next)
  socket.send('thread_sub', { thread_root_id: rootId, state: next })
  paintMute(rootId, button)
}

function paintMute(rootId, button) {
  const muted = state.threadSubs.get(rootId) === 'muted'
  button.title = muted ? 'Unmute this thread' : 'Mute this thread'
  button.setAttribute('aria-label', button.title)
  button.replaceChildren(icon(muted ? 'bell' : 'bell-off', 'h-4 w-4'))
  drawIcons(button)
}

function headerSignatureOf(rootId) {
  const c = rootContainer(rootId)
  const root = findMessage(rootId)
  return [rootId, c ? c.id : '', c ? c.name || c.slug : '', root ? root.thread_reply_count : -1].join('|')
}

function renderHeader(rootId) {
  const signature = headerSignatureOf(rootId)
  if (signature === headerSignature) return
  headerSignature = signature
  headerEl.replaceChildren()
  const bar = el('header', 'flex h-12 shrink-0 items-center gap-2 border-b border-surface0 px-3')
  bar.appendChild(icon('message-square-text', 'h-5 w-5 shrink-0 text-overlay1'))

  const title = el('div', 'min-w-0 flex-1')
  title.appendChild(el('h2', 'truncate text-sm font-semibold leading-tight text-text', 'Thread'))
  const c = rootContainer(rootId)
  if (c) {
    const link = el('a', 'block truncate text-xs leading-tight text-blue hover:underline', containerLabel(c, user))
    link.href = containerPath(c)
    link.addEventListener('click', () => {
      state.current.threadRootId = null
      notify('current')
    })
    title.appendChild(link)
  }
  bar.appendChild(title)

  const root = findMessage(rootId)
  if (root && root.thread_reply_count > 0) {
    bar.appendChild(el('span', 'shrink-0 text-xs tabular-nums text-overlay1',
      root.thread_reply_count === 1 ? '1 reply' : root.thread_reply_count + ' replies'))
  }

  const mute = iconButton('bell-off', 'Mute this thread', null)
  mute.addEventListener('click', () => toggleMute(rootId, mute))
  paintMute(rootId, mute)
  bar.appendChild(mute)
  bar.appendChild(iconButton('x', 'Close the thread', close))

  headerEl.appendChild(bar)
  drawIcons(headerEl)
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

function setPane(open) {
  const app = document.getElementById('app')
  if (app) app.dataset.thread = open ? 'open' : 'closed'
}

function render() {
  const rootId = state.current.threadRootId
  if (!rootId) {
    mountedRoot = null
    setPane(false)
    return
  }
  setPane(true)

  if (rootId !== mountedRoot) {
    mountedRoot = rootId
    headerSignature = ''
    nodes.clear()
    editing.clear()
    listEl.replaceChildren()
    load(rootId)
    queueMicrotask(() => composer.focus())
  }

  renderHeader(rootId)
  renderRootBlock(rootId)
  const list = replies(rootId)
  dividerEl.textContent = list.length === 1 ? '1 reply' : list.length + ' replies'
  dividerEl.classList.toggle('hidden', list.length === 0)
  reconcile(list)

  const c = rootContainer(rootId)
  const archived = Boolean(c && c.archived_at)
  noticeEl.classList.toggle('hidden', !archived)
  composerEl.classList.toggle('hidden', archived)
  composer.render()
}

export function mount(root) {
  rootEl = root
  setPane(false)

  const panel = el('div', 'flex h-full flex-col')

  headerEl = el('div', 'shrink-0')
  panel.appendChild(headerEl)

  bodyEl = el('div', 'min-h-0 flex-1 overflow-y-auto')
  rootBlockEl = el('div', 'mx-3 mt-3 rounded-xl bg-surface0/40 py-1')
  bodyEl.appendChild(rootBlockEl)
  dividerEl = el('p', 'px-4 py-3 text-micro font-bold uppercase tracking-widest text-overlay1')
  bodyEl.appendChild(dividerEl)
  listEl = el('div', 'flex flex-col pb-4')
  bodyEl.appendChild(listEl)
  panel.appendChild(bodyEl)

  noticeEl = el('p', 'hidden px-4 py-3 text-sm text-overlay1',
    'This channel is archived. History stays readable and new replies are blocked.')
  panel.appendChild(noticeEl)

  composerEl = el('div', 'shrink-0')
  panel.appendChild(composerEl)

  rootEl.replaceChildren(panel)

  composer = createComposer({
    draftPrefix: 'isane:draft:thread:',
    railClass: 'shrink-0 px-3 pb-3',
    maxHeight: 192,
    fieldLabel: 'Reply in thread',
    target: () => {
      const rootId = state.current.threadRootId
      const c = rootId ? rootContainer(rootId) : null
      return { containerId: c ? c.id : null, threadRootId: rootId }
    },
    placeholder: () => 'Reply in thread',
    archivedNotice: 'This channel is archived. New replies are blocked.',
    emptyNotice: 'Open a thread to reply.',
    onEscape: close,
  })
  composer.mount(composerEl)

  rootEl.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return
    e.preventDefault()
    close()
  })

  subscribe(scheduleRender)
  render()
}

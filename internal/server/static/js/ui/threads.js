import { state, subscribe, user, loadFollowed } from '../store.js'
import { plainText } from '../render.js'
import { containerLabel, drawIcons, el, emptyState, icon, iconButton, navigate, relativeLabel, textButton } from './dom.js'
import { toast } from './toast.js'

const RENDER_KEYS = new Set(['followed', 'containers', 'users', 'current'])

let listEl = null
let loadingOlder = false
let frame = 0

function scheduleRender() {
  if (frame) return
  frame = requestAnimationFrame(() => {
    frame = 0
    render()
  })
}

function placeOf(t, c) {
  if (c) return containerLabel(c, user)
  return t.container_name || t.container_slug || 'Conversation'
}

function rowNode(t) {
  const c = state.containers.get(t.container_id)
  const unread = t.unread > 0
  const row = el('button', 'block w-full rounded-lg px-3 py-2.5 text-left transition-colors hover:bg-surface0 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve')
  row.type = 'button'

  const head = el('div', 'flex items-center gap-1.5')
  head.appendChild(icon(t.container_kind === 'conversation' ? 'users' : 'hash', 'h-3.5 w-3.5 shrink-0 text-overlay1'))
  head.appendChild(el('span', 'min-w-0 truncate text-xs font-medium text-overlay1', placeOf(t, c)))
  row.appendChild(head)

  const body = el('p', 'mt-0.5 line-clamp-2 text-sm ' + (unread ? 'font-semibold text-text' : 'text-subtext0'))
  body.appendChild(el('span', unread ? '' : 'font-medium text-subtext1', user(t.author_id).display_name + ': '))
  if (t.deleted_at) body.appendChild(el('span', 'italic', 'Message deleted'))
  else body.append(plainText(t.body || ''))
  row.appendChild(body)

  const count = t.thread_reply_count || 0
  const meta = el('div', 'mt-1 flex items-center gap-2 text-xs')
  meta.appendChild(el('span', 'font-semibold text-blue', count === 1 ? '1 reply' : count + ' replies'))
  if (t.thread_last_reply_at) meta.appendChild(el('span', 'text-overlay1', 'last reply ' + relativeLabel(t.thread_last_reply_at)))
  if (unread) meta.appendChild(el('span', 'ml-auto font-semibold tabular-nums text-text', t.unread + ' new'))
  row.appendChild(meta)

  row.addEventListener('click', () => navigate('/t/' + t.id))
  return row
}

async function loadOlder() {
  const list = state.followed || []
  const last = list[list.length - 1]
  if (!last || loadingOlder) return
  loadingOlder = true
  render()
  try {
    await loadFollowed(last.thread_last_reply_at)
  } catch (err) {
    console.error('older threads failed', err)
    toast('Could not load older threads', { severity: 'error' })
  }
  loadingOlder = false
  render()
}

function render() {
  const open = state.current.view === 'threads'
  const app = document.getElementById('app')
  if (app) app.dataset.view = open ? 'threads' : 'chat'
  if (!open) return

  const list = state.followed
  listEl.replaceChildren()
  if (!list) {
    listEl.appendChild(emptyState('Loading threads.'))
    return
  }
  if (!list.length) {
    listEl.appendChild(emptyState('Threads you start, reply in, or are mentioned in show up here.'))
    return
  }
  for (const t of list) listEl.appendChild(rowNode(t))
  if (state.followedOlder) {
    const more = textButton(loadingOlder ? 'Loading' : 'Show older threads', loadOlder, 'secondary')
    more.disabled = loadingOlder
    more.classList.add('mx-auto', 'mt-2')
    listEl.appendChild(more)
  }
  drawIcons(listEl)
}

export function mount(root) {
  const header = el('header', 'flex h-12 w-full min-w-0 shrink-0 items-center gap-2 border-b border-surface0 px-4')
  header.appendChild(iconButton('menu', 'Channels', () => window.dispatchEvent(new CustomEvent('isane:sidebar-toggle')), 'md:hidden'))
  header.appendChild(icon('message-square-text', 'h-5 w-5 shrink-0 text-overlay1'))
  header.appendChild(el('h1', 'min-w-0 truncate text-message font-semibold text-text', 'Threads'))

  const scroll = el('div', 'min-h-0 flex-1 overflow-y-auto overscroll-contain')
  listEl = el('div', 'flex flex-col gap-0.5 p-2')
  scroll.appendChild(listEl)

  root.replaceChildren(header, scroll)
  drawIcons(header)

  subscribe((keys) => {
    if (keys.some((key) => RENDER_KEYS.has(key))) scheduleRender()
  })
  render()
}

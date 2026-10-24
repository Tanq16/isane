import { get } from './api.js'

export const state = {
  me: null,
  users: new Map(),
  containers: new Map(),
  messages: new Map(),
  threads: new Map(),
  threadSubs: new Map(),
  followed: null,
  followedOlder: false,
  pending: new Map(),
  typing: new Map(),
  presence: new Set(),
  calls: new Map(),
  jobs: new Map(),
  settings: { allow_member_channels: true },
  current: { containerId: null, threadRootId: null, replyToId: null, view: null },
  quality: null,
  connection: 'live',
}

const FOLLOWED_PAGE = 50
const FOLLOWED_REFRESH = 500

const subscribers = new Set()
const placeholders = new Map()
let queuedKeys = null
let followedTimer = 0

export function subscribe(fn) {
  subscribers.add(fn)
  return () => subscribers.delete(fn)
}

export function notify(...keys) {
  if (!queuedKeys) {
    queuedKeys = new Set()
    queueMicrotask(flush)
  }
  for (const key of keys) queuedKeys.add(key)
}

function flush() {
  const keys = Array.from(queuedKeys)
  queuedKeys = null
  for (const fn of Array.from(subscribers)) {
    try {
      fn(keys)
    } catch (err) {
      console.error('store subscriber failed', keys, err)
    }
  }
}

export function unreadTotal() {
  let total = 0
  for (const c of state.containers.values()) {
    const mentions = c.mentions || 0
    if (mentions > 0) total += mentions
    else if (c.kind === 'conversation') total += c.unread || 0
  }
  return total
}

export function containerUnread(c, seq) {
  const last = c.last_seq ?? 0
  if (seq >= last) return 0
  let unread = 0
  for (const m of state.messages.get(c.id) ?? []) {
    if ((m.seq ?? 0) > seq && !m.thread_root_id && !m.deleted_at) unread++
  }
  return unread
}

export function watchSends(match) {
  let known = new Set()
  return () => {
    const seen = new Set()
    let fresh = false
    for (const p of state.pending.values()) {
      if (!p.pending || !match(p)) continue
      seen.add(p.client_id)
      if (!known.has(p.client_id)) fresh = true
    }
    known = seen
    return fresh
  }
}

export function user(id) {
  const found = id ? state.users.get(id) : null
  if (found) return found
  let placeholder = placeholders.get(id ?? null)
  if (!placeholder) {
    placeholder = { id: id ?? null, kind: 'human', handle: 'unknown', display_name: 'Unknown', is_admin: false }
    placeholders.set(id ?? null, placeholder)
  }
  return placeholder
}

export function upsertContainer(c) {
  if (!c || !c.id) return null
  const prev = state.containers.get(c.id)
  const next = prev ? Object.assign({}, prev, c) : c
  state.containers.set(c.id, next)
  return next
}

function place(list, m) {
  const at = list.findIndex((x) => x.id === m.id)
  if (at >= 0) {
    list[at] = Object.assign({}, list[at], m)
    return false
  }
  let i = list.length
  while (i > 0 && (list[i - 1].seq ?? 0) > (m.seq ?? 0)) i--
  list.splice(i, 0, m)
  return true
}

export function upsertMessage(m) {
  if (!m || !m.id) return false
  if (m.client_id) state.pending.delete(m.client_id)
  const list = m.thread_root_id ? state.threads.get(m.thread_root_id) : state.messages.get(m.container_id)
  if (!list) return true
  return place(list, m)
}

export function mergeMessage(m) {
  if (!m || !m.id) return
  if (m.client_id) state.pending.delete(m.client_id)
  if (m.thread_root_id) merge(state.threads, m.thread_root_id, [m], false)
  else merge(state.messages, m.container_id, [m], false)
}

export function findMessage(id) {
  if (!id) return null
  for (const list of state.messages.values()) {
    const found = list.find((m) => m.id === id)
    if (found) return found
  }
  for (const list of state.threads.values()) {
    const found = list.find((m) => m.id === id)
    if (found) return found
  }
  return null
}

function listOf(res) {
  if (Array.isArray(res)) return res
  if (res && Array.isArray(res.messages)) return res.messages
  return []
}

function merge(map, key, msgs, replace) {
  const byId = new Map()
  if (!replace) {
    for (const m of map.get(key) ?? []) byId.set(m.id, m)
  }
  for (const m of msgs) byId.set(m.id, Object.assign({}, byId.get(m.id) ?? {}, m))
  const out = Array.from(byId.values()).sort((a, b) => (a.seq ?? 0) - (b.seq ?? 0))
  map.set(key, out)
  return out
}

export async function loadMessages(containerId, opts = {}) {
  const params = { limit: opts.limit ?? 50 }
  if (opts.before != null) params.before = opts.before
  if (opts.after != null) params.after = opts.after
  if (opts.around != null) params.around = opts.around
  const msgs = listOf(await get(`/containers/${containerId}/messages`, params))
  merge(state.messages, containerId, msgs, opts.around != null)
  notify('messages')
  return msgs
}

export async function loadThread(rootId, opts = {}) {
  const params = { limit: opts.limit ?? 50 }
  if (opts.after != null) params.after = opts.after
  const page = await get(`/threads/${rootId}/messages`, params)
  const msgs = listOf(page && page.messages)
  if (page && page.root) merge(state.messages, page.root.container_id, [page.root], false)
  state.threadSubs.set(rootId, (page && page.subscription) || '')
  merge(state.threads, rootId, msgs, false)
  notify('messages', 'threads')
  return { root: (page && page.root) || null, messages: msgs }
}

export function followedThread(rootId) {
  return (state.followed || []).find((t) => t.id === rootId) || null
}

export async function loadFollowed(before) {
  const page = await get('/threads', before ? { before } : undefined)
  const list = Array.isArray(page) ? page : []
  state.followed = before ? (state.followed || []).concat(list.filter((t) => !followedThread(t.id))) : list
  state.followedOlder = list.length >= FOLLOWED_PAGE
  notify('followed')
  return list
}

export function refreshFollowed() {
  clearTimeout(followedTimer)
  followedTimer = setTimeout(() => {
    loadFollowed().catch((err) => console.error('followed threads failed', err))
  }, FOLLOWED_REFRESH)
}

export function markThreadRead(rootId, seq) {
  const entry = followedThread(rootId)
  if (!entry) return
  entry.last_read_seq = Math.max(entry.last_read_seq || 0, seq)
  const replies = state.threads.get(rootId)
  if (!replies) {
    refreshFollowed()
    return
  }
  entry.unread = replies.filter((m) =>
    (m.seq ?? 0) > entry.last_read_seq && !m.deleted_at && (!state.me || m.author_id !== state.me.id)).length
  notify('followed')
}

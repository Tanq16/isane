import { get } from './api.js'

export const state = {
  me: null,
  users: new Map(),
  containers: new Map(),
  messages: new Map(),
  threads: new Map(),
  threadSubs: new Map(),
  pending: new Map(),
  typing: new Map(),
  presence: new Set(),
  calls: new Map(),
  jobs: new Map(),
  settings: { allow_member_channels: true },
  current: { containerId: null, threadRootId: null, replyToId: null },
  quality: null,
  connection: 'live',
}

const subscribers = new Set()
const placeholders = new Map()
let queuedKeys = null

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

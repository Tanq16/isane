import { get } from './api.js'
import { state, notify, upsertContainer, upsertMessage, findMessage, loadMessages } from './store.js'
import { currentEndpoint, onEndpoint } from './push.js'
import { play } from './sound.js'

const RECONNECT_MIN = 1000
const RECONNECT_MAX = 30000
const OFFLINE_AFTER = 15000
const PING_INTERVAL = 30000
const TYPING_TTL = 5000
const OUTBOX_LIMIT = 200
const BACKFILL_PAGES = 40

const listeners = new Map()

let ws = null
let started = false
let attempt = 0
let reconnectTimer = null
let pingTimer = null
let offlineTimer = null
let awaitingPong = false
let outbox = []
let helloEndpoint = null
let hiddenAt = 0

export function isLive() {
  return Boolean(ws) && ws.readyState === WebSocket.OPEN
}

export function on(type, fn) {
  let set = listeners.get(type)
  if (!set) {
    set = new Set()
    listeners.set(type, set)
  }
  set.add(fn)
  return () => set.delete(fn)
}

function emit(type, d) {
  const set = listeners.get(type)
  if (!set) return
  for (const fn of Array.from(set)) {
    try {
      fn(d)
    } catch (err) {
      console.error('socket listener failed', type, err)
    }
  }
}

export function connect() {
  started = true
  if (ws && (ws.readyState === WebSocket.CONNECTING || ws.readyState === WebSocket.OPEN)) return
  clearTimeout(reconnectTimer)
  reconnectTimer = null
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const socket = new WebSocket(`${scheme}//${location.host}/ws`)
  ws = socket
  socket.onopen = onOpen
  socket.onmessage = onFrame
  socket.onerror = () => {
    if (ws === socket && socket.readyState !== WebSocket.OPEN) get('/auth/me').catch(() => {})
  }
  socket.onclose = () => onClose(socket)
}

export function stop() {
  started = false
  clearTimeout(reconnectTimer)
  reconnectTimer = null
  stopPing()
  if (ws) ws.close()
  ws = null
}

export function send(type, data) {
  if (isLive()) {
    write(type, data)
    return true
  }
  if (type === 'ping' || type === 'typing') return false
  outbox.push({ t: type, d: data ?? {} })
  if (outbox.length > OUTBOX_LIMIT) outbox.shift()
  return false
}

function write(type, data) {
  ws.send(JSON.stringify({ t: type, d: data ?? {} }))
}

function onOpen() {
  attempt = 0
  clearTimeout(offlineTimer)
  offlineTimer = null
  if (state.connection !== 'live') {
    state.connection = 'live'
    notify('connection')
  }
  hello()
  drain()
  startPing()
}

function pageVisible() {
  return document.visibilityState === 'visible'
}

function hello() {
  const cursors = {}
  for (const [id, container] of state.containers) {
    if (typeof container.last_seq === 'number') cursors[id] = container.last_seq
  }
  helloEndpoint = currentEndpoint()
  write('hello', { cursors, push_endpoint: helloEndpoint, visible: pageVisible() })
}

onEndpoint((endpoint) => {
  if (!isLive() || endpoint === helloEndpoint) return
  hello()
})

function drain() {
  const queue = outbox
  outbox = []
  const replayed = new Set()
  for (const frame of queue) {
    if (frame.t === 'send' && frame.d && frame.d.client_id) replayed.add(frame.d.client_id)
    ws.send(JSON.stringify(frame))
  }
  for (const [clientId, m] of state.pending) {
    if (replayed.has(clientId)) continue
    write('send', {
      container_id: m.container_id,
      client_id: clientId,
      body: m.body,
      reply_to_id: m.reply_to_id ?? null,
      thread_root_id: m.thread_root_id ?? null,
      attachment_ids: m.attachment_ids ?? (m.attachments ?? []).map((a) => a.id),
    })
  }
}

function onClose(socket) {
  if (ws !== socket) return
  ws = null
  stopPing()
  if (!started) return
  if (!offlineTimer && state.connection === 'live') {
    offlineTimer = setTimeout(() => {
      offlineTimer = null
      state.connection = 'down'
      notify('connection')
    }, OFFLINE_AFTER)
  }
  const base = Math.min(RECONNECT_MAX, RECONNECT_MIN * 2 ** attempt)
  attempt = Math.min(attempt + 1, 10)
  const delay = Math.min(RECONNECT_MAX, Math.round(base * (0.75 + Math.random() * 0.5)))
  reconnectTimer = setTimeout(connect, delay)
}

function startPing() {
  stopPing()
  awaitingPong = false
  pingTimer = setInterval(() => {
    if (!isLive()) return
    if (awaitingPong) {
      ws.close()
      return
    }
    awaitingPong = true
    write('ping', {})
  }, PING_INTERVAL)
}

function stopPing() {
  if (!pingTimer) return
  clearInterval(pingTimer)
  pingTimer = null
}

function revive() {
  if (!started || isLive()) return
  attempt = 0
  clearTimeout(reconnectTimer)
  reconnectTimer = null
  connect()
}

document.addEventListener('visibilitychange', () => {
  if (isLive()) write('visibility', { visible: pageVisible() })
  if (!pageVisible()) {
    hiddenAt = Date.now()
    return
  }
  if (isLive() && hiddenAt && Date.now() - hiddenAt > PING_INTERVAL) {
    attempt = 0
    ws.close()
    return
  }
  revive()
})
window.addEventListener('online', revive)

function onFrame(event) {
  let frame
  try {
    frame = JSON.parse(event.data)
  } catch {
    return
  }
  if (!frame || !frame.t) return
  const d = frame.d ?? {}
  apply(frame.t, d)
  emit(frame.t, d)
}

function apply(type, d) {
  switch (type) {
    case 'ready':
      onReady(d)
      return
    case 'message':
      onMessage(d)
      return
    case 'message_edited':
      onEdited(d)
      return
    case 'message_deleted':
      onDeleted(d)
      return
    case 'read':
      onRead(d)
      return
    case 'typing':
      onTyping(d)
      return
    case 'presence':
      onPresence(d)
      return
    case 'call_started':
      onCallStarted(d)
      return
    case 'call_participant':
      onCallParticipant(d)
      return
    case 'call_recording':
      onCallRecording(d)
      return
    case 'call_ended':
      onCallEnded(d)
      return
    case 'agent_working':
      state.jobs.set(d.job_id, d)
      notify('jobs')
      return
    case 'agent_done':
      state.jobs.delete(d.job_id)
      notify('jobs')
      return
    case 'settings':
      state.settings = Object.assign({}, state.settings, d)
      notify('settings')
      return
    case 'container':
      upsertContainer(d)
      notify('containers')
      return
    case 'user':
      if (d && d.id) state.users.set(d.id, Object.assign({}, state.users.get(d.id) || {}, d))
      notify('users')
      return
    case 'attachment':
      onAttachment(d)
      return
    case 'error':
      onError(d)
      return
    case 'pong':
      awaitingPong = false
      return
    default:
      return
  }
}

function onReady(d) {
  if (d.user) {
    state.me = d.user
    state.users.set(d.user.id, d.user)
  }
  if (Array.isArray(d.users)) {
    for (const u of d.users) state.users.set(u.id, Object.assign({}, state.users.get(u.id) || {}, u))
  }
  if (Array.isArray(d.containers)) {
    for (const c of d.containers) upsertContainer(c)
  }
  if (Array.isArray(d.presence)) {
    state.presence = new Set(d.presence)
  }
  if (Array.isArray(d.calls)) {
    const live = new Map()
    for (const call of d.calls) {
      if (call && call.container_id && !call.ended_at) live.set(call.container_id, call)
    }
    state.calls = live
  }
  if (d.settings) state.settings = Object.assign({}, state.settings, d.settings)
  state.quality = d.media_quality ?? d.quality ?? state.quality
  notify('me', 'users', 'containers', 'presence', 'calls', 'settings', 'quality')
  if (Array.isArray(d.gaps) && d.gaps.length) backfill(d.gaps)
}

function onAttachment(a) {
  if (!a || !a.id || !a.message_id) return
  const m = findMessage(a.message_id)
  if (!m) return
  const list = Array.isArray(m.attachments) ? m.attachments.slice() : []
  const at = list.findIndex((x) => x.id === a.id)
  if (at >= 0) list[at] = Object.assign({}, list[at], a)
  else list.push(a)
  m.attachments = list
  notify('messages', 'threads')
}

function onError(d) {
  const pending = d.client_id ? state.pending.get(d.client_id) : null
  if (pending) {
    pending.pending = false
    pending.failed = true
    notify('pending')
  }
}

async function backfill(gaps) {
  for (const gap of gaps) {
    const id = gap.container_id
    if (!id || !state.messages.has(id)) continue
    let cursor = Math.max(0, (gap.from ?? 1) - 1)
    for (let page = 0; page < BACKFILL_PAGES; page++) {
      let msgs
      try {
        msgs = await loadMessages(id, { after: cursor, limit: 50 })
      } catch (err) {
        console.error('gap backfill failed', id, err)
        break
      }
      if (!msgs.length) break
      cursor = msgs[msgs.length - 1].seq ?? cursor
      if (cursor >= (gap.to ?? cursor)) break
    }
  }
}

function onMessage(m) {
  const fresh = upsertMessage(m)
  const container = state.containers.get(m.container_id)
  if (container) {
    if ((m.seq ?? 0) > (container.last_seq ?? 0)) container.last_seq = m.seq
    const mine = state.me && m.author_id === state.me.id
    if (fresh && !mine && !m.thread_root_id && state.current.containerId !== m.container_id) {
      container.unread = (container.unread ?? 0) + 1
      if (state.me && Array.isArray(m.mentions) && m.mentions.includes(state.me.id)) {
        container.mentions = (container.mentions ?? 0) + 1
      }
    }
  }
  if (fresh && m.thread_root_id) {
    const root = findMessage(m.thread_root_id)
    if (root) {
      root.thread_reply_count = (root.thread_reply_count ?? 0) + 1
      root.thread_last_reply_at = m.created_at
    }
  }
  const typists = state.typing.get(m.container_id)
  if (typists && typists.delete(m.author_id)) {
    if (!typists.size) state.typing.delete(m.container_id)
    notify('typing')
  }
  if (fresh) announce(m, container)
  notify('messages', 'containers')
}

function shouldAnnounce(container, m) {
  const level = container.level || (container.kind === 'conversation' ? 'all' : 'mentions')
  if (level === 'none') return false
  const mentioned = Boolean(state.me && Array.isArray(m.mentions) && m.mentions.includes(state.me.id))
  if (m.thread_root_id) {
    const sub = state.threadSubs.get(m.thread_root_id)
    if (sub === 'muted') return false
    return mentioned || sub === 'subscribed' || level === 'all'
  }
  return level === 'all' || mentioned
}

function announce(m, container) {
  if (!container) return
  if (state.me && m.author_id === state.me.id) return
  if (pageVisible() && state.current.containerId === m.container_id) return
  if (!shouldAnnounce(container, m)) return
  play()
}

function onEdited(d) {
  const m = findMessage(d.id)
  if (!m) return
  m.body = d.body
  m.edited_at = d.edited_at
  notify('messages')
}

function onDeleted(d) {
  const m = findMessage(d.id)
  if (!m) return
  m.body = ''
  m.deleted_at = d.deleted_at ?? new Date().toISOString()
  notify('messages')
}

function onRead(d) {
  const container = state.containers.get(d.container_id)
  if (!container) return
  container.last_read_seq = d.seq ?? 0
  container.unread = Math.max(0, (container.last_seq ?? 0) - (d.seq ?? 0))
  if ((d.seq ?? 0) >= (container.last_seq ?? 0)) container.mentions = 0
  notify('containers')
}

function onTyping(d) {
  if (state.me && d.user_id === state.me.id) return
  let byUser = state.typing.get(d.container_id)
  if (!byUser) {
    byUser = new Map()
    state.typing.set(d.container_id, byUser)
  }
  byUser.set(d.user_id, { expiry: Date.now() + TYPING_TTL, threadRootId: d.thread_root_id || null })
  notify('typing')
  setTimeout(pruneTyping, TYPING_TTL + 100)
}

function pruneTyping() {
  const now = Date.now()
  let changed = false
  for (const [containerId, byUser] of state.typing) {
    for (const [userId, entry] of byUser) {
      if (entry.expiry > now) continue
      byUser.delete(userId)
      changed = true
    }
    if (!byUser.size) state.typing.delete(containerId)
  }
  if (changed) notify('typing')
}

function onPresence(d) {
  if (d.online) state.presence.add(d.user_id)
  else state.presence.delete(d.user_id)
  notify('presence')
}

function onCallStarted(call) {
  if (!call || !call.container_id) return
  state.calls.set(call.container_id, call)
  notify('calls')
}

function onCallParticipant(d) {
  for (const call of state.calls.values()) {
    if (call.id !== d.call_id) continue
    const participants = Array.isArray(call.participants) ? call.participants : []
    const at = participants.indexOf(d.user_id)
    if (d.joined && at < 0) participants.push(d.user_id)
    if (!d.joined && at >= 0) participants.splice(at, 1)
    call.participants = participants
  }
  notify('calls')
}

function onCallRecording(d) {
  for (const call of state.calls.values()) {
    if (call.id !== d.call_id) continue
    call.recording_state = d.recording_state
  }
  notify('calls')
}

function onCallEnded(d) {
  for (const [containerId, call] of state.calls) {
    if (call.id === d.call_id) state.calls.delete(containerId)
  }
  notify('calls')
}

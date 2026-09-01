import { state, subscribe, notify, user, upsertMessage } from '../store.js'
import * as api from '../api.js'
import * as socket from '../socket.js'
import { renderMarkdown } from '../render.js'

const DRAFT_PREFIX = 'isane:draft:'
const TYPING_INTERVAL = 3000
const MENTION_PATTERN = /(^|\s)@([a-z0-9_-]*)$/i

let rootEl = null
let typingEl = null
let replyEl = null
let chipsEl = null
let boxEl = null
let textarea = null
let previewEl = null
let previewButton = null
let sendButton = null
let noticeEl = null
let mentionEl = null
let fileInput = null

let currentContainer = null
let attachments = []
let previewOpen = false
let mentionOpen = false
let mentionIndex = 0
let mentionMatches = []
let mentionStart = 0
let lastTyping = 0
let draftTimer = 0
let previewTimer = 0
let frame = 0

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

function container() {
  const id = state.current.containerId
  return id ? state.containers.get(id) : null
}

function draftKey(id) {
  return DRAFT_PREFIX + id
}

function loadDraft(id) {
  try {
    return localStorage.getItem(draftKey(id)) || ''
  } catch {
    return ''
  }
}

function saveDraft(id, value) {
  try {
    if (value) localStorage.setItem(draftKey(id), value)
    else localStorage.removeItem(draftKey(id))
  } catch {}
}

function autosize() {
  textarea.style.height = 'auto'
  textarea.style.height = Math.min(textarea.scrollHeight, 320) + 'px'
}

function renderChips() {
  chipsEl.replaceChildren()
  chipsEl.classList.toggle('hidden', attachments.length === 0)
  attachments.forEach((entry, index) => {
    const chip = el('div', 'flex items-center gap-2 rounded-lg border border-surface1 bg-surface0 px-2 py-1 text-xs')
    chip.appendChild(el('span', 'max-w-40 truncate text-subtext0', entry.name))
    if (entry.error) chip.appendChild(el('span', 'text-red', 'failed'))
    else if (!entry.attachment) chip.appendChild(el('span', 'text-yellow', Math.round(entry.progress * 100) + '%'))
    else chip.appendChild(el('span', 'text-green', 'ready'))
    const remove = el('button', 'text-overlay1 hover:text-red', 'x')
    remove.type = 'button'
    remove.addEventListener('click', () => {
      attachments.splice(index, 1)
      renderChips()
      updateSendState()
    })
    chip.appendChild(remove)
    chipsEl.appendChild(chip)
  })
}

function renderReply() {
  replyEl.replaceChildren()
  const id = state.current.replyToId
  if (!id) {
    replyEl.classList.add('hidden')
    return
  }
  replyEl.classList.remove('hidden')
  const list = state.messages.get(state.current.containerId) || []
  const parent = list.find((m) => m.id === id)
  replyEl.appendChild(el('span', 'shrink-0 text-overlay1', 'Replying to'))
  replyEl.appendChild(el('span', 'truncate font-semibold text-subtext1', parent ? user(parent.author_id).display_name : 'a message'))
  const cancel = el('button', 'ml-auto shrink-0 text-overlay1 hover:text-red', 'Cancel')
  cancel.type = 'button'
  cancel.addEventListener('click', () => {
    state.current.replyToId = null
    notify('current')
  })
  replyEl.appendChild(cancel)
}

function renderTyping() {
  const id = state.current.containerId
  const map = id ? state.typing.get(id) : null
  const now = Date.now()
  const names = []
  if (map) {
    for (const [userId, expiry] of map) {
      if (expiry < now) continue
      if (state.me && userId === state.me.id) continue
      names.push(user(userId).display_name)
    }
  }
  typingEl.classList.toggle('hidden', names.length === 0)
  if (!names.length) return
  const label = names.length === 1 ? names[0] + ' is typing' : names.slice(0, 3).join(', ') + ' are typing'
  typingEl.textContent = label
}

function updateSendState() {
  const ready = attachments.some((a) => a.attachment)
  sendButton.disabled = !textarea.value.trim() && !ready
}

function candidates(query) {
  const c = container()
  const q = query.toLowerCase()
  const out = []
  if (c && c.kind === 'channel' && 'channel'.startsWith(q)) {
    out.push({ handle: 'channel', display_name: 'Notify everyone in this channel', kind: 'channel' })
  }
  const participants = c && c.kind === 'conversation' ? new Set(c.participants || []) : null
  for (const u of state.users.values()) {
    if (u.deactivated_at) continue
    if (participants && u.kind !== 'agent' && !participants.has(u.id)) continue
    if (!u.handle.toLowerCase().startsWith(q) && !u.display_name.toLowerCase().includes(q)) continue
    out.push(u)
  }
  return out.slice(0, 8)
}

function closeMentions() {
  mentionOpen = false
  mentionMatches = []
  mentionEl.classList.add('hidden')
  mentionEl.replaceChildren()
}

function renderMentions() {
  mentionEl.replaceChildren()
  if (!mentionMatches.length) {
    closeMentions()
    return
  }
  mentionEl.classList.remove('hidden')
  mentionMatches.forEach((entry, index) => {
    const row = el(
      'button',
      index === mentionIndex
        ? 'flex w-full items-baseline gap-2 rounded-lg bg-surface0 px-2 py-1.5 text-left text-sm'
        : 'flex w-full items-baseline gap-2 rounded-lg px-2 py-1.5 text-left text-sm hover:bg-surface0'
    )
    row.type = 'button'
    row.appendChild(el('span', entry.kind === 'agent' ? 'font-mono text-lavender' : 'font-mono text-blue', '@' + entry.handle))
    row.appendChild(el('span', 'truncate text-xs text-overlay1', entry.display_name))
    row.addEventListener('mousedown', (e) => {
      e.preventDefault()
      acceptMention(entry)
    })
    mentionEl.appendChild(row)
  })
}

function acceptMention(entry) {
  const before = textarea.value.slice(0, mentionStart)
  const after = textarea.value.slice(textarea.selectionStart)
  const insert = '@' + entry.handle + ' '
  textarea.value = before + insert + after
  const caret = before.length + insert.length
  textarea.setSelectionRange(caret, caret)
  closeMentions()
  autosize()
  updateSendState()
  textarea.focus()
}

function updateMentions() {
  const caret = textarea.selectionStart
  const match = MENTION_PATTERN.exec(textarea.value.slice(0, caret))
  if (!match) {
    closeMentions()
    return
  }
  mentionStart = caret - match[2].length - 1
  mentionMatches = candidates(match[2])
  mentionIndex = 0
  mentionOpen = mentionMatches.length > 0
  renderMentions()
}

function schedulePreview() {
  if (!previewOpen) return
  clearTimeout(previewTimer)
  previewTimer = setTimeout(() => {
    previewEl.replaceChildren(renderMarkdown(textarea.value))
  }, 200)
}

function togglePreview() {
  previewOpen = !previewOpen
  previewEl.classList.toggle('hidden', !previewOpen)
  previewButton.textContent = previewOpen ? 'Hide preview' : 'Preview'
  if (previewOpen) previewEl.replaceChildren(renderMarkdown(textarea.value))
}

async function addFiles(files) {
  for (const file of files) {
    const entry = { name: file.name, progress: 0, attachment: null, error: null }
    attachments.push(entry)
    renderChips()
    try {
      entry.attachment = await api.upload(file, (p) => {
        entry.progress = p
        renderChips()
      })
    } catch (err) {
      entry.error = err.message || 'upload failed'
    }
    renderChips()
    updateSendState()
  }
}

function maybeTyping() {
  const id = state.current.containerId
  if (!id) return
  const now = Date.now()
  if (now - lastTyping < TYPING_INTERVAL) return
  lastTyping = now
  socket.send('typing', { container_id: id })
}

async function submit() {
  const c = container()
  if (!c || c.archived_at) return
  const body = textarea.value.trim()
  const ids = attachments.filter((a) => a.attachment).map((a) => a.attachment.id)
  if (!body && !ids.length) return

  const clientId = crypto.randomUUID()
  const payload = {
    container_id: c.id,
    client_id: clientId,
    body,
    reply_to_id: state.current.replyToId || null,
    attachment_ids: ids,
  }
  const optimistic = {
    id: 'pending:' + clientId,
    container_id: c.id,
    client_id: clientId,
    author_id: state.me ? state.me.id : null,
    body,
    reply_to_id: payload.reply_to_id,
    thread_root_id: null,
    thread_reply_count: 0,
    created_at: new Date().toISOString(),
    attachments: attachments.filter((a) => a.attachment).map((a) => a.attachment),
    pending: true,
  }
  state.pending.set(clientId, optimistic)

  textarea.value = ''
  attachments = []
  state.current.replyToId = null
  saveDraft(c.id, '')
  renderChips()
  autosize()
  updateSendState()
  closeMentions()
  if (previewOpen) previewEl.replaceChildren()
  notify('pending', 'current')

  if (socket.isLive()) {
    socket.send('send', payload)
    return
  }
  try {
    const saved = await api.post('/api/containers/' + c.id + '/messages', payload)
    state.pending.delete(clientId)
    upsertMessage(saved)
    notify('pending', 'messages')
  } catch {
    optimistic.pending = false
    optimistic.failed = true
    notify('pending')
  }
}

function renderNotice(c) {
  const archived = Boolean(c && c.archived_at)
  const disconnected = !c
  noticeEl.classList.toggle('hidden', !archived && !disconnected)
  boxEl.classList.toggle('hidden', archived || disconnected)
  previewEl.classList.toggle('hidden', archived || disconnected || !previewOpen)
  chipsEl.classList.toggle('hidden', archived || disconnected || attachments.length === 0)
  if (archived) noticeEl.textContent = 'This channel is archived. History stays readable and new messages are blocked.'
  else if (disconnected) noticeEl.textContent = 'Select a channel or conversation to start writing.'
}

function render() {
  const c = container()
  const id = c ? c.id : null
  if (id !== currentContainer) {
    if (currentContainer) saveDraft(currentContainer, textarea.value)
    currentContainer = id
    textarea.value = id ? loadDraft(id) : ''
    attachments = []
    closeMentions()
    renderChips()
    autosize()
    if (previewOpen) previewEl.replaceChildren(renderMarkdown(textarea.value))
  }
  renderNotice(c)
  renderReply()
  renderTyping()
  updateSendState()
}

export function mount(root) {
  rootEl = root
  rootEl.classList.add('border-t', 'border-surface0', 'bg-crust', 'px-4', 'py-3')

  typingEl = el('p', 'hidden pb-1 text-xs text-overlay1')
  replyEl = el('div', 'mb-2 hidden items-center gap-2 rounded-lg border border-surface0 bg-mantle px-2 py-1 text-xs')
  chipsEl = el('div', 'mb-2 hidden flex-wrap gap-2')
  noticeEl = el('p', 'hidden rounded-lg border border-surface0 bg-mantle px-3 py-2 text-sm text-overlay1')

  boxEl = el('div', 'relative rounded-xl border border-surface1 bg-surface0 focus-within:border-mauve')

  mentionEl = el('div', 'absolute bottom-full left-2 z-20 mb-2 hidden w-72 space-y-0.5 rounded-lg border border-surface0 bg-mantle p-1 shadow-lg')
  boxEl.appendChild(mentionEl)

  textarea = el('textarea', 'block max-h-80 w-full resize-none bg-transparent px-3 pt-3 text-sm text-text placeholder:text-overlay0 focus:outline-none')
  textarea.rows = 1
  textarea.placeholder = 'Write a message. Markdown is supported.'
  boxEl.appendChild(textarea)

  const bar = el('div', 'flex items-center gap-2 px-3 pb-2 pt-1')

  fileInput = el('input', 'hidden')
  fileInput.type = 'file'
  fileInput.multiple = true
  fileInput.addEventListener('change', () => {
    addFiles(Array.from(fileInput.files || []))
    fileInput.value = ''
  })
  bar.appendChild(fileInput)

  const attach = el('button', 'rounded-lg px-2 py-1 text-xs text-subtext0 hover:bg-surface1 hover:text-text', 'Attach')
  attach.type = 'button'
  attach.addEventListener('click', () => fileInput.click())
  bar.appendChild(attach)

  previewButton = el('button', 'rounded-lg px-2 py-1 text-xs text-subtext0 hover:bg-surface1 hover:text-text', 'Preview')
  previewButton.type = 'button'
  previewButton.addEventListener('click', togglePreview)
  bar.appendChild(previewButton)

  bar.appendChild(el('span', 'ml-auto text-xs text-overlay1', 'Enter sends, Shift plus Enter adds a line'))

  sendButton = el('button', 'rounded-lg bg-mauve px-3 py-1.5 text-xs font-semibold text-crust disabled:opacity-40', 'Send')
  sendButton.type = 'button'
  sendButton.disabled = true
  sendButton.addEventListener('click', submit)
  bar.appendChild(sendButton)

  boxEl.appendChild(bar)

  previewEl = el('div', 'markdown-body mt-2 hidden rounded-xl border border-surface0 bg-mantle p-3 text-sm')

  rootEl.replaceChildren(typingEl, replyEl, chipsEl, noticeEl, boxEl, previewEl)

  textarea.addEventListener('input', () => {
    autosize()
    updateSendState()
    updateMentions()
    schedulePreview()
    maybeTyping()
    clearTimeout(draftTimer)
    const id = state.current.containerId
    draftTimer = setTimeout(() => saveDraft(id, textarea.value), 300)
  })

  textarea.addEventListener('keydown', (e) => {
    if (mentionOpen) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        mentionIndex = (mentionIndex + 1) % mentionMatches.length
        renderMentions()
        return
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        mentionIndex = (mentionIndex - 1 + mentionMatches.length) % mentionMatches.length
        renderMentions()
        return
      }
      if (e.key === 'Enter' || e.key === 'Tab') {
        e.preventDefault()
        acceptMention(mentionMatches[mentionIndex])
        return
      }
      if (e.key === 'Escape') {
        e.preventDefault()
        closeMentions()
        return
      }
    }
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      submit()
      return
    }
    if (e.key === 'Escape' && state.current.replyToId) {
      e.preventDefault()
      state.current.replyToId = null
      notify('current')
    }
  })

  textarea.addEventListener('blur', () => closeMentions())

  textarea.addEventListener('paste', (e) => {
    const files = Array.from(e.clipboardData ? e.clipboardData.files : [])
    if (!files.length) return
    e.preventDefault()
    addFiles(files)
  })

  rootEl.addEventListener('dragover', (e) => {
    e.preventDefault()
    boxEl.classList.add('border-mauve')
  })
  rootEl.addEventListener('dragleave', () => boxEl.classList.remove('border-mauve'))
  rootEl.addEventListener('drop', (e) => {
    e.preventDefault()
    boxEl.classList.remove('border-mauve')
    const files = Array.from(e.dataTransfer ? e.dataTransfer.files : [])
    if (files.length) addFiles(files)
  })

  window.addEventListener('beforeunload', () => {
    if (currentContainer) saveDraft(currentContainer, textarea.value)
  })

  socket.on('message', (m) => {
    if (!m.client_id) return
    if (state.pending.delete(m.client_id)) notify('pending')
  })

  socket.on('ready', () => {
    for (const p of state.pending.values()) {
      if (p.failed) continue
      socket.send('send', {
        container_id: p.container_id,
        client_id: p.client_id,
        body: p.body,
        reply_to_id: p.reply_to_id || null,
        thread_root_id: p.thread_root_id || null,
        attachment_ids: (p.attachments || []).map((a) => a.id),
      })
    }
  })

  setInterval(renderTyping, 1000)

  subscribe(scheduleRender)
  render()
}

import * as api from '../api.js'
import * as socket from '../socket.js'
import { state, subscribe, notify, user, mergeMessage } from '../store.js'
import { renderMarkdown } from '../render.js'
import { drawIcons, el, icon } from './dom.js'

const TYPING_INTERVAL = 3000
const MENTION_PATTERN = /(^|\s)@([a-z0-9_-]*)$/i

function readDraft(key) {
  try {
    return localStorage.getItem(key) || ''
  } catch {
    return ''
  }
}

function writeDraft(key, value) {
  try {
    if (value) localStorage.setItem(key, value)
    else localStorage.removeItem(key)
  } catch {}
}

export function createComposer(options) {
  const draftPrefix = options.draftPrefix
  const maxHeight = options.maxHeight || 320

  let rootEl = null
  let typingEl = null
  let replyEl = null
  let chipsEl = null
  let fieldEl = null
  let textarea = null
  let previewEl = null
  let previewButton = null
  let sendButton = null
  let noticeEl = null
  let mentionEl = null
  let fileInput = null

  let mountedKey = null
  let attachments = []
  let previewOpen = false
  let mentionOpen = false
  let mentionIndex = 0
  let mentionMatches = []
  let mentionStart = 0
  let lastTyping = 0
  let draftTimer = 0
  let previewTimer = 0
  let typingTimer = 0

  function target() {
    return options.target()
  }

  function container() {
    const t = target()
    return t.containerId ? state.containers.get(t.containerId) : null
  }

  function draftKey() {
    const t = target()
    return draftPrefix + (t.threadRootId || t.containerId || 'none')
  }

  function autosize() {
    if (!textarea.offsetParent) return
    textarea.style.height = 'auto'
    textarea.style.height = Math.min(textarea.scrollHeight, maxHeight) + 'px'
  }

  function chipNode(entry, index) {
    const chip = el('div', 'flex h-6 items-center gap-1 rounded-full bg-surface1 pl-2.5 pr-1 text-xs')
    chip.appendChild(el('span', 'max-w-40 truncate text-subtext0', entry.name))
    if (entry.error) chip.appendChild(el('span', 'shrink-0 text-red', 'failed'))
    else if (!entry.attachment) chip.appendChild(el('span', 'shrink-0 text-yellow', Math.round(entry.progress * 100) + '%'))
    else chip.appendChild(icon('check', 'h-3.5 w-3.5 shrink-0 text-green'))
    const remove = el('button', 'grid h-5 w-5 shrink-0 place-items-center rounded-full text-overlay1 transition-colors hover:text-red')
    remove.type = 'button'
    remove.title = 'Remove ' + entry.name
    remove.setAttribute('aria-label', 'Remove ' + entry.name)
    remove.appendChild(icon('x', 'h-3.5 w-3.5'))
    remove.addEventListener('click', () => {
      attachments.splice(index, 1)
      renderChips()
      updateSendState()
    })
    chip.appendChild(remove)
    return chip
  }

  function renderChips() {
    chipsEl.replaceChildren()
    chipsEl.classList.toggle('hidden', attachments.length === 0)
    attachments.forEach((entry, index) => chipsEl.appendChild(chipNode(entry, index)))
    drawIcons(chipsEl)
  }

  function renderReply() {
    replyEl.replaceChildren()
    const id = options.replyTo ? options.replyTo() : null
    replyEl.classList.toggle('hidden', !id)
    if (!id) return
    const list = state.messages.get(target().containerId) || []
    const parent = list.find((m) => m.id === id)
    replyEl.appendChild(el('span', 'shrink-0 text-overlay1', 'Replying to'))
    replyEl.appendChild(el('span', 'min-w-0 truncate font-medium text-text', parent ? user(parent.author_id).display_name : 'a message'))
    const cancel = el('button', 'ml-auto grid h-5 w-5 shrink-0 place-items-center rounded text-overlay1 transition-colors hover:text-text')
    cancel.type = 'button'
    cancel.title = 'Stop replying'
    cancel.setAttribute('aria-label', 'Stop replying')
    cancel.appendChild(icon('x', 'h-3.5 w-3.5'))
    cancel.addEventListener('click', () => {
      state.current.replyToId = null
      notify('current')
    })
    replyEl.appendChild(cancel)
    drawIcons(replyEl)
  }

  function renderTyping() {
    const t = target()
    const map = t.containerId ? state.typing.get(t.containerId) : null
    const rootId = t.threadRootId || null
    const now = Date.now()
    const names = []
    if (map) {
      for (const [userId, entry] of map) {
        if (entry.expiry < now) continue
        if ((entry.threadRootId || null) !== rootId) continue
        if (state.me && userId === state.me.id) continue
        names.push(user(userId).display_name)
      }
    }
    typingEl.textContent = names.length === 1
      ? names[0] + ' is typing'
      : names.length ? names.slice(0, 3).join(', ') + ' are typing' : ''
    scheduleTypingSweep(names.length > 0)
  }

  function scheduleTypingSweep(active) {
    if (!active) {
      clearTimeout(typingTimer)
      typingTimer = 0
      return
    }
    if (typingTimer) return
    typingTimer = setTimeout(() => {
      typingTimer = 0
      renderTyping()
    }, 1000)
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
      if (!u.handle.toLowerCase().startsWith(q) && !(u.display_name || '').toLowerCase().includes(q)) continue
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
      const row = el('button', 'flex w-full items-baseline gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-surface0'
        + (index === mentionIndex ? ' bg-surface0' : ''))
      row.type = 'button'
      row.setAttribute('role', 'option')
      row.setAttribute('aria-selected', index === mentionIndex ? 'true' : 'false')
      row.appendChild(el('span', 'font-mono text-[0.8125rem] ' + (entry.kind === 'agent' ? 'text-lavender' : 'text-blue'), '@' + entry.handle))
      row.appendChild(el('span', 'min-w-0 truncate text-xs text-overlay1', entry.display_name))
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

  function setPreview(open) {
    previewOpen = open
    clearTimeout(previewTimer)
    previewTimer = 0
    previewEl.classList.toggle('hidden', !open)
    previewButton.setAttribute('aria-pressed', open ? 'true' : 'false')
    previewButton.classList.toggle('text-mauve', open)
    previewButton.title = open ? 'Hide the markdown preview' : 'Preview markdown'
    if (open) previewEl.replaceChildren(renderMarkdown(textarea.value))
    else previewEl.replaceChildren()
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
    const t = target()
    if (!t.containerId) return
    const now = Date.now()
    if (now - lastTyping < TYPING_INTERVAL) return
    lastTyping = now
    socket.send('typing', { container_id: t.containerId, thread_root_id: t.threadRootId || null })
  }

  async function submit() {
    const c = container()
    if (!c || c.archived_at) return
    const t = target()
    const body = textarea.value.trim()
    const ids = attachments.filter((a) => a.attachment).map((a) => a.attachment.id)
    if (!body && !ids.length) return

    const clientId = crypto.randomUUID()
    const replyToId = options.replyTo ? options.replyTo() : null
    const payload = {
      container_id: c.id,
      client_id: clientId,
      body,
      reply_to_id: replyToId || null,
      thread_root_id: t.threadRootId || null,
      attachment_ids: ids,
    }
    const optimistic = {
      id: 'pending:' + clientId,
      container_id: c.id,
      client_id: clientId,
      author_id: state.me ? state.me.id : null,
      body,
      reply_to_id: payload.reply_to_id,
      thread_root_id: payload.thread_root_id,
      thread_reply_count: 0,
      created_at: new Date().toISOString(),
      attachments: attachments.filter((a) => a.attachment).map((a) => a.attachment),
      pending: true,
    }
    state.pending.set(clientId, optimistic)

    textarea.value = ''
    attachments = []
    lastTyping = 0
    if (options.replyTo) state.current.replyToId = null
    writeDraft(draftKey(), '')
    renderChips()
    autosize()
    updateSendState()
    closeMentions()
    setPreview(false)
    notify('pending', 'current')

    if (socket.isLive()) {
      socket.send('send', payload)
      return
    }
    try {
      const saved = await api.post('/api/containers/' + c.id + '/messages', payload)
      mergeMessage(saved)
      notify('pending', 'messages')
    } catch {
      optimistic.pending = false
      optimistic.failed = true
      notify('pending')
    }
  }

  function renderNotice(c) {
    const archived = Boolean(c && c.archived_at)
    const missing = !c
    noticeEl.classList.toggle('hidden', !archived && !missing)
    fieldEl.classList.toggle('hidden', archived || missing)
    if (archived || missing) setPreview(false)
    typingEl.classList.toggle('hidden', archived || missing)
    if (archived) noticeEl.textContent = options.archivedNotice
    else if (missing) noticeEl.textContent = options.emptyNotice
  }

  function render() {
    const c = container()
    const key = draftKey()
    if (key !== mountedKey) {
      if (mountedKey) writeDraft(mountedKey, textarea.value)
      mountedKey = key
      textarea.value = readDraft(key)
      attachments = []
      closeMentions()
      renderChips()
      if (previewOpen) setPreview(true)
    }
    textarea.placeholder = options.placeholder(c)
    renderNotice(c)
    autosize()
    renderReply()
    renderTyping()
    updateSendState()
  }

  function mount(root) {
    rootEl = root
    rootEl.className = options.railClass || 'shrink-0 px-4 pb-6'

    noticeEl = el('p', 'hidden rounded-xl bg-surface0 px-3 py-2 text-sm text-overlay1')
    rootEl.appendChild(noticeEl)

    fieldEl = el('div', 'relative rounded-xl bg-surface0 focus-within:ring-1 focus-within:ring-surface2')

    mentionEl = el('div', 'absolute bottom-full left-2 z-20 mb-2 hidden w-72 space-y-0.5 rounded-lg bg-base p-1 shadow-pop')
    mentionEl.setAttribute('role', 'listbox')
    mentionEl.setAttribute('aria-label', 'Mention suggestions')
    fieldEl.appendChild(mentionEl)

    replyEl = el('div', 'hidden h-9 items-center gap-2 rounded-t-xl bg-surface1 px-3 text-xs')
    fieldEl.appendChild(replyEl)

    chipsEl = el('div', 'hidden flex-wrap gap-2 px-3 pt-3')
    fieldEl.appendChild(chipsEl)

    const bar = el('div', 'flex items-end gap-1 px-2 py-1.5')

    fileInput = el('input', 'hidden')
    fileInput.type = 'file'
    fileInput.multiple = true
    fileInput.addEventListener('change', () => {
      addFiles(Array.from(fileInput.files || []))
      fileInput.value = ''
    })
    bar.appendChild(fileInput)

    const attach = el('button', 'grid h-9 w-9 shrink-0 place-items-center rounded-lg text-overlay1 transition-colors hover:text-text focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve')
    attach.type = 'button'
    attach.title = 'Attach a file'
    attach.setAttribute('aria-label', 'Attach a file')
    attach.appendChild(icon('paperclip', 'h-5 w-5'))
    attach.addEventListener('click', () => fileInput.click())
    bar.appendChild(attach)

    textarea = el('textarea', 'max-h-80 min-h-9 flex-1 resize-none bg-transparent py-2 text-message text-text placeholder:text-overlay1 focus:outline-none')
    textarea.rows = 1
    textarea.setAttribute('aria-label', options.fieldLabel || 'Write a message')
    bar.appendChild(textarea)

    previewButton = el('button', 'grid h-9 w-9 shrink-0 place-items-center rounded-lg text-overlay1 transition-colors hover:text-text focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve')
    previewButton.type = 'button'
    previewButton.title = 'Preview markdown'
    previewButton.setAttribute('aria-label', 'Preview markdown')
    previewButton.setAttribute('aria-pressed', 'false')
    previewButton.appendChild(icon('eye', 'h-5 w-5'))
    previewButton.addEventListener('click', () => setPreview(!previewOpen))
    bar.appendChild(previewButton)

    sendButton = el('button', 'grid h-9 w-9 shrink-0 place-items-center rounded-lg text-mauve transition-colors hover:bg-surface1 disabled:text-overlay0 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve')
    sendButton.type = 'button'
    sendButton.title = 'Send. Shift and Enter adds a line'
    sendButton.setAttribute('aria-label', 'Send. Shift and Enter adds a line')
    sendButton.disabled = true
    sendButton.appendChild(icon('send-horizontal', 'h-5 w-5'))
    sendButton.addEventListener('click', submit)
    bar.appendChild(sendButton)

    fieldEl.appendChild(bar)
    rootEl.appendChild(fieldEl)

    previewEl = el('div', 'markdown-body mt-2 hidden max-h-64 overflow-y-auto rounded-xl bg-base p-3 text-message text-subtext0')
    rootEl.appendChild(previewEl)

    typingEl = el('p', 'h-5 px-2 pt-1 text-xs text-overlay1')
    typingEl.setAttribute('aria-live', 'polite')
    rootEl.appendChild(typingEl)

    drawIcons(rootEl)

    textarea.addEventListener('input', () => {
      autosize()
      updateSendState()
      updateMentions()
      schedulePreview()
      maybeTyping()
      clearTimeout(draftTimer)
      const key = draftKey()
      draftTimer = setTimeout(() => writeDraft(key, textarea.value), 300)
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
      if (e.key === 'Escape' && typeof options.onEscape === 'function') {
        e.preventDefault()
        options.onEscape()
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
      fieldEl.classList.add('ring-2', 'ring-mauve')
    })
    rootEl.addEventListener('dragleave', () => fieldEl.classList.remove('ring-2', 'ring-mauve'))
    rootEl.addEventListener('drop', (e) => {
      e.preventDefault()
      fieldEl.classList.remove('ring-2', 'ring-mauve')
      const files = Array.from(e.dataTransfer ? e.dataTransfer.files : [])
      if (files.length) addFiles(files)
    })

    window.addEventListener('pagehide', () => {
      if (mountedKey) writeDraft(mountedKey, textarea.value)
    })

    render()
  }

  return { mount, render, focus: () => textarea && textarea.focus() }
}

function placeholderForContainer(c) {
  if (!c) return 'Message'
  if (c.kind === 'channel') return 'Message #' + c.slug
  const ids = (c.participants || []).filter((id) => id !== (state.me && state.me.id))
  if (ids.length === 1) return 'Message @' + user(ids[0]).handle
  if (ids.length > 1) return 'Message ' + ids.map((id) => user(id).display_name).join(', ')
  return 'Message'
}

export function mount(root) {
  const composer = createComposer({
    draftPrefix: 'isane:draft:',
    target: () => ({ containerId: state.current.containerId, threadRootId: null }),
    replyTo: () => state.current.replyToId,
    placeholder: placeholderForContainer,
    archivedNotice: 'This channel is archived. History stays readable and new messages are blocked.',
    emptyNotice: 'Select a channel or conversation to start writing.',
    onEscape: () => {
      if (!state.current.replyToId) return
      state.current.replyToId = null
      notify('current')
    },
  })
  composer.mount(root)

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

  subscribe(composer.render)
}

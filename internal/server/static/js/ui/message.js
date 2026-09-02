import * as socket from '../socket.js'
import { state, user, findMessage } from '../store.js'
import { renderInto, plainText } from '../render.js'
import { avatarNode, clockLabel, drawIcons, el, icon, relativeLabel, sizeLabel, stampLabel, timeNode } from './dom.js'
import { confirmModal } from './modal.js'

export const GROUP_WINDOW = 7 * 60 * 1000

function mentionsMe(m) {
  return Boolean(state.me && Array.isArray(m.mentions) && m.mentions.includes(state.me.id))
}

function attachmentNode(a) {
  if (a.state !== 'ready') {
    const wrap = el('div', 'mt-2 flex max-w-md items-center gap-2 rounded-xl bg-base px-3 py-2 text-xs')
    wrap.appendChild(el('span', 'min-w-0 flex-1 truncate text-subtext0', a.original_name || 'Attachment'))
    if (a.state === 'failed') {
      const link = el('a', 'shrink-0 text-red', 'failed, download')
      link.href = '/api/attachments/' + a.id
      link.download = a.original_name || ''
      wrap.appendChild(link)
    } else {
      wrap.appendChild(icon('loader-circle', 'h-3.5 w-3.5 shrink-0 animate-spin text-yellow'))
    }
    return wrap
  }

  const href = '/api/attachments/' + a.id

  if (a.kind === 'image') {
    const link = el('a', 'mt-2 block max-w-md overflow-hidden rounded-xl ring-1 ring-edge')
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
    const video = el('video', 'mt-2 block w-full max-w-md rounded-xl ring-1 ring-edge')
    video.controls = true
    video.preload = 'metadata'
    video.poster = href + '/thumb'
    video.src = href
    if (a.width && a.height) {
      video.width = a.width
      video.height = a.height
    }
    return video
  }

  if (a.kind === 'audio') {
    const wrap = el('div', 'mt-2 max-w-md rounded-xl bg-base p-2')
    wrap.appendChild(el('p', 'truncate text-xs text-subtext0', a.original_name || 'Audio'))
    const audio = el('audio', 'mt-1 w-full')
    audio.controls = true
    audio.preload = 'metadata'
    audio.src = href
    wrap.appendChild(audio)
    return wrap
  }

  const row = el('a', 'mt-2 flex max-w-md items-center gap-3 rounded-xl bg-base px-3 py-2 transition-colors hover:bg-surface0')
  row.href = href
  row.download = a.original_name || ''
  const meta = el('div', 'min-w-0 flex-1')
  meta.appendChild(el('p', 'truncate text-sm text-text', a.original_name || 'File'))
  meta.appendChild(el('p', 'text-xs text-overlay1', sizeLabel(a.size_bytes)))
  row.appendChild(meta)
  row.appendChild(icon('download', 'h-4 w-4 shrink-0 text-blue'))
  return row
}

function replyQuoteNode(m, ctx) {
  const parent = findMessage(m.reply_to_id)
  const wrap = el('div', 'mb-1 flex items-center gap-1 pl-14 text-xs text-overlay1')
  wrap.appendChild(icon('corner-up-right', 'h-3 w-3 shrink-0'))
  if (!parent) {
    wrap.appendChild(el('span', 'truncate', 'Reply to an earlier message'))
    return wrap
  }
  wrap.appendChild(el('span', 'shrink-0 font-medium text-subtext1', user(parent.author_id).display_name))
  const excerpt = plainText(parent.body || '')
  wrap.appendChild(el('span', 'truncate', excerpt.length > 140 ? excerpt.slice(0, 140) + '...' : excerpt))
  if (parent.seq && ctx.onJump) {
    wrap.classList.add('cursor-pointer')
    wrap.addEventListener('click', () => ctx.onJump(parent.seq))
  }
  return wrap
}

function threadSummaryNode(m, ctx) {
  const button = el('button', 'mt-1 inline-flex h-7 items-center gap-2 rounded-md bg-surface0/50 py-1 pl-2.5 pr-2 transition-colors hover:bg-surface0 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve')
  button.type = 'button'
  const count = m.thread_reply_count
  button.appendChild(el('span', 'text-xs font-semibold text-blue', count === 1 ? '1 reply' : count + ' replies'))
  if (m.thread_last_reply_at) {
    button.appendChild(el('span', 'text-xs text-overlay1', 'last ' + relativeLabel(m.thread_last_reply_at)))
  }
  button.appendChild(icon('chevron-right', 'h-3.5 w-3.5 text-overlay1'))
  button.addEventListener('click', () => ctx.onThread(m.id))
  return button
}

function editorNode(m, ctx) {
  const wrap = el('div', 'mt-1')
  const box = el('textarea', 'w-full resize-y rounded-xl bg-surface0 px-3 py-2 font-mono text-[0.8125rem] text-text focus:outline-none focus:ring-1 focus:ring-mauve')
  box.value = m.body || ''
  box.rows = Math.min(12, (m.body || '').split('\n').length + 1)
  box.setAttribute('aria-label', 'Edit the message')

  const commit = () => {
    const body = box.value.trim()
    if (body && body !== m.body) socket.send('edit', { message_id: m.id, body })
    ctx.endEdit(m.id)
  }
  const abandon = () => ctx.endEdit(m.id)

  const actions = el('div', 'mt-2 flex items-center gap-2')
  const save = el('button', 'h-8 rounded-lg bg-mauve px-3 text-xs font-semibold text-crust transition-colors hover:brightness-110', 'Save')
  save.type = 'button'
  save.addEventListener('click', commit)
  const cancel = el('button', 'h-8 rounded-lg px-3 text-xs text-subtext0 transition-colors hover:bg-surface0 hover:text-text', 'Cancel')
  cancel.type = 'button'
  cancel.addEventListener('click', abandon)
  actions.append(save, cancel)

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

  wrap.append(box, actions)
  queueMicrotask(() => box.focus())
  return wrap
}

function messageActions(m, ctx) {
  const mine = state.me && m.author_id === state.me.id
  const admin = state.me && state.me.is_admin
  const out = []
  if (ctx.onReply) out.push({ icon: 'corner-up-left', label: 'Reply', run: () => ctx.onReply(m) })
  if (ctx.showThread) {
    out.push({ icon: 'message-square-text', label: 'Reply in thread', run: () => ctx.onThread(m.thread_root_id || m.id) })
  }
  if (mine) out.push({ icon: 'pen-line', label: 'Edit', run: () => ctx.startEdit(m.id) })
  if (mine || admin) {
    out.push({
      icon: 'trash-2',
      label: 'Delete',
      tone: 'hover:text-red',
      run: async () => {
        const ok = await confirmModal({
          title: 'Delete this message',
          message: 'The message is removed for everyone who can read it.',
          confirmLabel: 'Delete',
          destructive: true,
          icon: 'trash-2',
        })
        if (ok) socket.send('delete', { message_id: m.id })
      },
    })
  }
  return out
}

function actionsNode(m, ctx) {
  const actions = messageActions(m, ctx)
  if (!actions.length) return null

  const bar = el('div', 'absolute -top-5 right-4 z-10 hidden items-center gap-0.5 rounded-lg bg-base p-0.5 shadow-pop group-hover/msg:flex group-focus-within/msg:flex md:-top-3')
  for (const action of actions) {
    const button = el('button', 'grid h-11 w-11 place-items-center rounded-md text-overlay1 transition-colors hover:bg-surface0 hover:text-text focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve md:h-7 md:w-7 ' + (action.tone || ''))
    button.type = 'button'
    button.title = action.label
    button.setAttribute('aria-label', action.label)
    button.appendChild(icon(action.icon, 'h-4 w-4'))
    button.addEventListener('click', action.run)
    bar.appendChild(button)
  }
  return bar
}

function liveCallOf(m) {
  if (!m.call_id) return null
  const call = state.calls.get(m.container_id)
  return call && call.id === m.call_id ? call : null
}

function systemNode(m) {
  const article = el('article', 'flex flex-wrap items-center justify-center gap-2 px-4 py-1 text-center text-xs text-overlay1')
  article.appendChild(el('span', '', plainText(m.body || '')))
  if (!liveCallOf(m)) return article
  const join = el('button', 'rounded-full bg-mauve px-2.5 py-0.5 text-xs font-semibold text-crust transition-colors hover:brightness-110 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve', 'Join')
  join.type = 'button'
  join.addEventListener('click', () =>
    window.dispatchEvent(new CustomEvent('isane:call-start', { detail: { containerId: m.container_id } })))
  article.appendChild(join)
  return article
}

export function messageNode(view, ctx) {
  const m = view.m
  if (m.is_system) return systemNode(m)

  const mention = mentionsMe(m)
  const article = el('article', 'group/msg relative px-4 py-0.5 hover:bg-surface0/40'
    + (view.head ? ' mt-4' : '')
    + (mention ? ' border-l-2 border-red bg-red/8 pl-[14px]' : ''))
  article.tabIndex = 0

  if (m.deleted_at) {
    article.appendChild(el('p', 'pl-14 text-message italic text-overlay1', 'Message deleted'))
    return article
  }

  if (m.reply_to_id && view.head) article.appendChild(replyQuoteNode(m, ctx))

  const row = el('div', 'flex gap-4')
  const author = user(m.author_id)

  if (view.head) {
    row.appendChild(avatarNode(author, 'mt-0.5 h-10 w-10'))
  } else {
    row.appendChild(timeNode(
      m.created_at,
      'w-10 shrink-0 pt-1 text-right text-micro leading-none tabular-nums text-overlay0 opacity-0 group-hover/msg:opacity-100 group-focus-within/msg:opacity-100',
      clockLabel(m.created_at)
    ))
  }

  const main = el('div', 'min-w-0 flex-1')

  if (view.head) {
    const head = el('div', 'flex flex-wrap items-baseline gap-2')
    head.appendChild(el('span', 'text-message font-semibold ' + (author.kind === 'agent' ? 'text-lavender' : 'text-text'), author.display_name))
    if (author.kind === 'agent') {
      head.appendChild(el('span', 'rounded bg-lavender/15 px-1 text-micro font-semibold uppercase tracking-widest text-lavender', 'Agent'))
    }
    head.appendChild(timeNode(m.created_at, 'text-xs text-overlay1', stampLabel(m.created_at)))
    if (m.edited_at) head.appendChild(el('span', 'text-xs text-overlay1', 'edited'))
    if (m.pending) head.appendChild(el('span', 'text-xs text-yellow', 'sending'))
    if (m.failed) head.appendChild(el('span', 'text-xs text-red', 'failed'))
    main.appendChild(head)
  } else if (m.pending || m.failed) {
    main.appendChild(el('span', 'text-xs ' + (m.failed ? 'text-red' : 'text-yellow'), m.failed ? 'failed' : 'sending'))
  }

  if (ctx.isEditing(m.id)) {
    main.appendChild(editorNode(m, ctx))
  } else {
    const host = el('div', 'markdown-body text-message text-subtext0')
    main.appendChild(host)
    renderInto(host, m.body || '')
  }

  const attachments = m.attachments || []
  if (attachments.length) {
    const wrap = el('div', 'flex flex-col items-start')
    for (const a of attachments) wrap.appendChild(attachmentNode(a))
    main.appendChild(wrap)
  }

  if (ctx.showThread && m.thread_reply_count > 0) main.appendChild(threadSummaryNode(m, ctx))

  row.appendChild(main)
  article.appendChild(row)
  if (!m.pending) {
    const actions = actionsNode(m, ctx)
    if (actions) article.appendChild(actions)
  }
  drawIcons(article)
  return article
}

export function messageSignature(view, ctx) {
  const m = view.m
  return [
    view.head,
    m.body,
    m.edited_at,
    m.deleted_at,
    m.thread_reply_count,
    m.thread_last_reply_at,
    m.pending,
    m.failed,
    m.is_system,
    ctx.isEditing(m.id),
    mentionsMe(m),
    Boolean(liveCallOf(m)),
    (m.attachments || []).map((a) => a.id + ':' + a.state).join(','),
  ].join('|')
}

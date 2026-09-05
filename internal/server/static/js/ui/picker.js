import * as api from '../api.js'
import { state, notify, user, upsertContainer } from '../store.js'
import { avatarNode, containerPath, drawIcons, el, icon, navigate, presenceDot, textButton } from './dom.js'
import { openModal } from './modal.js'

const RECENT_LIMIT = 10

function partnerRank() {
  const rank = new Map()
  for (const c of state.containers.values()) {
    if (c.kind !== 'conversation') continue
    for (const id of c.participants || []) {
      if (state.me && id === state.me.id) continue
      rank.set(id, Math.max(rank.get(id) ?? 0, c.last_seq || 0))
    }
  }
  return rank
}

function candidates(query, rank) {
  const q = query.trim().toLowerCase()
  const out = []
  for (const u of state.users.values()) {
    if (u.kind !== 'human' || u.deactivated_at) continue
    if (state.me && u.id === state.me.id) continue
    if (q && !u.handle.toLowerCase().includes(q) && !(u.display_name || '').toLowerCase().includes(q)) continue
    out.push(u)
  }
  out.sort((a, b) => {
    const ra = rank.get(a.id) ?? -1
    const rb = rank.get(b.id) ?? -1
    if (ra !== rb) return rb - ra
    return (a.display_name || '').localeCompare(b.display_name || '')
  })
  if (!q) return out.slice(0, RECENT_LIMIT)
  return out
}

export function openPicker() {
  const selection = []
  let matches = []
  let cursor = 0
  let input = null
  let chipsEl = null
  let listEl = null
  let start = null

  const label = () => {
    if (selection.length === 1) return 'Message @' + user(selection[0]).handle
    if (selection.length > 1) return 'Start group message'
    return 'Start a conversation'
  }

  const toggle = (id) => {
    const at = selection.indexOf(id)
    if (at >= 0) selection.splice(at, 1)
    else selection.push(id)
    input.value = ''
    refresh()
    input.focus()
  }

  const submit = async () => {
    if (!selection.length) return
    start.disabled = true
    handle.clearError()
    try {
      const created = await api.post('/api/conversations', { user_ids: selection.slice() })
      const view = upsertContainer(created)
      notify('containers')
      handle.close()
      state.current.containerId = view.id
      state.current.threadRootId = null
      navigate(containerPath(view))
      notify('current')
    } catch (err) {
      handle.fail(err.message || 'Could not start the conversation.')
      start.disabled = false
    }
  }

  const renderChips = () => {
    for (const stale of Array.from(chipsEl.querySelectorAll('[data-chip]'))) stale.remove()
    for (const id of selection) {
      const u = user(id)
      const chip = el('span', 'flex h-6 items-center gap-1 rounded bg-surface1 pl-1 pr-1.5 text-xs text-text')
      chip.dataset.chip = '1'
      chip.appendChild(avatarNode(u, 'h-4 w-4', 'text-micro'))
      chip.appendChild(el('span', 'max-w-32 truncate', u.display_name))
      const remove = el('button', 'text-overlay1 transition-colors hover:text-red')
      remove.type = 'button'
      remove.title = 'Remove ' + u.display_name
      remove.setAttribute('aria-label', 'Remove ' + u.display_name)
      remove.appendChild(icon('x', 'h-3 w-3'))
      remove.addEventListener('click', () => toggle(id))
      chip.appendChild(remove)
      chipsEl.insertBefore(chip, input)
    }
    drawIcons(chipsEl)
  }

  const renderList = () => {
    listEl.replaceChildren()
    if (!matches.length) {
      listEl.appendChild(el('p', 'px-2 py-6 text-center text-sm text-subtext0', 'No matching people.'))
      return
    }
    matches.forEach((u, index) => {
      const picked = selection.includes(u.id)
      const row = el('button', 'flex h-11 w-full items-center gap-3 rounded-md px-2 text-left transition-colors hover:bg-surface0' + (index === cursor ? ' bg-surface0' : ''))
      row.type = 'button'
      row.setAttribute('role', 'option')
      row.setAttribute('aria-selected', index === cursor ? 'true' : 'false')
      const stack = el('span', 'relative shrink-0')
      stack.appendChild(avatarNode(u, 'h-7 w-7', 'text-xs'))
      const dot = presenceDot(u.id, 'ring-mantle')
      dot.classList.add('absolute', '-bottom-0.5', '-right-0.5')
      stack.appendChild(dot)
      row.appendChild(stack)
      row.appendChild(el('span', 'min-w-0 truncate text-sm text-text', u.display_name))
      row.appendChild(el('span', 'min-w-0 truncate text-xs text-overlay1', '@' + u.handle))
      if (picked) row.appendChild(icon('check', 'ml-auto h-4 w-4 shrink-0 text-mauve'))
      row.addEventListener('click', () => toggle(u.id))
      listEl.appendChild(row)
    })
    drawIcons(listEl)
  }

  const refresh = () => {
    matches = candidates(input.value, partnerRank())
    cursor = Math.min(cursor, Math.max(0, matches.length - 1))
    renderChips()
    renderList()
    start.textContent = label()
    start.disabled = selection.length === 0
  }

  start = textButton('Start a conversation', submit, 'primary')

  const handle = openModal({
    title: 'Start a conversation',
    icon: 'square-pen',
    build(body) {
      const fieldWrap = el('div', 'flex min-h-9 flex-wrap items-center gap-1.5 rounded-lg bg-surface0 p-1.5 focus-within:ring-1 focus-within:ring-mauve')
      input = el('input', 'h-6 min-w-32 flex-1 bg-transparent px-1 text-sm pointer-coarse:text-[1rem] text-text placeholder:text-overlay1 focus:outline-none')
      input.type = 'text'
      input.placeholder = 'Type a name'
      input.setAttribute('aria-label', 'Find people')
      fieldWrap.appendChild(input)
      chipsEl = fieldWrap
      body.appendChild(fieldWrap)

      listEl = el('div', 'mt-2 max-h-72 space-y-0.5 overflow-y-auto')
      listEl.setAttribute('role', 'listbox')
      listEl.setAttribute('aria-label', 'People')
      body.appendChild(listEl)

      input.addEventListener('input', () => {
        cursor = 0
        refresh()
      })
      input.addEventListener('keydown', (e) => {
        if (e.key === 'ArrowDown') {
          e.preventDefault()
          cursor = matches.length ? (cursor + 1) % matches.length : 0
          renderList()
          return
        }
        if (e.key === 'ArrowUp') {
          e.preventDefault()
          cursor = matches.length ? (cursor - 1 + matches.length) % matches.length : 0
          renderList()
          return
        }
        if (e.key === 'Enter') {
          e.preventDefault()
          if (matches[cursor]) toggle(matches[cursor].id)
          return
        }
        if (e.key === 'Backspace' && !input.value && selection.length) {
          e.preventDefault()
          toggle(selection[selection.length - 1])
        }
      })
    },
    actions: [start],
  })

  refresh()
  input.focus()
}

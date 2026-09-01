import { state, subscribe, notify } from '../store.js'
import * as api from '../api.js'

let rootEl = null
let shellEl = null
let gateEl = null
let gridEl = null

const sections = new Map()

let loadedFor = null
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

function onAdminRoute() {
  return location.pathname === '/admin' || location.pathname.startsWith('/admin/')
}

function isAdmin() {
  return Boolean(state.me && state.me.is_admin)
}

function sizeLabel(n) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = Number(n) || 0
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i += 1
  }
  return (i === 0 ? value : value.toFixed(1)) + ' ' + units[i]
}

function dateLabel(iso) {
  if (!iso) return 'never'
  return new Date(iso).toLocaleString([], { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function humanKey(key) {
  return key.replace(/_/g, ' ').replace(/^./, (c) => c.toUpperCase())
}

function button(label, fn, variant) {
  const cls =
    variant === 'primary'
      ? 'rounded-lg bg-mauve px-3 py-1.5 text-xs font-semibold text-crust disabled:opacity-40'
      : variant === 'danger'
        ? 'rounded-lg border border-surface1 px-2 py-1 text-xs text-red hover:bg-surface0'
        : 'rounded-lg border border-surface1 px-2 py-1 text-xs text-subtext0 hover:bg-surface0 hover:text-text'
  const node = el('button', cls, label)
  node.type = 'button'
  node.addEventListener('click', fn)
  return node
}

function field(placeholder, type) {
  const input = el('input', 'w-full rounded-lg border border-surface1 bg-surface0 px-2 py-1.5 text-sm text-text placeholder:text-overlay0')
  input.type = type || 'text'
  input.placeholder = placeholder
  return input
}

function card(title, description) {
  const wrap = el('section', 'rounded-xl border border-surface0 bg-mantle p-4')
  const head = el('div', 'mb-3')
  head.appendChild(el('h2', 'font-display text-sm font-semibold text-text', title))
  if (description) head.appendChild(el('p', 'text-xs text-overlay1', description))
  wrap.appendChild(head)
  const error = el('p', 'mb-2 hidden text-xs text-red')
  wrap.appendChild(error)
  const body = el('div')
  wrap.appendChild(body)
  return { wrap, body, error }
}

function fail(section, err) {
  section.error.textContent = (err && err.message) || 'Request failed.'
  section.error.classList.remove('hidden')
}

function clearError(section) {
  section.error.classList.add('hidden')
}

function secretBlock(label, value) {
  const wrap = el('div', 'mt-3 rounded-lg border border-peach bg-base p-3')
  wrap.appendChild(el('p', 'text-xs font-semibold text-peach', label + '. This is shown once and is not recoverable.'))
  const row = el('div', 'mt-2 flex items-center gap-2')
  const code = el('code', 'min-w-0 flex-1 break-all font-mono text-xs text-text', value)
  row.appendChild(code)
  const copy = button('Copy', async () => {
    try {
      await navigator.clipboard.writeText(value)
      copy.textContent = 'Copied'
    } catch {
      copy.textContent = 'Select and copy'
    }
  })
  row.appendChild(copy)
  wrap.appendChild(row)
  return wrap
}

function table(headings) {
  const wrap = el('div', 'overflow-x-auto')
  const t = el('table', 'w-full text-left text-xs')
  const thead = el('thead')
  const hrow = el('tr', 'border-b border-surface0')
  for (const h of headings) hrow.appendChild(el('th', 'px-2 py-1.5 font-semibold text-overlay1', h))
  thead.appendChild(hrow)
  t.appendChild(thead)
  const tbody = el('tbody')
  t.appendChild(tbody)
  wrap.appendChild(t)
  return { wrap, tbody }
}

function row(values) {
  const tr = el('tr', 'border-b border-surface0/50')
  for (const v of values) {
    const td = el('td', 'px-2 py-1.5 align-middle')
    if (v instanceof Node) td.appendChild(v)
    else td.appendChild(el('span', 'text-subtext0', v == null ? '' : String(v)))
    tr.appendChild(td)
  }
  return tr
}

function buildUsers() {
  const section = card('Users', 'Create accounts, reset a password, or deactivate someone. Handles are never reused.')

  const form = el('div', 'mb-3 grid gap-2 sm:grid-cols-4')
  const handle = field('handle')
  const display = field('Display name')
  const email = field('email@example.com', 'email')
  form.appendChild(handle)
  form.appendChild(display)
  form.appendChild(email)
  const create = button(
    'Create user',
    async () => {
      clearError(section)
      create.disabled = true
      try {
        await api.post('/api/admin/users', { handle: handle.value.trim(), display_name: display.value.trim(), email: email.value.trim() })
        handle.value = ''
        display.value = ''
        email.value = ''
        await section.load()
      } catch (err) {
        fail(section, err)
      }
      create.disabled = false
    },
    'primary'
  )
  form.appendChild(create)
  section.body.appendChild(form)

  const list = el('div')
  section.body.appendChild(list)

  section.load = async () => {
    try {
      const users = await api.get('/api/admin/users')
      const t = table(['Handle', 'Name', 'Email', 'Role', 'State', ''])
      for (const u of users) {
        const actions = el('div', 'flex flex-wrap gap-1')
        actions.appendChild(
          button(u.is_admin ? 'Revoke admin' : 'Make admin', async () => {
            clearError(section)
            try {
              await api.patch('/api/admin/users/' + u.id, { is_admin: !u.is_admin })
              await section.load()
            } catch (err) {
              fail(section, err)
            }
          })
        )
        actions.appendChild(
          button('Reset password', async () => {
            const password = window.prompt('New password for @' + u.handle)
            if (!password) return
            clearError(section)
            try {
              await api.post('/api/admin/users/' + u.id + '/password', { password })
            } catch (err) {
              fail(section, err)
            }
          })
        )
        if (!u.deactivated_at) {
          actions.appendChild(
            button(
              'Deactivate',
              async () => {
                if (!window.confirm('Deactivate @' + u.handle + '?')) return
                clearError(section)
                try {
                  await api.del('/api/admin/users/' + u.id)
                  await section.load()
                } catch (err) {
                  fail(section, err)
                }
              },
              'danger'
            )
          )
        }
        t.tbody.appendChild(
          row([
            '@' + u.handle,
            u.display_name,
            u.email || '',
            u.is_admin ? 'admin' : 'member',
            u.deactivated_at ? 'deactivated' : 'active',
            actions,
          ])
        )
      }
      list.replaceChildren(t.wrap)
    } catch (err) {
      fail(section, err)
    }
  }

  return section
}

function buildInvites() {
  const section = card('Invites', 'An invite URL is displayed once. The server keeps only its hash.')

  const form = el('div', 'mb-3 grid gap-2 sm:grid-cols-3')
  const note = field('Note, for example new designer')
  const expiry = el('select', 'w-full rounded-lg border border-surface1 bg-surface0 px-2 py-1.5 text-sm text-text')
  for (const pair of [['24h', 'Expires in 1 day'], ['168h', 'Expires in 7 days'], ['720h', 'Expires in 30 days']]) {
    const option = el('option', null, pair[1])
    option.value = pair[0]
    expiry.appendChild(option)
  }
  form.appendChild(note)
  form.appendChild(expiry)
  const create = button(
    'Create invite',
    async () => {
      clearError(section)
      create.disabled = true
      try {
        const invite = await api.post('/api/admin/invites', { note: note.value.trim(), expires_in: expiry.value })
        note.value = ''
        secrets.replaceChildren(secretBlock('Invite URL', invite.url))
        await section.load()
      } catch (err) {
        fail(section, err)
      }
      create.disabled = false
    },
    'primary'
  )
  form.appendChild(create)
  section.body.appendChild(form)

  const secrets = el('div')
  section.body.appendChild(secrets)

  const list = el('div', 'mt-3')
  section.body.appendChild(list)

  section.load = async () => {
    try {
      const invites = await api.get('/api/admin/invites')
      const t = table(['Note', 'Expires', 'Used by', ''])
      for (const invite of invites) {
        const actions = el('div')
        if (!invite.used_at) {
          actions.appendChild(
            button(
              'Revoke',
              async () => {
                clearError(section)
                try {
                  await api.del('/api/admin/invites/' + invite.id)
                  await section.load()
                } catch (err) {
                  fail(section, err)
                }
              },
              'danger'
            )
          )
        }
        t.tbody.appendChild(row([invite.note || '', dateLabel(invite.expires_at), invite.used_at ? dateLabel(invite.used_at) : 'outstanding', actions]))
      }
      list.replaceChildren(t.wrap)
    } catch (err) {
      fail(section, err)
    }
  }

  return section
}

function buildChannels() {
  const section = card('Channels', 'Every active human is a member of every channel. Archiving keeps history and blocks new messages.')

  const form = el('div', 'mb-3 grid gap-2 sm:grid-cols-4')
  const slug = field('slug')
  const name = field('Name')
  const topic = field('Topic')
  form.appendChild(slug)
  form.appendChild(name)
  form.appendChild(topic)
  const create = button(
    'Create channel',
    async () => {
      clearError(section)
      create.disabled = true
      try {
        const channel = await api.post('/api/admin/channels', { slug: slug.value.trim(), name: name.value.trim(), topic: topic.value.trim() })
        state.containers.set(channel.id, channel)
        notify('containers')
        slug.value = ''
        name.value = ''
        topic.value = ''
        section.load()
      } catch (err) {
        fail(section, err)
      }
      create.disabled = false
    },
    'primary'
  )
  form.appendChild(create)
  section.body.appendChild(form)

  const list = el('div')
  section.body.appendChild(list)

  section.load = () => {
    const channels = []
    for (const c of state.containers.values()) if (c.kind === 'channel') channels.push(c)
    channels.sort((a, b) => (a.slug || '').localeCompare(b.slug || ''))

    const t = table(['Slug', 'Name', 'Topic', 'State', ''])
    for (const c of channels) {
      const actions = el('div', 'flex flex-wrap gap-1')
      actions.appendChild(
        button('Rename', async () => {
          const next = window.prompt('New name for #' + c.slug, c.name || '')
          if (next == null) return
          clearError(section)
          try {
            await api.patch('/api/admin/channels/' + c.id, { name: next })
            c.name = next
            notify('containers')
            section.load()
          } catch (err) {
            fail(section, err)
          }
        })
      )
      actions.appendChild(
        button('Set topic', async () => {
          const next = window.prompt('New topic for #' + c.slug, c.topic || '')
          if (next == null) return
          clearError(section)
          try {
            await api.patch('/api/admin/channels/' + c.id, { topic: next })
            c.topic = next
            notify('containers')
            section.load()
          } catch (err) {
            fail(section, err)
          }
        })
      )
      if (!c.archived_at) {
        actions.appendChild(
          button(
            'Archive',
            async () => {
              if (!window.confirm('Archive #' + c.slug + '?')) return
              clearError(section)
              try {
                await api.del('/api/admin/channels/' + c.id)
                c.archived_at = new Date().toISOString()
                notify('containers')
                section.load()
              } catch (err) {
                fail(section, err)
              }
            },
            'danger'
          )
        )
      }
      t.tbody.appendChild(row(['#' + c.slug, c.name || '', c.topic || '', c.archived_at ? 'archived' : 'active', actions]))
    }
    list.replaceChildren(t.wrap)
  }

  return section
}

function buildAgents() {
  const section = card('Agents', 'Reserving a handle displays the claim token once. The daemon registers with it from its owner machine.')

  const form = el('div', 'mb-3 grid gap-2 sm:grid-cols-3')
  const handle = field('handle')
  const display = field('Display name')
  form.appendChild(handle)
  form.appendChild(display)
  const reserve = button(
    'Reserve agent',
    async () => {
      clearError(section)
      reserve.disabled = true
      try {
        const agent = await api.post('/api/admin/agents', { handle: handle.value.trim(), display_name: display.value.trim() })
        handle.value = ''
        display.value = ''
        secrets.replaceChildren(secretBlock('Claim token', agent.claim_token))
        await section.load()
      } catch (err) {
        fail(section, err)
      }
      reserve.disabled = false
    },
    'primary'
  )
  form.appendChild(reserve)
  section.body.appendChild(form)

  const secrets = el('div')
  section.body.appendChild(secrets)

  const list = el('div', 'mt-3')
  section.body.appendChild(list)

  section.load = async () => {
    try {
      const agents = await api.get('/api/admin/agents')
      const t = table(['Handle', 'Name', 'State', 'Last seen', 'History', 'Argv', ''])
      for (const a of agents) {
        const remove = button(
          'Delete',
          async () => {
            if (!window.confirm('Delete agent @' + a.handle + '?')) return
            clearError(section)
            try {
              await api.del('/api/admin/agents/' + a.handle)
              await section.load()
            } catch (err) {
              fail(section, err)
            }
          },
          'danger'
        )
        t.tbody.appendChild(
          row([
            '@' + a.handle,
            a.display_name || '',
            a.state || '',
            dateLabel(a.last_seen_at),
            a.allow_history ? 'allowed' : 'denied',
            (a.argv || []).join(' '),
            remove,
          ])
        )
      }
      list.replaceChildren(t.wrap)
    } catch (err) {
      fail(section, err)
    }
  }

  return section
}

function buildStats() {
  const section = card('Stats and retention', 'Counts, storage, and the configured retention policy.')
  const list = el('div')
  section.body.appendChild(list)

  section.load = async () => {
    try {
      const stats = await api.get('/api/admin/stats')
      const grid = el('dl', 'grid gap-3 sm:grid-cols-3')
      for (const [key, value] of Object.entries(stats)) {
        if (value && typeof value === 'object') {
          for (const [inner, innerValue] of Object.entries(value)) {
            grid.appendChild(statTile(key + '_' + inner, innerValue))
          }
          continue
        }
        grid.appendChild(statTile(key, value))
      }
      list.replaceChildren(grid)
    } catch (err) {
      fail(section, err)
    }
  }

  return section
}

function statTile(key, value) {
  const tile = el('div', 'rounded-lg border border-surface0 bg-base p-3')
  tile.appendChild(el('dt', 'text-xs text-overlay1', humanKey(key)))
  let shown = value
  if (key.endsWith('_bytes')) shown = sizeLabel(value)
  else if (typeof value === 'number') shown = value.toLocaleString()
  else if (typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T/.test(value)) shown = dateLabel(value)
  else if (typeof value === 'boolean') shown = value ? 'yes' : 'no'
  tile.appendChild(el('dd', 'font-display text-lg text-text', String(shown)))
  return tile
}

function loadAll() {
  for (const section of sections.values()) section.load()
}

function render() {
  const visible = onAdminRoute()
  rootEl.classList.toggle('hidden', !visible)
  if (!visible) {
    loadedFor = null
    return
  }

  const admin = isAdmin()
  gateEl.classList.toggle('hidden', admin)
  shellEl.classList.toggle('hidden', !admin)
  if (!admin) {
    gateEl.textContent = state.me ? 'Administrator access is required for this page.' : 'Sign in to continue.'
    return
  }

  if (loadedFor !== state.me.id) {
    loadedFor = state.me.id
    loadAll()
    return
  }
  const channels = sections.get('channels')
  if (channels) channels.load()
}

export function mount(root) {
  rootEl = root
  rootEl.classList.add('hidden')

  const page = el('div', 'h-full overflow-y-auto bg-crust')
  const header = el('div', 'flex items-center gap-3 border-b border-surface0 px-4 py-3')
  header.appendChild(el('h1', 'flex-1 font-display text-lg font-semibold text-text', 'Administration'))
  header.appendChild(
    button('Refresh', () => {
      if (isAdmin()) loadAll()
    })
  )
  header.appendChild(
    button('Back to chat', () => {
      history.pushState(null, '', '/')
      window.dispatchEvent(new CustomEvent('isane:navigate', { detail: { path: '/' } }))
      render()
    })
  )
  page.appendChild(header)

  gateEl = el('p', 'hidden px-4 py-8 text-sm text-overlay1')
  page.appendChild(gateEl)

  shellEl = el('div', 'hidden')
  gridEl = el('div', 'grid gap-4 p-4')
  shellEl.appendChild(gridEl)
  page.appendChild(shellEl)

  sections.set('users', buildUsers())
  sections.set('invites', buildInvites())
  sections.set('channels', buildChannels())
  sections.set('agents', buildAgents())
  sections.set('stats', buildStats())
  for (const section of sections.values()) gridEl.appendChild(section.wrap)

  rootEl.replaceChildren(page)

  window.addEventListener('popstate', render)
  window.addEventListener('isane:navigate', render)

  subscribe(scheduleRender)
  render()
}

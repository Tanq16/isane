import * as api from '../api.js'
import { state, subscribe, notify, upsertContainer } from '../store.js'
import { dateLabel, drawIcons, el, emptyState, field, icon, iconButton, navigate, sizeLabel, textButton } from './dom.js'
import { confirmModal, openModal, promptModal } from './modal.js'
import { slugify } from './settings.js'
import { toast } from './toast.js'

const SECTIONS = [
  ['identities', 'Identities', 'users'],
  ['channels', 'Channels', 'hash'],
  ['invites', 'Invites', 'ticket'],
  ['stats', 'Stats', 'chart-column'],
]

let rootEl = null
let railEl = null
let titleEl = null
let contentEl = null
let gateEl = null
let shellEl = null

const builders = new Map()
const pills = new Map()

let activeSection = null
let frame = 0

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

function sectionFromPath() {
  const rest = location.pathname.replace(/^\/admin\/?/, '')
  const name = rest.split('/')[0]
  return SECTIONS.some(([key]) => key === name) ? name : 'identities'
}

function humanKey(key) {
  return key.replace(/_/g, ' ').replace(/^./, (c) => c.toUpperCase())
}

function panel(title, description) {
  const wrap = el('section', 'min-w-0')
  wrap.appendChild(el('h2', 'text-message font-semibold text-text', title))
  if (description) wrap.appendChild(el('p', 'mt-1 max-w-prose text-sm text-overlay1', description))
  const error = el('p', 'mt-2 hidden text-sm text-red')
  error.setAttribute('role', 'alert')
  wrap.appendChild(error)
  const body = el('div', 'mt-4 min-w-0')
  wrap.appendChild(body)
  return {
    wrap,
    body,
    fail(err) {
      error.textContent = (err && err.message) || 'Request failed.'
      error.classList.remove('hidden')
    },
    clear() {
      error.classList.add('hidden')
    },
  }
}

function table(headings) {
  const wrap = el('div', 'min-w-0 overflow-x-auto')
  const node = el('table', 'w-full min-w-max text-left text-sm')
  const thead = el('thead')
  const hrow = el('tr', 'border-b border-surface0')
  for (const h of headings) {
    hrow.appendChild(el('th', 'px-2 py-2 text-micro font-bold uppercase tracking-widest text-overlay1', h))
  }
  thead.appendChild(hrow)
  const tbody = el('tbody', 'divide-y divide-surface0/50')
  node.append(thead, tbody)
  wrap.appendChild(node)
  return { wrap, tbody }
}

function row(values, muted) {
  const tr = el('tr', 'transition-colors hover:bg-surface0/40')
  for (const v of values) {
    const td = el('td', 'max-w-64 truncate px-2 py-2 align-middle')
    if (v instanceof Node) td.appendChild(v)
    else td.appendChild(el('span', muted ? 'text-overlay1' : 'text-subtext0', v == null ? '' : String(v)))
    tr.appendChild(td)
  }
  return tr
}

function actionButton(name, title, tone, run) {
  const button = el('button', 'grid h-8 w-8 place-items-center rounded-md text-overlay1 transition-colors hover:bg-surface0 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve ' + tone)
  button.type = 'button'
  button.title = title
  button.setAttribute('aria-label', title)
  button.appendChild(icon(name, 'h-4 w-4'))
  button.addEventListener('click', run)
  return button
}

function actionGroup(...buttons) {
  const wrap = el('div', 'flex items-center justify-end gap-0.5')
  for (const b of buttons) {
    if (b) wrap.appendChild(b)
  }
  return wrap
}

function badge(text, tone) {
  return el('span', 'rounded-full px-2 py-0.5 text-micro font-semibold uppercase tracking-widest ' + tone, text)
}

function secretBlock(label, value) {
  const wrap = el('div', 'mt-4 rounded-xl bg-base p-3')
  wrap.appendChild(el('p', 'text-xs font-semibold text-peach', label + '. This is shown once and is not recoverable.'))
  const line = el('div', 'mt-2 flex items-center gap-2')
  line.appendChild(el('code', 'min-w-0 flex-1 break-all font-mono text-[0.8125rem] text-text', value))
  const copy = actionButton('copy', 'Copy', 'hover:text-green', async () => {
    try {
      await navigator.clipboard.writeText(value)
    } catch {
      toast('Select the text and copy it', { severity: 'warning' })
      return
    }
    copy.replaceChildren(icon('check', 'h-4 w-4'))
    copy.classList.add('text-green')
    drawIcons(copy)
    setTimeout(() => {
      copy.replaceChildren(icon('copy', 'h-4 w-4'))
      copy.classList.remove('text-green')
      drawIcons(copy)
    }, 2000)
  })
  line.appendChild(copy)
  wrap.appendChild(line)
  drawIcons(wrap)
  return wrap
}

function labelledGrid(...wraps) {
  const grid = el('div', 'grid min-w-0 gap-3 sm:grid-cols-2 lg:grid-cols-4')
  for (const w of wraps) grid.appendChild(w)
  return grid
}

function buildIdentities() {
  const section = panel('Identities', 'People and agents that can sign in. Handles are never reused.')
  const usersList = el('div', 'min-w-0')
  const agentsList = el('div', 'min-w-0')
  const secrets = el('div')

  const handle = field('Handle', { name: 'admin-user-handle', placeholder: 'ana' })
  const display = field('Display name', { name: 'admin-user-name', placeholder: 'Ana Ruiz' })
  const email = field('Email', { name: 'admin-user-email', type: 'email', placeholder: 'ana@example.com' })
  const create = textButton('Create user', async () => {
    section.clear()
    create.disabled = true
    try {
      const created = await api.post('/api/admin/users', {
        handle: handle.input.value.trim(),
        display_name: display.input.value.trim(),
        email: email.input.value.trim(),
      })
      state.users.set(created.id, created)
      notify('users')
      handle.input.value = ''
      display.input.value = ''
      email.input.value = ''
      await loadUsers()
    } catch (err) {
      section.fail(err)
    }
    create.disabled = false
  }, 'primary')

  const form = el('div', 'mb-6')
  form.appendChild(labelledGrid(handle.wrap, display.wrap, email.wrap, wrapAction(create)))
  section.body.append(form, usersList)

  const agentHandle = field('Handle', { name: 'admin-agent-handle', placeholder: 'helper' })
  const agentName = field('Display name', { name: 'admin-agent-name', placeholder: 'Helper' })
  const reserve = textButton('Reserve agent', async () => {
    section.clear()
    reserve.disabled = true
    try {
      const reserved = await api.post('/api/admin/agents', {
        handle: agentHandle.input.value.trim(),
        display_name: agentName.input.value.trim(),
      })
      const created = reserved.agent && reserved.agent.user
      if (created) {
        state.users.set(created.id, created)
        notify('users')
      }
      secrets.replaceChildren(claimBlock(reserved, agentHandle.input.value.trim()))
      agentHandle.input.value = ''
      agentName.input.value = ''
      await loadAgents()
    } catch (err) {
      section.fail(err)
    }
    reserve.disabled = false
  }, 'secondary')

  const agentsHead = el('div', 'mt-8')
  agentsHead.appendChild(el('h3', 'text-message font-semibold text-text', 'Agents'))
  agentsHead.appendChild(el('p', 'mt-1 max-w-prose text-sm text-overlay1',
    'Reserving a handle shows the claim token once. The daemon registers with it from its own machine.'))
  const agentForm = el('div', 'mt-4')
  agentForm.appendChild(labelledGrid(agentHandle.wrap, agentName.wrap, wrapAction(reserve)))
  section.body.append(agentsHead, agentForm, secrets, agentsList)

  function claimBlock(reserved, requested) {
    const handleName = (reserved.agent && reserved.agent.user && reserved.agent.user.handle) || requested
    const wrap = el('div')
    wrap.appendChild(secretBlock('Claim token', reserved.claim_token))
    wrap.appendChild(secretBlock('Daemon headers',
      'Authorization: Bearer ' + reserved.claim_token + '\nX-Isane-Agent: ' + handleName))
    return wrap
  }

  async function loadUsers() {
    const users = await api.get('/api/admin/users')
    const t = table(['Handle', 'Name', 'Email', 'Role', 'State', ''])
    for (const u of users) {
      const actions = actionGroup(
        actionButton('key-round', 'Reset the password', 'hover:text-peach', async () => {
          const password = await promptModal({
            title: 'Reset the password for @' + u.handle,
            label: 'New password',
            type: 'password',
            confirmLabel: 'Reset',
            icon: 'key-round',
            required: true,
            hint: 'At least 8 characters. The person keeps their handle.',
          })
          if (!password) return
          section.clear()
          try {
            await api.post('/api/admin/users/' + u.id + '/password', { password })
            toast('Password reset for @' + u.handle, { severity: 'success' })
          } catch (err) {
            section.fail(err)
          }
        }),
        actionButton('shield', u.is_admin ? 'Revoke administrator' : 'Make administrator', 'hover:text-mauve', async () => {
          section.clear()
          try {
            await api.put('/api/admin/users/' + u.id + '/admin', { is_admin: !u.is_admin })
            await loadUsers()
          } catch (err) {
            section.fail(err)
          }
        }),
        u.deactivated_at ? null : actionButton('user-round-x', 'Deactivate', 'hover:text-red', async () => {
          const ok = await confirmModal({
            title: 'Deactivate @' + u.handle,
            message: 'They lose every session and can no longer sign in.',
            consequence: 'The handle stays reserved and their messages stay readable.',
            confirmLabel: 'Deactivate',
            destructive: true,
            icon: 'user-round-x',
          })
          if (!ok) return
          section.clear()
          try {
            await api.del('/api/admin/users/' + u.id)
            await loadUsers()
          } catch (err) {
            section.fail(err)
          }
        })
      )
      t.tbody.appendChild(row([
        '@' + u.handle,
        u.display_name,
        u.email || '',
        u.is_admin ? badge('admin', 'bg-mauve/15 text-mauve') : 'member',
        u.deactivated_at ? badge('deactivated', 'bg-surface1 text-overlay1') : 'active',
        actions,
      ], Boolean(u.deactivated_at)))
    }
    usersList.replaceChildren(t.wrap)
    drawIcons(usersList)
  }

  async function loadAgents() {
    const agents = await api.get('/api/admin/agents')
    const t = table(['Handle', 'Name', 'State', 'Last seen', 'History', 'Argv', ''])
    for (const info of agents) {
      const u = info.user || {}
      const a = info.agent || {}
      const actions = actionGroup(
        a.state === 'serving' ? actionButton('phone-off', 'Deregister the daemon', 'hover:text-peach', async () => {
          section.clear()
          try {
            await api.post('/api/admin/agents/' + u.handle + '/deregister', {})
            await loadAgents()
          } catch (err) {
            section.fail(err)
          }
        }) : null,
        actionButton('trash-2', 'Delete the agent', 'hover:text-red', async () => {
          const ok = await confirmModal({
            title: 'Delete agent @' + u.handle,
            message: 'The daemon stops receiving jobs and the reservation is released.',
            confirmLabel: 'Delete',
            destructive: true,
            icon: 'trash-2',
          })
          if (!ok) return
          section.clear()
          try {
            await api.del('/api/admin/agents/' + u.handle)
            await loadAgents()
          } catch (err) {
            section.fail(err)
          }
        })
      )
      t.tbody.appendChild(row([
        '@' + (u.handle || ''),
        u.display_name || '',
        a.state || '',
        dateLabel(a.last_seen_at),
        a.allow_history ? 'allowed' : 'denied',
        (a.argv || []).join(' '),
        actions,
      ]))
    }
    if (!agents.length) t.tbody.appendChild(row(['No agents reserved yet.', '', '', '', '', '', '']))
    agentsList.replaceChildren(t.wrap)
    drawIcons(agentsList)
  }

  section.load = async () => {
    section.clear()
    try {
      await loadUsers()
      await loadAgents()
    } catch (err) {
      section.fail(err)
    }
  }
  return section
}

function wrapAction(button) {
  const wrap = el('div', 'flex items-end')
  button.classList.add('w-full')
  wrap.appendChild(button)
  return wrap
}

function buildChannels() {
  const section = panel('Channels', 'Every active person is a member of every channel. Archiving keeps history and blocks new messages.')
  const list = el('div', 'min-w-0')
  const permission = el('div', 'mb-6 flex items-center justify-between gap-4 rounded-xl bg-surface0 px-4 py-3')

  const name = field('Name', { name: 'admin-channel-name', placeholder: 'Release planning' })
  const slug = field('Address', { name: 'admin-channel-slug', placeholder: 'release-planning' })
  const topic = field('Topic', { name: 'admin-channel-topic', placeholder: 'Optional' })
  let slugTouched = false
  name.input.addEventListener('input', () => {
    if (!slugTouched) slug.input.value = slugify(name.input.value)
  })
  slug.input.addEventListener('input', () => {
    slugTouched = true
  })

  const create = textButton('Create channel', async () => {
    section.clear()
    create.disabled = true
    try {
      const created = await api.post('/api/channels', {
        slug: slug.input.value.trim(),
        name: name.input.value.trim(),
        topic: topic.input.value.trim(),
      })
      upsertContainer(created)
      notify('containers')
      name.input.value = ''
      slug.input.value = ''
      topic.input.value = ''
      slugTouched = false
      await section.load()
    } catch (err) {
      section.fail(err)
    }
    create.disabled = false
  }, 'primary')

  const form = el('div', 'mb-6')
  form.appendChild(labelledGrid(name.wrap, slug.wrap, topic.wrap, wrapAction(create)))
  section.body.append(permission, form, list)

  function renderPermission() {
    permission.replaceChildren()
    const copy = el('div', 'min-w-0')
    copy.appendChild(el('p', 'text-sm font-medium text-text', 'Who can create channels'))
    copy.appendChild(el('p', 'mt-1 text-xs text-overlay1',
      state.settings.allow_member_channels
        ? 'Anyone can create a channel and edit its name or topic.'
        : 'Only an administrator can create a channel or edit one.'))
    permission.appendChild(copy)

    const on = Boolean(state.settings.allow_member_channels)
    const toggle = el('button', 'h-6 w-11 shrink-0 rounded-full transition-colors ' + (on ? 'bg-green' : 'bg-surface2'))
    toggle.type = 'button'
    toggle.setAttribute('role', 'switch')
    toggle.setAttribute('aria-checked', on ? 'true' : 'false')
    toggle.setAttribute('aria-label', 'Anyone can create a channel')
    toggle.appendChild(el('span', 'block h-5 w-5 rounded-full bg-crust transition-transform ' + (on ? 'translate-x-5' : 'translate-x-0.5')))
    toggle.addEventListener('click', async () => {
      section.clear()
      try {
        const saved = await api.put('/api/settings', { allow_member_channels: !on })
        state.settings = Object.assign({}, state.settings, saved)
        notify('settings')
        renderPermission()
      } catch (err) {
        section.fail(err)
      }
    })
    permission.appendChild(toggle)
  }

  section.load = async () => {
    section.clear()
    renderPermission()
    try {
      const channels = await api.get('/api/admin/channels')
      const t = table(['Address', 'Name', 'Topic', 'State', ''])
      for (const c of channels) {
        const actions = actionGroup(
          actionButton('pen-line', 'Rename', 'hover:text-blue', async () => {
            const next = await promptModal({
              title: 'Rename #' + c.slug,
              label: 'Name',
              value: c.name || '',
              confirmLabel: 'Rename',
              required: true,
            })
            if (next == null) return
            section.clear()
            try {
              upsertContainer(await api.patch('/api/channels/' + c.id, { name: next }))
              notify('containers')
              await section.load()
            } catch (err) {
              section.fail(err)
            }
          }),
          actionButton('message-square-text', 'Set the topic', 'hover:text-yellow', async () => {
            const next = await promptModal({
              title: 'Topic for #' + c.slug,
              label: 'Topic',
              value: c.topic || '',
              confirmLabel: 'Save',
            })
            if (next == null) return
            section.clear()
            try {
              upsertContainer(await api.patch('/api/channels/' + c.id, { name: c.name, topic: next }))
              notify('containers')
              await section.load()
            } catch (err) {
              section.fail(err)
            }
          }),
          c.archived_at
            ? actionButton('archive-restore', 'Unarchive', 'hover:text-green', async () => {
              section.clear()
              try {
                await api.post('/api/admin/channels/' + c.id + '/unarchive', {})
                const stored = state.containers.get(c.id)
                if (stored) stored.archived_at = null
                notify('containers')
                await section.load()
              } catch (err) {
                section.fail(err)
              }
            })
            : actionButton('archive', 'Archive', 'hover:text-red', async () => {
              const ok = await confirmModal({
                title: 'Archive #' + c.slug,
                message: 'New messages are blocked and the channel moves to the archived group.',
                consequence: 'History stays readable and the channel can be unarchived.',
                confirmLabel: 'Archive',
                destructive: true,
                icon: 'archive',
              })
              if (!ok) return
              section.clear()
              try {
                await api.del('/api/admin/channels/' + c.id)
                const stored = state.containers.get(c.id)
                if (stored) stored.archived_at = new Date().toISOString()
                notify('containers')
                await section.load()
              } catch (err) {
                section.fail(err)
              }
            })
        )
        t.tbody.appendChild(row([
          '#' + c.slug,
          c.name || '',
          c.topic || '',
          c.archived_at ? badge('archived', 'bg-surface1 text-peach') : 'active',
          actions,
        ], Boolean(c.archived_at)))
      }
      list.replaceChildren(t.wrap)
      drawIcons(list)
    } catch (err) {
      section.fail(err)
    }
  }
  return section
}

function buildInvites() {
  const section = panel('Invites', 'An invite URL is shown once. The server keeps only its hash.')
  const secrets = el('div')
  const list = el('div', 'mt-6 min-w-0')

  const note = field('Note', { name: 'admin-invite-note', placeholder: 'new designer' })
  const expiryWrap = el('label', 'block min-w-0')
  expiryWrap.appendChild(el('span', 'mb-1 block text-xs font-medium text-subtext0', 'Expires'))
  const expiry = el('select', 'h-9 w-full rounded-lg bg-surface0 px-3 text-sm text-text focus:outline-none focus:ring-1 focus:ring-mauve')
  for (const [value, label] of [['86400', 'In 1 day'], ['604800', 'In 7 days'], ['2592000', 'In 30 days']]) {
    const option = el('option', null, label)
    option.value = value
    expiry.appendChild(option)
  }
  expiry.value = '604800'
  expiryWrap.appendChild(expiry)

  const create = textButton('Create invite', async () => {
    section.clear()
    create.disabled = true
    try {
      const invite = await api.post('/api/admin/invites', {
        note: note.input.value.trim(),
        expires_in: Number(expiry.value),
      })
      note.input.value = ''
      secrets.replaceChildren(secretBlock('Invite URL', invite.url))
      await section.load()
    } catch (err) {
      section.fail(err)
    }
    create.disabled = false
  }, 'primary')

  const form = el('div')
  form.appendChild(labelledGrid(note.wrap, expiryWrap, wrapAction(create)))
  section.body.append(form, secrets, list)

  section.load = async () => {
    section.clear()
    try {
      const invites = await api.get('/api/admin/invites')
      const t = table(['Note', 'Expires', 'Used by', ''])
      for (const invite of invites) {
        const expiring = !invite.used_at && new Date(invite.expires_at) - Date.now() < 86400000
        const actions = actionGroup(invite.used_at ? null : actionButton('trash-2', 'Revoke', 'hover:text-red', async () => {
          const ok = await confirmModal({
            title: 'Revoke this invite',
            message: 'The URL stops working immediately.',
            confirmLabel: 'Revoke',
            destructive: true,
            icon: 'trash-2',
          })
          if (!ok) return
          section.clear()
          try {
            await api.del('/api/admin/invites/' + invite.id)
            await section.load()
          } catch (err) {
            section.fail(err)
          }
        }))
        t.tbody.appendChild(row([
          invite.note || '',
          expiring ? el('span', 'text-yellow', dateLabel(invite.expires_at)) : dateLabel(invite.expires_at),
          invite.used_at ? dateLabel(invite.used_at) : 'outstanding',
          actions,
        ], Boolean(invite.used_at)))
      }
      if (!invites.length) t.tbody.appendChild(row(['No invites yet.', '', '', '']))
      list.replaceChildren(t.wrap)
      drawIcons(list)
    } catch (err) {
      section.fail(err)
    }
  }
  return section
}

function flatten(source, prefix) {
  const out = []
  for (const [key, value] of Object.entries(source || {})) {
    const name = prefix ? prefix + '_' + key : key
    if (value && typeof value === 'object' && !Array.isArray(value)) out.push(...flatten(value, name))
    else out.push([name, value])
  }
  return out
}

function statTile(key, value) {
  const tile = el('div', 'min-w-0 rounded-xl bg-surface0 p-3')
  tile.appendChild(el('dt', 'truncate text-xs text-overlay1', humanKey(key)))
  let shown = value
  let tone = 'text-blue'
  if (key.endsWith('_bytes')) {
    shown = sizeLabel(value)
    tone = 'text-peach'
  } else if (typeof value === 'number') {
    shown = value.toLocaleString()
  } else if (typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T/.test(value)) {
    shown = dateLabel(value)
    tone = 'text-subtext1'
  } else if (typeof value === 'boolean') {
    shown = value ? 'yes' : 'no'
    tone = value ? 'text-green' : 'text-red'
  }
  tile.appendChild(el('dd', 'font-display text-lg ' + tone, String(shown)))
  return tile
}

function buildStats() {
  const section = panel('Stats', 'Counts, storage, and the configured retention policy.')
  const grid = el('dl', 'grid min-w-0 gap-3 sm:grid-cols-2 lg:grid-cols-4')
  const retention = el('div', 'mt-8 min-w-0')
  section.body.append(grid, retention)

  section.load = async () => {
    section.clear()
    try {
      const stats = await api.get('/api/admin/stats')
      grid.replaceChildren()
      for (const [key, value] of flatten(stats)) grid.appendChild(statTile(key, value))
    } catch (err) {
      section.fail(err)
    }
    try {
      const policy = await api.get('/api/admin/retention')
      retention.replaceChildren()
      retention.appendChild(el('h3', 'text-message font-semibold text-text', 'Retention'))
      const rows = el('div', 'mt-3 space-y-2')
      for (const [label, value, unit] of [
        ['Messages', policy.message_days, 'days'],
        ['Recordings', policy.recording_days, 'days'],
        ['Staged uploads', policy.staged_upload_hours, 'hours'],
      ]) {
        const line = el('div', 'flex items-center justify-between gap-4 rounded-xl bg-surface0 px-4 py-3')
        line.appendChild(el('span', 'text-sm text-subtext0', label))
        line.appendChild(el('span', 'text-sm text-text', value === 0 ? 'Kept forever' : value + ' ' + unit))
        rows.appendChild(line)
      }
      const last = el('p', 'mt-3 text-xs text-overlay1', 'Last sweep ' + dateLabel(policy.last_run_at))
      retention.append(rows, last)
    } catch (err) {
      section.fail(err)
    }
  }
  return section
}

function activate(name) {
  if (!builders.has(name)) return
  activeSection = name
  for (const [key, pill] of pills) {
    const active = key === name
    pill.className = 'flex items-center gap-1.5 rounded-full px-3.5 py-1.5 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve '
      + (active ? 'bg-surface0 text-text' : 'text-subtext0 hover:text-text')
    pill.setAttribute('aria-current', active ? 'page' : 'false')
  }
  const section = builders.get(name)
  titleEl.textContent = SECTIONS.find(([key]) => key === name)[1]
  contentEl.replaceChildren(section.wrap)
  section.load()
}

function render() {
  const visible = onAdminRoute()
  rootEl.classList.toggle('hidden', !visible)
  if (!visible) {
    activeSection = null
    return
  }

  const admin = isAdmin()
  gateEl.classList.toggle('hidden', admin)
  shellEl.classList.toggle('hidden', !admin)
  if (!admin) {
    gateEl.textContent = state.me ? 'Administrator access is required for this page.' : 'Sign in to continue.'
    return
  }

  const wanted = sectionFromPath()
  if (wanted !== activeSection) activate(wanted)
}

export function mount(root) {
  rootEl = root
  rootEl.classList.add('hidden')

  const page = el('div', 'flex h-full min-h-0 flex-col')

  const header = el('header', 'flex h-12 shrink-0 items-center gap-3 px-4')
  header.appendChild(iconButton('arrow-left', 'Back to chat', () => navigate('/')))
  titleEl = el('h1', 'min-w-0 shrink-0 font-display text-xl font-semibold text-text', 'Administration')
  header.appendChild(titleEl)

  railEl = el('nav', 'ml-auto flex items-center gap-1 rounded-full bg-base p-1')
  railEl.setAttribute('aria-label', 'Administration')
  for (const [key, label, iconName] of SECTIONS) {
    const pill = el('button')
    pill.type = 'button'
    pill.dataset.section = key
    pill.appendChild(icon(iconName, 'h-4 w-4'))
    pill.appendChild(el('span', 'hidden sm:inline', label))
    pill.addEventListener('click', () => navigate('/admin/' + key))
    pills.set(key, pill)
    railEl.appendChild(pill)
  }
  header.appendChild(railEl)
  header.appendChild(iconButton('refresh-cw', 'Refresh', () => {
    if (activeSection) builders.get(activeSection).load()
  }, 'hover:text-green'))
  page.appendChild(header)

  gateEl = emptyState('Administrator access is required for this page.')
  gateEl.classList.add('hidden')
  page.appendChild(gateEl)

  shellEl = el('div', 'hidden min-h-0 flex-1 overflow-y-auto bg-mantle')
  contentEl = el('div', 'min-w-0 px-4 py-6 sm:px-6')
  shellEl.appendChild(contentEl)
  page.appendChild(shellEl)

  rootEl.replaceChildren(page)
  drawIcons(page)

  builders.set('identities', buildIdentities())
  builders.set('channels', buildChannels())
  builders.set('invites', buildInvites())
  builders.set('stats', buildStats())

  window.addEventListener('popstate', render)
  window.addEventListener('isane:navigate', render)
  window.addEventListener('isane:admin-route', render)

  subscribe(scheduleRender)
  render()
}

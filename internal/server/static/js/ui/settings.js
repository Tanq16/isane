import * as api from '../api.js'
import { state, notify, user, upsertContainer } from '../store.js'
import { currentEndpoint, enablePush, pushSupported } from '../push.js'
import { muted, setMuted } from '../sound.js'
import {
  avatarNode, containerPath, dateLabel, drawIcons, el, field, icon,
  navigate, presenceDot, textButton,
} from './dom.js'
import { confirmModal, openModal } from './modal.js'
import { toast } from './toast.js'

const LEVELS = [
  ['all', 'All messages', 'Every message raises a notification.', 'bell'],
  ['mentions', 'Mentions only', 'Only a message that names you.', 'at-sign'],
  ['none', 'Nothing', 'No notifications at all.', 'bell-off'],
]

const MIN_PASSWORD = 8

export function mayManageChannels() {
  if (!state.me) return false
  return Boolean(state.me.is_admin || state.settings.allow_member_channels)
}

export function slugify(value) {
  return String(value || '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 64)
}

function sectionLabel(text) {
  return el('p', 'mb-2 mt-4 text-micro font-bold uppercase tracking-widest text-lavender first:mt-0', text)
}

function readOnlyRow(label, value) {
  const row = el('div', 'flex items-baseline justify-between gap-4 py-1 text-sm')
  row.appendChild(el('span', 'shrink-0 text-subtext0', label))
  row.appendChild(el('span', 'min-w-0 truncate text-text', value))
  return row
}

function notificationSection(body, c) {
  body.appendChild(sectionLabel('Notifications'))
  const group = el('div', 'space-y-1')
  group.setAttribute('role', 'radiogroup')
  group.setAttribute('aria-label', 'Notification level')
  const buttons = new Map()

  const paint = () => {
    for (const [level, button] of buttons) {
      const active = (c.level || 'mentions') === level
      button.setAttribute('aria-checked', active ? 'true' : 'false')
      button.className = 'flex w-full items-start gap-3 rounded-lg px-2 py-2 text-left transition-colors hover:bg-surface0 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve' + (active ? ' bg-surface0' : '')
      const mark = button.querySelector('[data-mark]')
      mark.classList.toggle('hidden', !active)
    }
  }

  for (const [level, title, description, iconName] of LEVELS) {
    const button = el('button')
    button.type = 'button'
    button.setAttribute('role', 'radio')
    button.appendChild(icon(iconName, 'mt-0.5 h-4 w-4 shrink-0 text-overlay1'))
    const copy = el('span', 'min-w-0 flex-1')
    copy.appendChild(el('span', 'block text-sm text-text', title))
    copy.appendChild(el('span', 'block text-xs text-overlay1', description))
    button.appendChild(copy)
    const mark = icon('check', 'hidden h-4 w-4 shrink-0 text-mauve')
    mark.dataset.mark = '1'
    button.appendChild(mark)
    button.addEventListener('click', async () => {
      const previous = c.level || 'mentions'
      if (previous === level) return
      c.level = level
      paint()
      notify('containers')
      try {
        await api.put('/api/containers/' + c.id + '/notification-pref', { level })
      } catch (err) {
        c.level = previous
        paint()
        notify('containers')
        toast(err.message || 'Could not change the notification level.', { severity: 'error' })
      }
    })
    buttons.set(level, button)
    group.appendChild(button)
  }
  body.appendChild(group)
  paint()
  drawIcons(group)
}

function archiveSection(body, c, handle) {
  body.appendChild(sectionLabel(c.archived_at ? 'Archived' : 'Archive'))
  body.appendChild(el('p', 'mb-2 text-sm text-overlay1', c.archived_at
    ? 'History stays readable and new messages are blocked.'
    : 'Archiving keeps the history and blocks new messages.'))
  const action = textButton(c.archived_at ? 'Unarchive' : 'Archive', async () => {
    if (action.dataset.armed !== '1') {
      action.dataset.armed = '1'
      action.textContent = c.archived_at ? 'Confirm unarchive' : 'Confirm archive'
      action.className = 'h-9 rounded-lg bg-red px-4 text-sm font-semibold text-crust transition-colors hover:brightness-110 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve'
      return
    }
    action.disabled = true
    handle.clearError()
    try {
      if (c.archived_at) {
        await api.post('/api/admin/channels/' + c.id + '/unarchive', {})
        c.archived_at = null
      } else {
        await api.del('/api/admin/channels/' + c.id)
        c.archived_at = new Date().toISOString()
      }
      notify('containers')
      handle.close()
    } catch (err) {
      handle.fail(err.message || 'Could not change the archive state.')
      action.disabled = false
      action.dataset.armed = ''
      action.textContent = c.archived_at ? 'Unarchive' : 'Archive'
    }
  }, 'ghost')
  action.classList.add('text-red')
  body.appendChild(action)
}

function participantsSection(body, c) {
  const list = el('div', 'space-y-0.5')
  for (const id of c.participants || []) {
    const u = user(id)
    const row = el('div', 'flex h-11 items-center gap-3 rounded-md px-2')
    const stack = el('span', 'relative shrink-0')
    stack.appendChild(avatarNode(u, 'h-7 w-7', 'text-xs'))
    const dot = presenceDot(u.id, 'ring-mantle')
    dot.classList.add('absolute', '-bottom-0.5', '-right-0.5')
    stack.appendChild(dot)
    row.appendChild(stack)
    row.appendChild(el('span', 'min-w-0 truncate text-sm text-text', u.display_name))
    row.appendChild(el('span', 'min-w-0 truncate text-xs text-overlay1', '@' + u.handle))
    list.appendChild(row)
  }
  body.appendChild(list)
}

export function openChannelSettings(c) {
  if (!c) return
  if (c.kind !== 'channel') {
    openModal({
      title: 'Members',
      icon: 'users-round',
      build(body) {
        participantsSection(body, c)
        notificationSection(body, c)
      },
    })
    return
  }

  const editable = mayManageChannels()
  let name = null
  let topic = null

  const save = textButton('Save', async () => {
    save.disabled = true
    handle.clearError()
    try {
      const updated = await api.patch('/api/channels/' + c.id, {
        name: name.input.value.trim(),
        topic: topic.input.value.trim(),
      })
      upsertContainer(updated)
      notify('containers')
      handle.close()
    } catch (err) {
      handle.fail(err.message || 'Could not save the channel.')
      save.disabled = false
    }
  }, 'primary')
  save.disabled = true

  const handle = openModal({
    title: c.name || c.slug,
    icon: 'settings',
    width: 'max-w-lg',
    build(body, api2) {
      body.appendChild(sectionLabel('About'))
      if (editable) {
        name = field('Name', { value: c.name || '', name: 'channel-name' })
        topic = field('Topic', { value: c.topic || '', name: 'channel-topic', placeholder: 'What this channel is for' })
        const grid = el('div', 'space-y-3')
        grid.appendChild(name.wrap)
        grid.appendChild(topic.wrap)
        body.appendChild(grid)

        const sync = () => {
          const changed = name.input.value.trim() !== (c.name || '') || topic.input.value.trim() !== (c.topic || '')
          save.disabled = !changed || !name.input.value.trim()
        }
        name.input.addEventListener('input', sync)
        topic.input.addEventListener('input', sync)
      } else {
        body.appendChild(readOnlyRow('Name', c.name || c.slug))
        body.appendChild(readOnlyRow('Topic', c.topic || 'No topic'))
      }
      body.appendChild(readOnlyRow('Address', '#' + c.slug))
      body.appendChild(readOnlyRow('Created by', user(c.created_by).display_name))
      body.appendChild(readOnlyRow('Created', dateLabel(c.created_at)))

      notificationSection(body, c)
      if (state.me && state.me.is_admin) archiveSection(body, c, api2)
    },
    actions: editable ? [save] : [],
  })
}

export function openCreateChannel() {
  let name = null
  let slug = null
  let topic = null
  let slugTouched = false

  const create = textButton('Create channel', async () => {
    create.disabled = true
    handle.clearError()
    try {
      const created = await api.post('/api/channels', {
        slug: slug.input.value.trim(),
        name: name.input.value.trim(),
        topic: topic.input.value.trim(),
      })
      const view = upsertContainer(created)
      notify('containers')
      handle.close()
      state.current.containerId = view.id
      navigate(containerPath(view))
      notify('current')
    } catch (err) {
      handle.fail(err.message || 'Could not create the channel.')
      create.disabled = false
    }
  }, 'primary')

  const handle = openModal({
    title: 'Create a channel',
    icon: 'plus',
    build(body) {
      const grid = el('div', 'space-y-3')
      name = field('Name', { name: 'new-channel-name', placeholder: 'Release planning' })
      slug = field('Address', { name: 'new-channel-slug', placeholder: 'release-planning' })
      topic = field('Topic', { name: 'new-channel-topic', placeholder: 'Optional' })
      grid.appendChild(name.wrap)
      grid.appendChild(slug.wrap)
      grid.appendChild(topic.wrap)
      body.appendChild(grid)
      body.appendChild(el('p', 'mt-2 text-xs text-overlay1', 'The address is lowercase letters, numbers, and hyphens. Everyone can read every channel.'))

      const sync = () => {
        create.disabled = !name.input.value.trim() || !slug.input.value.trim()
      }
      name.input.addEventListener('input', () => {
        if (!slugTouched) slug.input.value = slugify(name.input.value)
        sync()
      })
      slug.input.addEventListener('input', () => {
        slugTouched = true
        slug.input.value = slug.input.value.toLowerCase()
        sync()
      })
      create.disabled = true
    },
    actions: [create],
  })
  return handle
}

function profileSection(body, handle) {
  const me = state.me
  body.appendChild(sectionLabel('Profile'))

  const row = el('div', 'mb-3 flex items-center gap-4')
  const preview = el('div', 'shrink-0')
  preview.appendChild(avatarNode(me, 'h-14 w-14', 'text-lg'))
  row.appendChild(preview)

  const picker = el('input', 'hidden')
  picker.type = 'file'
  picker.accept = 'image/*'
  const upload = textButton('Change picture', () => picker.click(), 'secondary')
  const status = el('p', 'text-xs text-overlay1')
  const column = el('div', 'min-w-0 flex-1')
  column.appendChild(upload)
  column.appendChild(status)
  row.appendChild(column)
  row.appendChild(picker)
  body.appendChild(row)

  const name = field('Display name', { value: me.display_name || '', name: 'me-display-name' })
  body.appendChild(name.wrap)
  body.appendChild(el('div', 'mt-2'))
  body.appendChild(readOnlyRow('Handle', '@' + me.handle))
  body.appendChild(readOnlyRow('Email', me.email || 'Not set'))

  let avatarId = me.avatar_id || null

  const save = textButton('Save profile', async () => {
    save.disabled = true
    handle.clearError()
    try {
      const updated = await api.patch('/api/auth/me', {
        display_name: name.input.value.trim(),
        avatar_id: avatarId,
      })
      state.me = updated
      state.users.set(updated.id, updated)
      notify('me', 'users')
      toast('Profile saved', { severity: 'success' })
    } catch (err) {
      handle.fail(err.message || 'Could not save the profile.')
    }
    save.disabled = false
  }, 'primary')

  picker.addEventListener('change', async () => {
    const file = picker.files && picker.files[0]
    picker.value = ''
    if (!file) return
    status.textContent = 'Uploading'
    status.className = 'text-xs text-yellow'
    try {
      const attachment = await api.upload(file)
      avatarId = attachment.id
      status.textContent = 'Ready. Save to apply.'
      status.className = 'text-xs text-green'
    } catch (err) {
      status.textContent = err.message || 'Upload failed'
      status.className = 'text-xs text-red'
    }
  })

  body.appendChild(el('div', 'mt-3'))
  body.appendChild(save)
}

function passwordSection(body, handle) {
  body.appendChild(sectionLabel('Password'))
  const current = field('Current password', { type: 'password', name: 'current-password', autocomplete: 'current-password' })
  const next = field('New password', { type: 'password', name: 'new-password', autocomplete: 'new-password' })
  const again = field('Confirm new password', { type: 'password', name: 'confirm-password', autocomplete: 'new-password' })
  const grid = el('div', 'space-y-3')
  grid.appendChild(current.wrap)
  grid.appendChild(next.wrap)
  grid.appendChild(again.wrap)
  body.appendChild(grid)
  const hint = el('p', 'mt-2 text-xs text-overlay1', 'At least ' + MIN_PASSWORD + ' characters.')
  body.appendChild(hint)

  const change = textButton('Change password', async () => {
    if (next.input.value.length < MIN_PASSWORD) {
      handle.fail('The new password must be at least ' + MIN_PASSWORD + ' characters.')
      return
    }
    if (next.input.value !== again.input.value) {
      handle.fail('The two new passwords do not match.')
      return
    }
    change.disabled = true
    handle.clearError()
    try {
      await api.post('/api/auth/password', {
        current_password: current.input.value,
        new_password: next.input.value,
      })
      current.input.value = ''
      next.input.value = ''
      again.input.value = ''
      toast('Password changed', { severity: 'success' })
    } catch (err) {
      handle.fail(err.message || 'Could not change the password.')
    }
    change.disabled = false
  }, 'secondary')
  body.appendChild(el('div', 'mt-3'))
  body.appendChild(change)
}

function toggleRow(body, label, isOn, onChange) {
  const row = el('div', 'flex items-center justify-between gap-4 rounded-lg bg-surface0 px-3 py-2')
  row.appendChild(el('span', 'text-sm text-text', label))
  const toggle = el('button', 'h-6 w-11 shrink-0 rounded-full transition-colors')
  toggle.type = 'button'
  toggle.setAttribute('role', 'switch')
  toggle.setAttribute('aria-label', label)
  const knob = el('span', 'block h-5 w-5 rounded-full bg-crust transition-transform')
  toggle.appendChild(knob)
  const paint = () => {
    const on = isOn()
    toggle.setAttribute('aria-checked', on ? 'true' : 'false')
    toggle.className = 'h-6 w-11 shrink-0 rounded-full transition-colors ' + (on ? 'bg-green' : 'bg-surface2')
    knob.className = 'block h-5 w-5 rounded-full bg-crust transition-transform ' + (on ? 'translate-x-5' : 'translate-x-0.5')
  }
  toggle.addEventListener('click', async () => {
    toggle.disabled = true
    await onChange(!isOn())
    toggle.disabled = false
    paint()
  })
  paint()
  row.appendChild(toggle)
  body.appendChild(row)
}

function soundRow(body) {
  toggleRow(body, 'Play a sound for a new message', () => !muted(), (next) => setMuted(!next))
}

function readingSection(body) {
  body.appendChild(sectionLabel('Reading'))
  toggleRow(body, 'Mark as read on open', () => !state.me || state.me.mark_read_on_open !== false, async (next) => {
    try {
      const updated = await api.patch('/api/auth/me', { mark_read_on_open: next })
      state.me = updated
      state.users.set(updated.id, updated)
      notify('me', 'users')
    } catch (err) {
      toast(err.message || 'Could not change how messages are marked read.', { severity: 'error' })
    }
  })

  const help = el('p', 'mt-2 hidden text-xs text-overlay1',
    'Turn this off and you have to click the New badge to mark messages as read.')
  const explain = el('button', 'mt-2 text-xs text-overlay1 transition-colors hover:text-text', '?')
  explain.type = 'button'
  explain.title = 'What this does'
  explain.setAttribute('aria-label', explain.title)
  explain.setAttribute('aria-expanded', 'false')
  explain.addEventListener('click', () => {
    const shown = !help.classList.toggle('hidden')
    explain.setAttribute('aria-expanded', shown ? 'true' : 'false')
  })
  body.appendChild(explain)
  body.appendChild(help)
}

function pushSection(body) {
  body.appendChild(sectionLabel('Notifications'))
  soundRow(body)
  body.appendChild(el('p', 'my-2 text-xs text-overlay1', 'Android decides how loudly a website may notify. To make Isane interrupt you, raise its importance in Android Settings under Chrome, Notifications, Sites.'))
  if (!pushSupported()) {
    body.appendChild(el('p', 'text-sm text-subtext0', 'Push notifications are not configured on this server.'))
    return
  }
  if (typeof Notification !== 'undefined' && Notification.permission === 'denied') {
    body.appendChild(el('p', 'text-sm text-subtext0', 'Your browser is blocking notifications for this site.'))
    return
  }

  const endpoint = currentEndpoint()
  if (!endpoint) {
    const enable = textButton('Enable notifications', async () => {
      enable.disabled = true
      const ok = await enablePush()
      toast(ok ? 'Notifications are on' : 'Notifications were not enabled', { severity: ok ? 'success' : 'warning' })
      enable.disabled = false
    }, 'primary')
    body.appendChild(enable)
    return
  }

  const row = el('div', 'flex items-center justify-between gap-4 rounded-lg bg-surface0 px-3 py-2')
  row.appendChild(el('span', 'text-sm text-text', 'Notify this device'))
  const toggle = el('button', 'h-6 w-11 shrink-0 rounded-full bg-green transition-colors')
  toggle.type = 'button'
  toggle.setAttribute('role', 'switch')
  toggle.setAttribute('aria-checked', 'true')
  toggle.setAttribute('aria-label', 'Notify this device')
  const knob = el('span', 'block h-5 w-5 translate-x-5 rounded-full bg-crust transition-transform')
  toggle.appendChild(knob)
  toggle.addEventListener('click', async () => {
    const enabled = toggle.getAttribute('aria-checked') !== 'true'
    try {
      await api.put('/api/push/subscribe/enabled', { endpoint, enabled })
      toggle.setAttribute('aria-checked', enabled ? 'true' : 'false')
      toggle.className = 'h-6 w-11 shrink-0 rounded-full transition-colors ' + (enabled ? 'bg-green' : 'bg-surface2')
      knob.className = 'block h-5 w-5 rounded-full bg-crust transition-transform ' + (enabled ? 'translate-x-5' : 'translate-x-0.5')
    } catch (err) {
      toast(err.message || 'Could not change notifications.', { severity: 'error' })
    }
  })
  row.appendChild(toggle)
  body.appendChild(row)

  const forget = textButton('Forget this device', async () => {
    try {
      await api.del('/api/push/subscribe', { endpoint })
      toast('This device will no longer be notified', { severity: 'success' })
    } catch (err) {
      toast(err.message || 'Could not forget this device.', { severity: 'error' })
    }
  }, 'ghost')
  body.appendChild(el('div', 'mt-2'))
  body.appendChild(forget)
}

export function openUserSettings(onSignOut) {
  if (!state.me) return
  openModal({
    title: 'Your account',
    icon: 'settings',
    width: 'max-w-lg',
    build(body, handle) {
      profileSection(body, handle)
      passwordSection(body, handle)
      pushSection(body)
      readingSection(body)
      body.appendChild(sectionLabel('Session'))
      body.appendChild(textButton('Sign out', async () => {
        const ok = await confirmModal({
          title: 'Sign out',
          message: 'Sign out of Isane on this device?',
          confirmLabel: 'Sign out',
          icon: 'log-out',
        })
        if (ok) onSignOut()
      }, 'ghost'))
    },
  })
}

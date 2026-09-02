import { get, post, onUnauthorized, ApiError } from './api.js'
import { state, subscribe, notify, findMessage, loadThread } from './store.js'
import { connect, stop as stopSocket, on as onSocket } from './socket.js'
import { initPush, enablePush, pushSupported } from './push.js'
import * as badge from './badge.js'
import * as sidebar from './ui/sidebar.js'
import * as timeline from './ui/timeline.js'
import * as composer from './ui/composer.js'
import * as thread from './ui/thread.js'
import * as call from './ui/call.js'
import * as admin from './ui/admin.js'
import { el, field, textButton } from './ui/dom.js'
import { closeModal, isModalOpen } from './ui/modal.js'
import { openPicker } from './ui/picker.js'
import { wireResize } from './ui/resize.js'
import { toast } from './ui/toast.js'

const LAST_CONTAINER_KEY = 'isane:last-container'
const INSTALL_HINT_KEY = 'isane:install-hint-seen'
const PUSH_BANNER_KEY = 'isane:push-banner-seen'
const SIDEBAR_WIDTH_KEY = 'isane-sidebar-width'
const THREAD_WIDTH_KEY = 'isane-thread-width'
const CALL_WIDTH_KEY = 'isane-call-width'
const PUSH_INIT_TIMEOUT = 3000
const SIDEBAR_MIN = 200
const SIDEBAR_MAX = 400
const SIDEBAR_FALLBACK = 240
const PANE_MIN = 280
const PANE_MAX = 560
const PANE_FALLBACK = 380
const HANDLE_WIDTH = 4
const COLUMN_MIN = 400

let awaitingContainers = false
let offlineToast = null
let started = false
let threadPane = null
let callPane = null
let fitFrame = 0

function byId(id) {
  return document.getElementById(id)
}

function show(node, visible) {
  if (node) node.classList.toggle('hidden', !visible)
}

function readLocal(key) {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function writeLocal(key, value) {
  try {
    localStorage.setItem(key, value)
  } catch {}
}

function navigate(path, replace = false) {
  if (replace) history.replaceState(null, '', path)
  else history.pushState(null, '', path)
  route()
}

function wireNavigation() {
  window.addEventListener('popstate', route)
  window.addEventListener('isane:navigate', route)
  window.addEventListener('isane:signed-out', () => {
    state.me = null
    stopSocket()
    navigate('/login', true)
  })
  document.addEventListener('click', (e) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
    const anchor = e.target instanceof Element ? e.target.closest('a[href]') : null
    if (!anchor || anchor.target === '_blank' || anchor.hasAttribute('download')) return
    const href = anchor.getAttribute('href')
    if (!href || !href.startsWith('/') || href.startsWith('//')) return
    e.preventDefault()
    navigate(href)
  })
  document.addEventListener('keydown', (e) => {
    if (e.key === 'k' && (e.metaKey || e.ctrlKey)) {
      if (!state.me) return
      e.preventDefault()
      openPicker()
      return
    }
    if (e.key === 'Escape' && !isModalOpen() && state.current.threadRootId) {
      state.current.threadRootId = null
      notify('current')
    }
  })
}

function paneCap() {
  const panel = call.panelState()
  const open = (state.current.threadRootId ? 1 : 0) + (!panel.hidden && !panel.expanded ? 1 : 0)
  const sidebar = parseInt(getComputedStyle(document.documentElement).getPropertyValue('--sidebar-width'), 10) || 0
  const budget = window.innerWidth - sidebar - (open + 1) * HANDLE_WIDTH - COLUMN_MIN
  return Math.max(PANE_MIN, Math.min(PANE_MAX, budget / Math.max(open, 1)))
}

function fitPanes() {
  if (threadPane) threadPane.apply(threadPane.read(), false)
  if (callPane) callPane.apply(callPane.read(), false)
}

function scheduleFit() {
  if (fitFrame) return
  fitFrame = requestAnimationFrame(() => {
    fitFrame = 0
    fitPanes()
  })
}

function wirePanes() {
  const sidebarPane = wireResize({
    handle: byId('sidebar-resize'),
    pane: byId('sidebar'),
    edge: 'left',
    prop: '--sidebar-width',
    key: SIDEBAR_WIDTH_KEY,
    min: SIDEBAR_MIN,
    max: SIDEBAR_MAX,
    fallback: SIDEBAR_FALLBACK,
    onChange: scheduleFit,
  })
  timeline.setSidebarWidth(sidebarPane.read)

  threadPane = wireResize({
    handle: byId('thread-resize'),
    pane: byId('thread-pane'),
    edge: 'right',
    prop: '--thread-width',
    key: THREAD_WIDTH_KEY,
    min: PANE_MIN,
    max: paneCap,
    fallback: PANE_FALLBACK,
  })

  callPane = wireResize({
    handle: byId('call-resize'),
    pane: byId('call-pane'),
    edge: 'right',
    prop: '--call-width',
    key: CALL_WIDTH_KEY,
    min: PANE_MIN,
    max: paneCap,
    fallback: PANE_FALLBACK,
  })

  window.addEventListener('resize', scheduleFit)
  fitPanes()
}

function mountAll() {
  const panes = [
    [sidebar, 'sidebar'],
    [timeline, 'timeline'],
    [composer, 'composer'],
    [thread, 'thread-pane'],
    [call, 'call-pane'],
    [admin, 'admin-root'],
  ]
  for (const [module, id] of panes) {
    const root = byId(id)
    if (!root) {
      console.error('missing mount point', id)
      continue
    }
    module.mount(root)
  }
}

async function loadUsers() {
  try {
    const users = await get('/users')
    for (const u of Array.isArray(users) ? users : []) state.users.set(u.id, u)
    notify('users')
  } catch (err) {
    console.error('user list failed', err)
  }
}

function setCurrent(containerId, threadRootId) {
  state.current.containerId = containerId ?? null
  state.current.threadRootId = threadRootId ?? null
  if (containerId) writeLocal(LAST_CONTAINER_KEY, containerId)
  notify('current')
}

function containerBySlug(slug) {
  for (const container of state.containers.values()) {
    if (container.slug === slug) return container
  }
  return null
}

function firstChannel() {
  for (const container of state.containers.values()) {
    if (container.kind === 'channel' && !container.archived_at) return container
  }
  return state.containers.values().next().value ?? null
}

function pathOf(container) {
  if (!container) return '/'
  if (container.kind === 'channel' && container.slug) return `/c/${container.slug}`
  return `/d/${container.id}`
}

function goHome() {
  if (!state.containers.size) {
    awaitingContainers = true
    setCurrent(null, null)
    return
  }
  const last = readLocal(LAST_CONTAINER_KEY)
  const target = (last ? state.containers.get(last) : null) ?? firstChannel()
  navigate(pathOf(target), true)
}

function openContainer(containerId) {
  if (!state.containers.has(containerId)) {
    if (!state.containers.size) awaitingContainers = true
    else navigate('/', true)
    return
  }
  setCurrent(containerId, null)
}

async function openThreadRoute(rootId) {
  const root = findMessage(rootId)
  if (root) {
    setCurrent(root.container_id, rootId)
    return
  }
  try {
    const page = await loadThread(rootId)
    const owner = page.root || page.messages[0]
    setCurrent(owner ? owner.container_id : state.current.containerId, rootId)
  } catch (err) {
    console.error('thread load failed', rootId, err)
    toast('Could not open that thread', { severity: 'error' })
  }
}

function route() {
  const path = decodeURIComponent(location.pathname)
  if (path.startsWith('/invite/')) {
    showInvite(path.slice('/invite/'.length))
    return
  }
  if (!state.me || path === '/login') {
    showLogin()
    return
  }
  closeModal()
  show(byId('login-root'), false)
  awaitingContainers = false
  if (path === '/admin' || path.startsWith('/admin/')) {
    if (!state.me.is_admin) {
      navigate('/', true)
      return
    }
    show(byId('app'), false)
    show(byId('admin-root'), true)
    window.dispatchEvent(new CustomEvent('isane:admin-route'))
    return
  }
  show(byId('admin-root'), false)
  show(byId('app'), true)
  if (path === '/' || path === '') {
    goHome()
    return
  }
  if (path.startsWith('/c/')) {
    const container = containerBySlug(path.slice(3))
    if (!container) {
      if (!state.containers.size) awaitingContainers = true
      else navigate('/', true)
      return
    }
    setCurrent(container.id, null)
    return
  }
  if (path.startsWith('/d/')) {
    openContainer(path.slice(3))
    return
  }
  if (path.startsWith('/t/')) {
    openThreadRoute(path.slice(3))
    return
  }
  navigate('/', true)
}

function onStateChange(keys) {
  if (keys.includes('containers') && awaitingContainers) {
    awaitingContainers = false
    route()
  }
  if (keys.includes('current') || keys.includes('calls')) fitPanes()
  if (!keys.includes('connection')) return
  if (state.connection === 'down' && !offlineToast) {
    offlineToast = toast('Reconnecting', { sticky: true, severity: 'warning' })
    return
  }
  if (state.connection === 'live' && offlineToast) {
    offlineToast()
    offlineToast = null
  }
}

function authForm(root, title, subtitle, fields, submitLabel, onSubmit) {
  const card = document.createElement('form')
  card.className = 'flex w-full max-w-sm flex-col gap-4 rounded-2xl bg-mantle p-6 shadow-pop ring-1 ring-edge'
  card.appendChild(el('span', 'font-display text-lg font-bold text-text', 'Isane'))
  card.appendChild(el('h1', 'text-message font-semibold text-text', title))
  if (subtitle) card.appendChild(el('p', '-mt-3 text-sm text-overlay1', subtitle))

  const inputs = new Map()
  for (const spec of fields) {
    const built = field(spec.label, {
      type: spec.type,
      name: spec.name,
      autocomplete: spec.autocomplete,
      placeholder: spec.placeholder,
    })
    built.input.required = true
    card.appendChild(built.wrap)
    inputs.set(spec.name, built.input)
  }

  const error = el('p', 'min-h-5 text-sm text-red')
  error.setAttribute('role', 'alert')
  const button = textButton(submitLabel, null, 'primary')
  button.type = 'submit'
  card.append(error, button)

  card.addEventListener('submit', async (e) => {
    e.preventDefault()
    button.disabled = true
    error.textContent = ''
    const values = {}
    for (const [name, input] of inputs) values[name] = input.value.trim()
    try {
      const me = await onSubmit(values)
      state.me = me
      state.users.set(me.id, me)
      root.dataset.screen = ''
      show(root, false)
      await start()
      navigate('/', true)
    } catch (err) {
      error.textContent = err instanceof ApiError ? err.message : 'Something went wrong'
      button.disabled = false
    }
  })

  root.replaceChildren(card)
  root.classList.remove('hidden')
  root.classList.add('flex')
}

function showScreen(name) {
  const root = byId('login-root')
  if (!root) return null
  show(byId('app'), false)
  show(byId('admin-root'), false)
  if (root.dataset.screen === name) {
    root.classList.remove('hidden')
    root.classList.add('flex')
    return null
  }
  root.dataset.screen = name
  return root
}

function showLogin() {
  const root = showScreen('login')
  if (!root) return
  authForm(root, 'Sign in', null, [
    { name: 'identity', label: 'Handle or email', autocomplete: 'username', placeholder: 'tanq' },
    { name: 'password', label: 'Password', type: 'password', autocomplete: 'current-password' },
  ], 'Sign in', (values) => {
    const identity = values.identity.includes('@') ? { email: values.identity } : { handle: values.identity }
    return post('/auth/login', Object.assign(identity, { password: values.password }))
  })
}

function showInvite(token) {
  const root = showScreen('invite')
  if (!root) return
  authForm(root, 'Create your account', 'This invite works once.', [
    { name: 'handle', label: 'Handle', autocomplete: 'username', placeholder: 'lowercase, no spaces' },
    { name: 'display_name', label: 'Display name', autocomplete: 'name' },
    { name: 'password', label: 'Password', type: 'password', autocomplete: 'new-password' },
  ], 'Create account', (values) => post('/auth/accept-invite', {
    token,
    handle: values.handle,
    display_name: values.display_name,
    password: values.password,
  }))
}

function wirePushBanner() {
  const banner = byId('push-banner')
  if (!banner) return
  const blocked = readLocal(PUSH_BANNER_KEY) === '1'
    || !pushSupported()
    || Notification.permission !== 'default'
  if (blocked) {
    banner.classList.add('hidden')
    return
  }
  const accept = byId('push-banner-enable')
  const dismiss = byId('push-banner-dismiss')
  if (accept) {
    accept.addEventListener('click', async () => {
      accept.disabled = true
      const enabled = await enablePush()
      banner.classList.add('hidden')
      toast(enabled ? 'Notifications are on' : 'Notifications were not enabled', {
        severity: enabled ? 'success' : 'warning',
      })
    })
  }
  if (dismiss) {
    dismiss.addEventListener('click', () => {
      writeLocal(PUSH_BANNER_KEY, '1')
      banner.classList.add('hidden')
    })
  }
  banner.classList.remove('hidden')
  banner.classList.add('flex')
}

function wireInstallHint() {
  const hint = byId('install-hint')
  if (!hint) return
  const iosSafari = /iP(hone|ad|od)/.test(navigator.userAgent) && !/CriOS|FxiOS|EdgiOS/.test(navigator.userAgent)
  const standalone = window.navigator.standalone === true
    || window.matchMedia('(display-mode: standalone)').matches
  if (!iosSafari || standalone || readLocal(INSTALL_HINT_KEY) === '1') {
    hint.classList.add('hidden')
    return
  }
  const dismiss = byId('install-hint-dismiss')
  if (dismiss) {
    dismiss.addEventListener('click', () => {
      writeLocal(INSTALL_HINT_KEY, '1')
      hint.classList.add('hidden')
    })
  }
  hint.classList.remove('hidden')
}

function errorText(d) {
  if (!d || d.code === 'internal') return 'Something went wrong. Try again.'
  return d.message || 'Something went wrong. Try again.'
}

async function start() {
  if (started) return
  started = true
  mountAll()
  wirePanes()
  badge.mount()
  connect()
  loadUsers()
  Promise.race([
    initPush().catch((err) => console.error('push init failed', err)),
    new Promise((resolve) => setTimeout(resolve, PUSH_INIT_TIMEOUT)),
  ]).then(() => {
    wirePushBanner()
    wireInstallHint()
  })
}

function onSessionLost() {
  if (!state.me) return
  state.me = null
  stopSocket()
  toast('Your session ended. Sign in again.', { severity: 'warning' })
  navigate('/login', true)
}

async function boot() {
  wireNavigation()
  subscribe(onStateChange)
  onUnauthorized(onSessionLost)
  onSocket('error', (d) => toast(errorText(d), { severity: 'error' }))
  let me = null
  try {
    me = await get('/auth/me')
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
  }
  if (!me) {
    route()
    return
  }
  state.me = me
  state.users.set(me.id, me)
  await start()
  route()
}

boot().catch((err) => {
  console.error('boot failed', err)
  toast('Isane could not start. Reload the page.', { sticky: true, severity: 'error' })
})

import { get, post, onUnauthorized, ApiError } from './api.js'
import { state, subscribe, notify, findMessage, loadThread } from './store.js'
import { connect, stop as stopSocket, on as onSocket } from './socket.js'
import { initPush, enablePush, pushSupported } from './push.js'
import * as sidebar from './ui/sidebar.js'
import * as timeline from './ui/timeline.js'
import * as composer from './ui/composer.js'
import * as thread from './ui/thread.js'
import * as call from './ui/call.js'
import * as admin from './ui/admin.js'

const LAST_CONTAINER_KEY = 'isane:last-container'
const INSTALL_HINT_KEY = 'isane:install-hint-seen'
const PUSH_BANNER_KEY = 'isane:push-banner-seen'
const PUSH_INIT_TIMEOUT = 3000

const CARD_CLASS = 'flex w-full max-w-sm flex-col gap-3 rounded-xl border border-surface0 bg-mantle p-6'
const FIELD_CLASS = 'w-full rounded-lg border border-surface0 bg-base px-3 py-2 text-text placeholder:text-overlay0 focus:border-mauve focus:outline-none'
const ACTION_CLASS = 'rounded-lg bg-mauve px-3 py-2 font-medium text-crust disabled:opacity-60'
const TOAST_CLASS = 'pointer-events-auto rounded-lg bg-base px-3 py-2 text-sm shadow-pop'
const TOAST_TONE = { info: 'text-blue', success: 'text-green', error: 'text-red', warning: 'text-yellow' }

let awaitingContainers = false
let offlineToast = null

function el(id) {
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

function toast(message, opts = {}) {
  const root = el('toast-root')
  if (!root) return () => {}
  const node = document.createElement('div')
  node.className = TOAST_CLASS + ' ' + (TOAST_TONE[opts.severity] || TOAST_TONE.info)
  node.setAttribute('role', 'status')
  node.textContent = message
  root.appendChild(node)
  const dismiss = () => node.remove()
  if (!opts.sticky) setTimeout(dismiss, opts.duration ?? 4000)
  return dismiss
}

function navigate(path, replace = false) {
  if (replace) history.replaceState(null, '', path)
  else history.pushState(null, '', path)
  route()
}

function wireNavigation() {
  window.addEventListener('popstate', route)
  window.addEventListener('isane:navigate', route)
  document.addEventListener('click', (e) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
    const anchor = e.target instanceof Element ? e.target.closest('a[href]') : null
    if (!anchor || anchor.target === '_blank' || anchor.hasAttribute('download')) return
    const href = anchor.getAttribute('href')
    if (!href || !href.startsWith('/') || href.startsWith('//')) return
    e.preventDefault()
    navigate(href)
  })
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
    const root = el(id)
    if (!root) {
      console.error('missing mount point', id)
      continue
    }
    if (typeof module.mount !== 'function') {
      console.error('module has no mount', id)
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
  show(el('login-root'), false)
  awaitingContainers = false
  if (path === '/admin' || path.startsWith('/admin/')) {
    if (!state.me.is_admin) {
      navigate('/', true)
      return
    }
    show(el('app'), false)
    show(el('admin-root'), true)
    return
  }
  show(el('admin-root'), false)
  show(el('app'), true)
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
  if (!keys.includes('connection')) return
  if (state.connection === 'down' && !offlineToast) {
    offlineToast = toast('Reconnecting', { sticky: true })
    return
  }
  if (state.connection === 'live' && offlineToast) {
    offlineToast()
    offlineToast = null
  }
}

function form(root, title, fields, submitLabel, onSubmit) {
  const card = document.createElement('form')
  card.className = CARD_CLASS
  const heading = document.createElement('h1')
  heading.className = 'text-lg font-semibold text-text'
  heading.textContent = title
  card.appendChild(heading)
  const inputs = new Map()
  for (const field of fields) {
    const input = document.createElement('input')
    input.type = field.type ?? 'text'
    input.placeholder = field.label
    input.autocomplete = field.autocomplete ?? 'off'
    input.required = true
    input.className = FIELD_CLASS
    card.appendChild(input)
    inputs.set(field.name, input)
  }
  const error = document.createElement('p')
  error.className = 'min-h-5 text-sm text-red'
  const button = document.createElement('button')
  button.type = 'submit'
  button.className = ACTION_CLASS
  button.textContent = submitLabel
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
  show(root, true)
}

function showScreen(name) {
  const root = el('login-root')
  if (!root) return null
  show(el('app'), false)
  show(el('admin-root'), false)
  if (root.dataset.screen === name) {
    show(root, true)
    return null
  }
  root.dataset.screen = name
  return root
}

function showLogin() {
  const root = showScreen('login')
  if (!root) return
  form(root, 'Sign in to Isane', [
    { name: 'identity', label: 'Handle or email', autocomplete: 'username' },
    { name: 'password', label: 'Password', type: 'password', autocomplete: 'current-password' },
  ], 'Sign in', (values) => {
    const identity = values.identity.includes('@') ? { email: values.identity } : { handle: values.identity }
    return post('/auth/login', Object.assign(identity, { password: values.password }))
  })
}

function showInvite(token) {
  const root = showScreen('invite')
  if (!root) return
  form(root, 'Create your account', [
    { name: 'handle', label: 'Handle', autocomplete: 'username' },
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
  const banner = el('push-banner')
  if (!banner) return
  const blocked = readLocal(PUSH_BANNER_KEY) === '1'
    || !pushSupported()
    || Notification.permission !== 'default'
  if (blocked) {
    show(banner, false)
    return
  }
  const accept = el('push-banner-enable')
  const dismiss = el('push-banner-dismiss')
  if (accept) {
    accept.addEventListener('click', async () => {
      accept.disabled = true
      const enabled = await enablePush()
      show(banner, false)
      toast(enabled ? 'Notifications are on' : 'Notifications were not enabled')
    })
  }
  if (dismiss) {
    dismiss.addEventListener('click', () => {
      writeLocal(PUSH_BANNER_KEY, '1')
      show(banner, false)
    })
  }
  show(banner, true)
}

function wireInstallHint() {
  const hint = el('install-hint')
  if (!hint) return
  const iosSafari = /iP(hone|ad|od)/.test(navigator.userAgent) && !/CriOS|FxiOS|EdgiOS/.test(navigator.userAgent)
  const standalone = window.navigator.standalone === true
    || window.matchMedia('(display-mode: standalone)').matches
  if (!iosSafari || standalone || readLocal(INSTALL_HINT_KEY) === '1') {
    show(hint, false)
    return
  }
  const dismiss = el('install-hint-dismiss')
  if (dismiss) {
    dismiss.addEventListener('click', () => {
      writeLocal(INSTALL_HINT_KEY, '1')
      show(hint, false)
    })
  }
  show(hint, true)
}

let started = false

async function start() {
  if (started) return
  started = true
  mountAll()
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

function errorText(d) {
  if (!d || d.code === 'internal') return 'Something went wrong. Try again.'
  return d.message || 'Something went wrong. Try again.'
}

boot().catch((err) => {
  console.error('boot failed', err)
  toast('Isane could not start. Reload the page.', { sticky: true, severity: 'error' })
})

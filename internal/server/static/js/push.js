import { get, post } from './api.js'

const endpointListeners = new Set()

let registration = null
let endpoint = null
let vapidKey = null
let serverEnabled = null

function browserSupports() {
  return typeof navigator !== 'undefined'
    && 'serviceWorker' in navigator
    && typeof window !== 'undefined'
    && 'PushManager' in window
    && 'Notification' in window
    && window.isSecureContext === true
}

export function pushSupported() {
  if (serverEnabled === false) return false
  return browserSupports()
}

export function currentEndpoint() {
  return endpoint
}

export function currentRegistration() {
  return registration
}

export function onEndpoint(fn) {
  endpointListeners.add(fn)
  return () => endpointListeners.delete(fn)
}

function decodeKey(base64) {
  const padded = String(base64).replace(/-/g, '+').replace(/_/g, '/')
  const binary = atob(padded + '='.repeat((4 - (padded.length % 4)) % 4))
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return bytes
}

function keyOf(res) {
  if (typeof res === 'string') return res.trim() || null
  if (!res || typeof res !== 'object') return null
  return res.key || res.vapid_public_key || res.public_key || null
}

async function upsert(subscription) {
  const json = subscription.toJSON()
  endpoint = json.endpoint || subscription.endpoint || null
  if (!endpoint || !json.keys || !json.keys.p256dh || !json.keys.auth) return
  await post('/push/subscribe', {
    endpoint,
    keys: { p256dh: json.keys.p256dh, auth: json.keys.auth },
  })
  for (const fn of Array.from(endpointListeners)) {
    try {
      fn(endpoint)
    } catch (err) {
      console.error('push endpoint listener failed', err)
    }
  }
}

async function ensureSubscription() {
  let subscription = await registration.pushManager.getSubscription()
  if (!subscription) {
    if (Notification.permission !== 'granted') return false
    subscription = await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: decodeKey(vapidKey),
    })
  }
  await upsert(subscription)
  return true
}

export async function initPush() {
  if (!browserSupports()) {
    serverEnabled = false
    return false
  }
  try {
    await navigator.serviceWorker.register('/sw.js', { scope: '/' })
    registration = await navigator.serviceWorker.ready
  } catch (err) {
    console.error('service worker registration failed', err)
    serverEnabled = false
    return false
  }
  try {
    vapidKey = keyOf(await get('/push/vapid-key'))
  } catch {
    vapidKey = null
  }
  if (!vapidKey) {
    serverEnabled = false
    return false
  }
  serverEnabled = true
  try {
    await ensureSubscription()
  } catch (err) {
    console.error('push subscription refresh failed', err)
  }
  return true
}

export async function enablePush() {
  if (!registration && !(await initPush())) return false
  if (!pushSupported()) return false
  let permission = Notification.permission
  if (permission === 'default') permission = await Notification.requestPermission()
  if (permission !== 'granted') return false
  try {
    return await ensureSubscription()
  } catch (err) {
    console.error('push subscribe failed', err)
    return false
  }
}

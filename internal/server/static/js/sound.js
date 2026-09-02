const SOUND_URL = '/static/vendor/notification.mp3'
const MUTED_KEY = 'isane:sound-muted'

let element = null

export function muted() {
  try {
    return localStorage.getItem(MUTED_KEY) === '1'
  } catch {
    return false
  }
}

export function setMuted(value) {
  try {
    if (value) localStorage.setItem(MUTED_KEY, '1')
    else localStorage.removeItem(MUTED_KEY)
  } catch {}
}

export function play() {
  if (muted()) return
  if (!element) {
    element = new Audio(SOUND_URL)
    element.preload = 'auto'
  }
  element.currentTime = 0
  const started = element.play()
  if (started && typeof started.catch === 'function') started.catch(() => {})
}

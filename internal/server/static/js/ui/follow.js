const STICK_SLACK = 16
const GESTURE_LINGER = 400
const GESTURES = ['wheel', 'touchstart', 'touchmove', 'pointerdown', 'keydown']

export function followBottom(scrollEl, options = {}) {
  let following = false
  let gesturing = false
  let gestureTimer = 0

  function bottom() {
    return scrollEl.scrollHeight - scrollEl.clientHeight
  }

  function pin() {
    if (!following || gesturing) return
    const target = options.target ? options.target(bottom()) : bottom()
    if (Math.abs(scrollEl.scrollTop - target) > 1) scrollEl.scrollTop = target
  }

  function hold() {
    if (!gesturing && options.onGesture) options.onGesture()
    gesturing = true
    clearTimeout(gestureTimer)
    gestureTimer = setTimeout(release, GESTURE_LINGER)
  }

  function release() {
    clearTimeout(gestureTimer)
    gestureTimer = 0
    gesturing = false
    pin()
  }

  function refresh() {
    following = bottom() - scrollEl.scrollTop <= STICK_SLACK
  }

  for (const type of GESTURES) scrollEl.addEventListener(type, hold, { passive: true })
  scrollEl.addEventListener('scrollend', release)
  scrollEl.addEventListener('scroll', () => {
    if (!gesturing) return
    refresh()
    hold()
  })

  const resize = new ResizeObserver(pin)
  resize.observe(scrollEl)

  return {
    observe(node) {
      resize.observe(node)
    },
    follow() {
      following = true
      release()
    },
    unfollow() {
      following = false
    },
    refresh,
  }
}

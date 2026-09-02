const KEY = 'isane-theme'
const COLOR = { dark: '#11111b', light: '#dce0e8' }

export function currentTheme() {
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
}

function meta(name) {
  return document.querySelector(`meta[name="${name}"]`)
}

function applyTheme(theme) {
  const dark = theme !== 'light'
  document.documentElement.classList.toggle('dark', dark)
  const color = meta('theme-color')
  if (color) color.content = dark ? COLOR.dark : COLOR.light
  const scheme = meta('color-scheme')
  if (scheme) scheme.content = dark ? 'dark' : 'light'
  window.dispatchEvent(new CustomEvent('isane:theme', { detail: { theme: dark ? 'dark' : 'light' } }))
}

export function toggleTheme() {
  const next = currentTheme() === 'dark' ? 'light' : 'dark'
  applyTheme(next)
  try {
    localStorage.setItem(KEY, next)
  } catch {}
  return next
}

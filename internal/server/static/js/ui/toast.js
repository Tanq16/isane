const SHAPE = 'pointer-events-auto rounded-lg bg-base px-3 py-2 text-sm shadow-pop'
const TONE = { info: 'text-blue', success: 'text-green', error: 'text-red', warning: 'text-yellow' }

export function toast(message, opts = {}) {
  const root = document.getElementById('toast-root')
  if (!root) return () => {}
  const node = document.createElement('div')
  node.className = SHAPE + ' ' + (TONE[opts.severity] || TONE.info)
  node.setAttribute('role', 'status')
  node.textContent = message
  root.appendChild(node)
  const dismiss = () => node.remove()
  if (!opts.sticky) setTimeout(dismiss, opts.duration ?? 4000)
  return dismiss
}

import { drawIcons, el, icon } from './dom.js'

function clockLabel(ms) {
  const total = Math.max(0, Math.round(ms / 1000))
  return Math.floor(total / 60) + ':' + String(total % 60).padStart(2, '0')
}

export function recordingPlayer(rec) {
  const href = '/api/recordings/' + rec.id
  let total = Number(rec.duration_ms) || 0

  const wrap = el('div', 'mt-1 flex w-full max-w-md items-center gap-2 rounded-xl bg-base px-2 py-1.5')

  const toggle = el('button', 'grid h-8 w-8 shrink-0 place-items-center rounded-full bg-mauve text-crust transition-colors hover:brightness-110 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve')
  toggle.type = 'button'

  const seek = el('input', 'accent-mauve h-1 min-w-0 flex-1')
  seek.type = 'range'
  seek.min = '0'
  seek.max = '1000'
  seek.value = '0'
  seek.setAttribute('aria-label', 'Seek in the recording')

  const label = el('span', 'shrink-0 text-micro tabular-nums text-overlay1')

  const download = el('a', 'shrink-0 text-overlay1 transition-colors hover:text-text')
  download.href = href + '?download=1'
  download.title = 'Download the recording'
  download.setAttribute('aria-label', download.title)
  download.appendChild(icon('download', 'h-4 w-4'))

  const audio = el('audio', 'hidden')
  audio.preload = 'metadata'
  audio.src = href

  const paintTime = () => {
    const at = audio.currentTime * 1000
    label.textContent = clockLabel(at) + ' / ' + clockLabel(total)
    seek.value = total ? String(Math.round((at / total) * 1000)) : '0'
  }

  const paintToggle = () => {
    const playing = !audio.paused && !audio.ended
    toggle.title = playing ? 'Pause the recording' : 'Play the recording'
    toggle.setAttribute('aria-label', toggle.title)
    toggle.replaceChildren(icon(playing ? 'pause' : 'play', 'h-4 w-4'))
    drawIcons(toggle)
  }

  toggle.addEventListener('click', () => {
    if (audio.paused) {
      const started = audio.play()
      if (started && typeof started.catch === 'function') started.catch(() => {})
      return
    }
    audio.pause()
  })

  seek.addEventListener('input', () => {
    if (!total) return
    audio.currentTime = (Number(seek.value) / 1000) * (total / 1000)
  })

  audio.addEventListener('loadedmetadata', () => {
    if (Number.isFinite(audio.duration) && audio.duration > 0) total = audio.duration * 1000
    paintTime()
  })
  audio.addEventListener('timeupdate', paintTime)
  audio.addEventListener('play', paintToggle)
  audio.addEventListener('pause', paintToggle)
  audio.addEventListener('ended', paintToggle)

  wrap.append(toggle, seek, label, download, audio)
  paintTime()
  paintToggle()
  drawIcons(wrap)
  return wrap
}

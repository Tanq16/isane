import { drawIcons, el, icon } from './dom.js'

function clockLabel(ms) {
  const total = Math.max(0, Math.round(ms / 1000))
  return Math.floor(total / 60) + ':' + String(total % 60).padStart(2, '0')
}

export function recordingPlayer(rec) {
  const href = '/api/recordings/' + rec.id
  return audioPlayer({ src: href, download: href + '?download=1', durationMs: rec.duration_ms, noun: 'recording', cls: 'mt-1' })
}

export function audioPlayer(spec) {
  let total = Number(spec.durationMs) || 0

  const wrap = el('div', spec.cls + ' flex w-full max-w-md items-center gap-2 rounded-xl bg-base px-2 py-1.5')

  const toggle = el('button', 'grid h-8 w-8 shrink-0 place-items-center rounded-full bg-mauve text-crust transition-colors hover:brightness-110 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve')
  toggle.type = 'button'

  const seek = el('input', 'accent-mauve h-1 min-w-0 flex-1')
  seek.type = 'range'
  seek.min = '0'
  seek.max = '1000'
  seek.value = '0'
  seek.setAttribute('aria-label', 'Seek in the ' + spec.noun)

  const label = el('span', 'shrink-0 text-micro tabular-nums text-overlay1')

  const download = el('a', 'shrink-0 text-overlay1 transition-colors hover:text-text')
  download.href = spec.download
  if (spec.name) download.download = spec.name
  download.title = 'Download the ' + spec.noun
  download.setAttribute('aria-label', download.title)
  download.appendChild(icon('download', 'h-4 w-4'))

  const audio = el('audio', 'hidden')
  audio.preload = 'metadata'
  audio.src = spec.src

  const paintTime = () => {
    const at = audio.currentTime * 1000
    label.textContent = clockLabel(at) + ' / ' + clockLabel(total)
    seek.value = total ? String(Math.round((at / total) * 1000)) : '0'
  }

  const paintToggle = () => {
    const playing = !audio.paused && !audio.ended
    toggle.title = (playing ? 'Pause the ' : 'Play the ') + spec.noun
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

  if (spec.name) {
    const row = el('div', 'flex items-center gap-2')
    row.append(seek, label)
    const titled = el('div', 'flex min-w-0 flex-1 flex-col')
    titled.append(el('p', 'truncate text-xs text-subtext0', spec.name), row)
    wrap.append(toggle, titled, download, audio)
  } else {
    wrap.append(toggle, seek, label, download, audio)
  }
  paintTime()
  paintToggle()
  drawIcons(wrap)
  return wrap
}

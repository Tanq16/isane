import { state, subscribe, notify, user } from '../store.js'
import * as api from '../api.js'
import * as socket from '../socket.js'
import { drawIcons, icon, iconButton } from './dom.js'
import { toast } from './toast.js'

const IDLE_DIM_MS = 240000

const TONES = {
  on: 'bg-surface0 text-text hover:bg-surface1',
  off: 'bg-surface0 text-overlay1 hover:bg-surface1 hover:text-text',
  alert: 'bg-red/15 text-red hover:bg-red/25',
  accent: 'bg-mauve text-crust hover:brightness-110',
  danger: 'bg-red text-crust hover:brightness-110',
  busy: 'bg-surface0 text-overlay0 cursor-not-allowed',
}

const CALL_ERRORS = {
  microphone: 'Could not use the microphone.',
  token: 'Could not get a call token.',
  connect: 'Could not reach the call server.',
  publish: 'Could not publish your microphone.',
}

const MIC_DENIED = 'Isane needs microphone access. Allow it in your device settings, then try again.'

const STRIP_QUERY = '(min-width: 48rem)'
const MAX_STAGE_TILES = 8

const TILE = 'relative overflow-hidden bg-base ring-1 ring-edge'
const STAGE_TILE_FIT = TILE + ' w-full self-center aspect-video max-h-full rounded-xl'
const STAGE_TILE_SCROLL = TILE + ' w-full aspect-video rounded-xl'
const STRIP_TILE = TILE + ' aspect-video h-20 w-auto shrink-0 rounded-lg'
const OVERFLOW_TILE = 'grid place-items-center text-message font-semibold text-subtext0'
const STAGE_FIT = 'grid min-h-0 flex-1 gap-2 overflow-hidden auto-rows-[minmax(0,1fr)] grid-cols-[repeat(auto-fit,minmax(min(22rem,100%),1fr))]'
const STAGE_SCROLL = 'grid min-h-0 flex-1 content-start gap-2 overflow-y-auto grid-cols-[repeat(auto-fit,minmax(min(22rem,100%),1fr))]'
const STRIP = 'flex shrink-0 items-center gap-2 overflow-x-auto empty:hidden'
const CAPTION = 'absolute inset-x-0 bottom-0 flex items-center gap-2 bg-scrim'
const STAGE_CAPTION = CAPTION + ' px-2 py-1 text-xs'
const STRIP_CAPTION = CAPTION + ' px-1.5 py-0.5 text-micro'

const stripMedia = matchMedia(STRIP_QUERY)

function lk() {
  return globalThis.LivekitClient || globalThis.LiveKit || null
}

let rootEl = null
let panelEl = null
let headerEl = null
let stripEl = null
let stageEl = null
let overflowEl = null
let controlsEl = null
let audioSinkEl = null
let audioButtonEl = null
let statusEl = null
let dimEl = null

const tiles = new Map()

let room = null
let activeCall = null
let joined = false
let connecting = false
let micOn = true
let cameraOn = false
let screenOn = false
let wakeLock = null
let idleTimer = 0
let frame = 0
let statusText = ''
let dismissed = false
let expanded = false

function el(tag, cls, text) {
  const node = document.createElement(tag)
  if (cls) node.className = cls
  if (text != null) node.textContent = text
  return node
}

function scheduleRender() {
  if (frame) return
  frame = requestAnimationFrame(() => {
    frame = 0
    render()
  })
}

function liveCall() {
  if (!activeCall) return null
  const live = state.calls.get(activeCall.container_id)
  return live && live.id === activeCall.id ? live : activeCall
}

function recordingState() {
  const call = liveCall()
  return (call && call.recording_state) || 'off'
}

function visible() {
  return (joined || connecting || Boolean(statusText)) && !dismissed
}

export function panelState() {
  const call = liveCall()
  return {
    joined,
    connecting,
    hidden: !visible(),
    expanded,
    callId: call ? call.id : null,
    containerId: call ? call.container_id : null,
    recordingState: recordingState(),
  }
}

function screenShareSupported() {
  return Boolean(navigator.mediaDevices && typeof navigator.mediaDevices.getDisplayMedia === 'function')
}

function livekitUrl(fromServer) {
  if (fromServer) return fromServer
  const scheme = location.protocol === 'https:' ? 'wss://' : 'ws://'
  return scheme + location.host + '/livekit'
}

function audioCaptureOptions() {
  return {
    echoCancellation: true,
    noiseSuppression: true,
    autoGainControl: true,
  }
}

function roomOptions() {
  const LK = lk()
  const q = state.quality || {}
  const video = q.video || {}
  const screen = q.screen_share || {}
  const audio = q.audio || {}
  const height = video.max_resolution || 720
  const framerate = video.max_framerate || 30
  const layers = (video.simulcast_layers || []).map(
    (l) => new LK.VideoPreset(Math.round((l.height * 16) / 9), l.height, l.bitrate, framerate)
  )

  return {
    adaptiveStream: true,
    dynacast: true,
    disconnectOnPageLeave: false,
    audioCaptureDefaults: audioCaptureOptions(),
    videoCaptureDefaults: {
      resolution: { width: Math.round((height * 16) / 9), height, frameRate: framerate },
    },
    publishDefaults: {
      videoCodec: q.codec || 'vp8',
      simulcast: true,
      videoSimulcastLayers: layers,
      videoEncoding: { maxBitrate: video.max_bitrate || 1700000, maxFramerate: framerate },
      screenShareEncoding: { maxBitrate: screen.max_bitrate || 2500000, maxFramerate: screen.max_framerate || 15 },
      audioPreset: { maxBitrate: audio.max_bitrate || 24000 },
      dtx: audio.dtx !== false,
      red: true,
    },
  }
}

function screenShareOptions() {
  const screen = (state.quality && state.quality.screen_share) || {}
  const height = screen.max_resolution || 1080
  return {
    audio: false,
    resolution: { width: Math.round((height * 16) / 9), height, frameRate: screen.max_framerate || 15 },
  }
}

async function acquireWakeLock() {
  if (!navigator.wakeLock) return
  try {
    wakeLock = await navigator.wakeLock.request('screen')
  } catch {
    wakeLock = null
  }
}

async function releaseWakeLock() {
  if (!wakeLock) return
  try {
    await wakeLock.release()
  } catch {}
  wakeLock = null
}

function resetIdle() {
  dimEl.classList.add('hidden')
  clearTimeout(idleTimer)
  if (!joined) return
  idleTimer = setTimeout(() => dimEl.classList.remove('hidden'), IDLE_DIM_MS)
}

function tileKey(identity, source) {
  return identity + ':' + source
}

function ensureTile(identity, source, label) {
  const key = tileKey(identity, source)
  let tile = tiles.get(key)
  if (tile) return tile

  const wrap = el('div', STAGE_TILE_SCROLL)
  const video = el('video', 'h-full w-full ' + (source === 'screen' ? 'object-contain' : 'object-cover'))
  video.autoplay = true
  video.playsInline = true
  video.muted = true
  const placeholder = el('div', 'absolute inset-0 flex items-center justify-center text-2xl font-semibold text-overlay1')
  placeholder.textContent = (label || '?').trim().charAt(0).toUpperCase()
  const caption = el('div', STAGE_CAPTION)
  const name = el('span', 'truncate text-subtext0', source === 'screen' ? label + ' screen' : label)
  const mutedFlag = el('span', 'hidden shrink-0 text-red', 'muted')
  const speakerEl = el('div', 'pointer-events-none absolute inset-0 hidden rounded-xl ring-2 ring-mauve')
  caption.appendChild(name)
  caption.appendChild(mutedFlag)
  wrap.appendChild(video)
  wrap.appendChild(placeholder)
  wrap.appendChild(caption)
  wrap.appendChild(speakerEl)

  tile = { wrap, video, placeholder, caption, name, mutedFlag, speakerEl, source, track: null }
  tiles.set(key, tile)
  scheduleRender()
  return tile
}

function removeTile(identity, source) {
  const key = tileKey(identity, source)
  const tile = tiles.get(key)
  if (!tile) return
  if (tile.track) tile.track.detach(tile.video)
  tile.wrap.remove()
  tiles.delete(key)
  scheduleRender()
}

function attachVideo(identity, source, label, track) {
  const tile = ensureTile(identity, source, label)
  if (tile.track === track) return
  if (tile.track) tile.track.detach(tile.video)
  tile.track = track
  track.attach(tile.video)
  tile.placeholder.classList.add('hidden')
}

function detachVideo(identity, source, track) {
  const tile = tiles.get(tileKey(identity, source))
  if (!tile) return
  if (tile.track) tile.track.detach(tile.video)
  tile.track = null
  if (source === 'screen') removeTile(identity, source)
  else tile.placeholder.classList.remove('hidden')
}

function labelOf(participant) {
  if (state.users.has(participant.identity)) return user(participant.identity).display_name
  return participant.name || participant.identity
}

function sourceOf(publication) {
  const LK = lk()
  if (!LK) return 'camera'
  return publication.source === LK.Track.Source.ScreenShare ? 'screen' : 'camera'
}

function wire() {
  const LK = lk()
  const E = LK.RoomEvent

  room.on(E.ParticipantConnected, (p) => ensureTile(p.identity, 'camera', labelOf(p)))
  room.on(E.ParticipantDisconnected, (p) => {
    removeTile(p.identity, 'camera')
    removeTile(p.identity, 'screen')
  })

  room.on(E.TrackSubscribed, (track, publication, participant) => {
    if (track.kind === LK.Track.Kind.Audio) {
      const element = track.attach()
      element.autoplay = true
      audioSinkEl.appendChild(element)
      return
    }
    attachVideo(participant.identity, sourceOf(publication), labelOf(participant), track)
  })

  room.on(E.TrackUnsubscribed, (track, publication, participant) => {
    if (track.kind === LK.Track.Kind.Audio) {
      for (const element of track.detach()) element.remove()
      return
    }
    detachVideo(participant.identity, sourceOf(publication), track)
  })

  room.on(E.LocalTrackPublished, (publication) => {
    const track = publication.track
    if (!track || track.kind === LK.Track.Kind.Audio) return
    attachVideo(room.localParticipant.identity, sourceOf(publication), labelOf(room.localParticipant), track)
  })

  room.on(E.LocalTrackUnpublished, (publication) => {
    detachVideo(room.localParticipant.identity, sourceOf(publication), publication.track)
  })

  room.on(E.TrackMuted, (publication, participant) => {
    if (publication.kind !== LK.Track.Kind.Audio) return
    const tile = tiles.get(tileKey(participant.identity, 'camera'))
    if (tile) tile.mutedFlag.classList.remove('hidden')
  })

  room.on(E.TrackUnmuted, (publication, participant) => {
    if (publication.kind !== LK.Track.Kind.Audio) return
    const tile = tiles.get(tileKey(participant.identity, 'camera'))
    if (tile) tile.mutedFlag.classList.add('hidden')
  })

  room.on(E.ActiveSpeakersChanged, (speakers) => {
    const loud = new Set(speakers.map((p) => p.identity))
    for (const [key, tile] of tiles) {
      const identity = key.slice(0, key.lastIndexOf(':'))
      tile.speakerEl.classList.toggle('hidden', !loud.has(identity))
    }
  })

  room.on(E.AudioPlaybackStatusChanged, () => render())

  room.on(E.Disconnected, () => teardown())
}

function callError(stage, err) {
  if (stage === 'microphone' && err && err.name === 'NotAllowedError') return MIC_DENIED
  const detail = err && err.message
  return detail ? CALL_ERRORS[stage] + ' ' + detail : CALL_ERRORS[stage]
}

async function join(call, mic) {
  const LK = lk()
  connecting = true
  activeCall = call
  statusText = 'Connecting'
  notify('calls')
  render()

  let stage = 'token'
  try {
    const grant = await api.get('/api/calls/' + call.id + '/token')
    stage = 'connect'
    room = new LK.Room(roomOptions())
    wire()
    await room.connect(livekitUrl(grant.url), grant.token)
    stage = 'publish'
    await room.localParticipant.publishTrack(mic)
    ensureTile(room.localParticipant.identity, 'camera', labelOf(room.localParticipant))
    for (const participant of (room.remoteParticipants || room.participants || new Map()).values()) {
      ensureTile(participant.identity, 'camera', labelOf(participant))
    }
    joined = true
    micOn = true
    cameraOn = false
    screenOn = false
    statusText = ''
    await acquireWakeLock()
    resetIdle()
  } catch (err) {
    mic.stop()
    statusText = ''
    toast(callError(stage, err), { severity: 'error' })
    if (room) {
      try {
        await room.disconnect()
      } catch {}
    }
    room = null
    joined = false
  }
  connecting = false
  notify('calls')
  render()
}

async function leave() {
  const call = activeCall
  joined = false
  connecting = false
  if (room) {
    try {
      await room.disconnect()
    } catch {}
  }
  teardown()
  if (call) {
    try {
      await api.del('/api/calls/' + call.id + '/me')
    } catch {}
  }
}

function teardown() {
  joined = false
  dismissed = false
  statusText = ''
  expanded = false
  room = null
  activeCall = null
  screenOn = false
  cameraOn = false
  for (const key of Array.from(tiles.keys())) {
    const index = key.lastIndexOf(':')
    removeTile(key.slice(0, index), key.slice(index + 1))
  }
  if (overflowEl) {
    overflowEl.remove()
    overflowEl = null
  }
  audioSinkEl.replaceChildren()
  clearTimeout(idleTimer)
  dimEl.classList.add('hidden')
  releaseWakeLock()
  notify('calls')
  render()
}

async function start(containerId) {
  const cid = containerId || state.current.containerId
  if (!cid) return
  dismissed = false
  const LK = lk()
  if (!LK) {
    statusText = ''
    toast('The call client did not load.', { severity: 'error' })
    render()
    return
  }
  if (connecting || joined) {
    render()
    return
  }
  const existing = state.calls.get(cid)
  const live = existing && !existing.ended_at ? existing : null

  statusText = live ? 'Connecting' : 'Starting'
  render()

  let mic
  try {
    mic = await LK.createLocalAudioTrack(audioCaptureOptions())
  } catch (err) {
    statusText = ''
    toast(callError('microphone', err), { severity: 'error' })
    render()
    return
  }

  if (live) {
    await join(live, mic)
    return
  }

  try {
    const call = await api.post('/api/containers/' + cid + '/call', {})
    state.calls.set(cid, call)
    notify('calls')
    await join(call, mic)
  } catch (err) {
    mic.stop()
    statusText = ''
    toast(err.message || 'Could not start a call.', { severity: 'error' })
    render()
  }
}

async function toggleMic() {
  if (!room) return
  micOn = !micOn
  await room.localParticipant.setMicrophoneEnabled(micOn)
  render()
}

async function toggleCamera() {
  if (!room) return
  cameraOn = !cameraOn
  await room.localParticipant.setCameraEnabled(cameraOn)
  render()
}

async function toggleScreen() {
  if (!room) return
  screenOn = !screenOn
  try {
    await room.localParticipant.setScreenShareEnabled(screenOn, screenShareOptions())
  } catch {
    screenOn = false
  }
  render()
}

async function toggleRecording() {
  const call = liveCall()
  if (!call) return
  try {
    const updated = await api.post('/api/calls/' + call.id + '/recording', { on: call.recording_state !== 'recording' })
    if (updated && updated.container_id) state.calls.set(updated.container_id, updated)
    notify('calls')
  } catch (err) {
    toast(err.message || 'Could not change recording.', { severity: 'error' })
  }
  render()
}

function callButton(name, label, tone, onClick, pressed) {
  const button = el('button', 'grid h-11 w-11 shrink-0 place-items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve ' + TONES[tone])
  button.type = 'button'
  button.title = label
  button.setAttribute('aria-label', label)
  if (pressed != null) button.setAttribute('aria-pressed', pressed ? 'true' : 'false')
  button.appendChild(icon(name, 'h-5 w-5'))
  if (onClick) button.addEventListener('click', onClick)
  return button
}

function recordButton() {
  const recording = recordingState()
  if (recording === 'processing') {
    const button = callButton('loader-circle', 'Saving the recording', 'busy', null, true)
    button.disabled = true
    button.setAttribute('aria-busy', 'true')
    button.querySelector('[data-lucide]').classList.add('animate-spin')
    return button
  }
  if (recording === 'recording') return callButton('circle-stop', 'Stop recording', 'alert', toggleRecording, true)
  return callButton('circle-dot', 'Start recording', 'off', toggleRecording, false)
}

function renderControls() {
  controlsEl.replaceChildren()
  if (!joined) return

  controlsEl.appendChild(callButton(micOn ? 'mic' : 'mic-off', micOn ? 'Mute' : 'Unmute', micOn ? 'on' : 'alert', toggleMic, !micOn))
  controlsEl.appendChild(callButton(cameraOn ? 'video' : 'video-off',
    cameraOn ? 'Turn the camera off' : 'Turn the camera on', cameraOn ? 'on' : 'off', toggleCamera, cameraOn))
  if (screenShareSupported()) {
    controlsEl.appendChild(callButton(screenOn ? 'screen-share' : 'screen-share-off',
      screenOn ? 'Stop sharing' : 'Share your screen', screenOn ? 'accent' : 'off', toggleScreen, screenOn))
  }
  controlsEl.appendChild(recordButton())
  controlsEl.appendChild(callButton('phone-off', 'Leave the call', 'danger', leave))
  drawIcons(controlsEl)
}

function renderHeader() {
  headerEl.replaceChildren()
  const bar = el('div', 'flex h-12 shrink-0 items-center gap-2 px-3')
  bar.appendChild(el('h2', 'flex-1 text-message font-semibold text-text', 'Call'))
  if (recordingState() === 'recording') {
    const badge = el('span', 'flex shrink-0 items-center gap-1.5 rounded-full bg-red px-2 py-0.5 text-micro font-semibold uppercase tracking-widest text-crust')
    badge.appendChild(el('span', 'h-1.5 w-1.5 shrink-0 animate-pulse rounded-full bg-crust'))
    badge.appendChild(el('span', '', 'recording'))
    bar.appendChild(badge)
  }
  bar.appendChild(iconButton(expanded ? 'minimize-2' : 'maximize-2',
    expanded ? 'Dock the call panel' : 'Expand the call panel', toggleExpand, 'hidden xl:grid'))
  bar.appendChild(iconButton('x', 'Hide the call panel', dismiss))
  headerEl.appendChild(bar)
  drawIcons(headerEl)
}

function toggleExpand() {
  expanded = !expanded
  notify('calls')
  render()
}

export function reopen() {
  if (!joined && !connecting) return
  dismissed = false
  notify('calls')
  render()
}

function dismiss() {
  dismissed = true
  notify('calls')
  render()
}

function syncChildren(container, nodes) {
  const current = container.children
  if (current.length === nodes.length && nodes.every((node, i) => current[i] === node)) return
  container.replaceChildren(...nodes)
}

function overflowCell(count, tileClass) {
  if (!overflowEl) overflowEl = el('div')
  overflowEl.className = tileClass + ' ' + OVERFLOW_TILE
  overflowEl.textContent = '+' + count + (count === 1 ? ' other' : ' others')
  return overflowEl
}

function placeTiles() {
  const wide = stripMedia.matches
  stageEl.className = wide ? STAGE_FIT : STAGE_SCROLL
  if (!joined) {
    syncChildren(stripEl, [])
    syncChildren(stageEl, [])
    return
  }

  const screens = []
  const cameras = []
  for (const tile of tiles.values()) {
    if (tile.source === 'screen') screens.push(tile)
    else cameras.push(tile)
  }
  const local = room ? tiles.get(tileKey(room.localParticipant.identity, 'camera')) : null

  let strip = []
  let stage = []
  if (!wide) {
    stage = screens.concat(cameras)
  } else if (screens.length) {
    strip = cameras
    stage = screens
  } else {
    strip = local ? [local] : []
    stage = cameras.filter((tile) => tile !== local)
  }
  if (!stage.length) {
    stage = strip
    strip = []
  }

  const spilled = stage.length > MAX_STAGE_TILES ? stage.length - (MAX_STAGE_TILES - 1) : 0
  if (spilled) stage = stage.slice(0, MAX_STAGE_TILES - 1)

  const stageTile = wide ? STAGE_TILE_FIT : STAGE_TILE_SCROLL
  const stageNodes = stage.map((tile) => {
    tile.wrap.className = stageTile + (tile.source === 'screen' ? ' col-span-full' : '')
    tile.caption.className = STAGE_CAPTION
    return tile.wrap
  })
  if (spilled) stageNodes.push(overflowCell(spilled, stageTile))

  const stripNodes = strip.map((tile) => {
    tile.wrap.className = STRIP_TILE
    tile.caption.className = STRIP_CAPTION
    return tile.wrap
  })

  syncChildren(stripEl, stripNodes)
  syncChildren(stageEl, stageNodes)
}

function render() {
  const app = document.getElementById('app')
  const open = visible()
  if (app) app.dataset.call = !open ? 'hidden' : expanded ? 'expanded' : 'docked'
  if (!open) return

  renderHeader()
  renderControls()
  placeTiles()
  audioButtonEl.classList.toggle('hidden', !joined || !room || room.canPlaybackAudio)
  statusEl.textContent = statusText
  statusEl.classList.toggle('hidden', !statusText)
}

export function mount(root) {
  rootEl = root

  panelEl = el('div', 'flex h-full flex-col')

  headerEl = el('div')
  panelEl.appendChild(headerEl)

  const body = el('div', 'relative flex min-h-0 flex-1 flex-col gap-2 p-3')
  audioButtonEl = el('button', 'hidden w-full rounded-xl bg-mauve px-3 py-2 text-xs font-semibold text-crust transition-colors hover:brightness-110', 'Tap to hear the call')
  audioButtonEl.type = 'button'
  audioButtonEl.addEventListener('click', async () => {
    if (!room) return
    try {
      await room.startAudio()
    } catch {}
    render()
  })
  body.appendChild(audioButtonEl)
  statusEl = el('p', 'hidden w-full py-2 text-left text-xs text-peach')
  body.appendChild(statusEl)
  stripEl = el('div', STRIP)
  body.appendChild(stripEl)
  stageEl = el('div', STAGE_SCROLL)
  body.appendChild(stageEl)
  panelEl.appendChild(body)

  dimEl = el('div', 'absolute inset-0 z-10 hidden bg-scrim')
  panelEl.appendChild(dimEl)

  controlsEl = el('div', 'flex shrink-0 items-center justify-center gap-2 border-t border-surface0 px-3 py-2')
  panelEl.appendChild(controlsEl)

  audioSinkEl = el('div', 'hidden')
  panelEl.appendChild(audioSinkEl)

  rootEl.replaceChildren(panelEl)

  rootEl.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return
    e.preventDefault()
    if (expanded) {
      toggleExpand()
      return
    }
    dismiss()
  })

  window.addEventListener('isane:call-start', (e) => start(e.detail && e.detail.containerId))

  stripMedia.addEventListener('change', scheduleRender)

  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible' && joined) acquireWakeLock()
  })

  for (const event of ['pointerdown', 'pointermove', 'keydown', 'touchstart']) {
    window.addEventListener(event, () => {
      if (joined) resetIdle()
    }, { passive: true })
  }

  socket.on('call_ended', (d) => {
    if (activeCall && d.call_id === activeCall.id) leave()
  })

  subscribe(scheduleRender)
  render()
}

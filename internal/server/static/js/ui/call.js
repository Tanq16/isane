import { state, subscribe, notify, user } from '../store.js'
import * as api from '../api.js'
import * as socket from '../socket.js'

const IDLE_DIM_MS = 240000

function lk() {
  return globalThis.LivekitClient || globalThis.LiveKit || null
}

let rootEl = null
let panelEl = null
let headerEl = null
let gridEl = null
let controlsEl = null
let audioSinkEl = null
let promptEl = null
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

function screenShareSupported() {
  return Boolean(navigator.mediaDevices && typeof navigator.mediaDevices.getDisplayMedia === 'function')
}

function livekitUrl(fromServer) {
  if (fromServer) return fromServer
  const scheme = location.protocol === 'https:' ? 'wss://' : 'ws://'
  return scheme + location.host + '/livekit'
}

function liveCall() {
  const cid = state.current.containerId
  if (!cid) return null
  const call = state.calls.get(cid)
  if (!call || call.ended_at) return null
  return call
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
    audioCaptureDefaults: {
      echoCancellation: true,
      noiseSuppression: true,
      autoGainControl: true,
    },
    videoCaptureDefaults: {
      resolution: { width: Math.round((height * 16) / 9), height, frameRate: framerate },
    },
    publishDefaults: {
      videoCodec: q.codec || 'vp8',
      simulcast: true,
      videoSimulcastLayers: layers,
      videoEncoding: { maxBitrate: video.max_bitrate || 1700000, maxFramerate: framerate },
      screenShareEncoding: { maxBitrate: screen.max_bitrate || 2500000, maxFramerate: screen.max_framerate || 15 },
      audioPreset: { maxBitrate: audio.max_bitrate || 48000 },
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

  const wrap = el(
    'div',
    source === 'screen'
      ? 'relative col-span-full aspect-video overflow-hidden rounded-xl border border-surface1 bg-base'
      : 'relative aspect-video overflow-hidden rounded-xl border border-surface0 bg-base'
  )
  const video = el('video', 'h-full w-full object-cover')
  video.autoplay = true
  video.playsInline = true
  video.muted = true
  const placeholder = el('div', 'absolute inset-0 flex items-center justify-center text-2xl font-semibold text-overlay1')
  placeholder.textContent = (label || '?').trim().charAt(0).toUpperCase()
  const caption = el('div', 'absolute inset-x-0 bottom-0 flex items-center gap-2 bg-crust/80 px-2 py-1 text-xs')
  const name = el('span', 'truncate text-subtext0', source === 'screen' ? label + ' screen' : label)
  const mutedFlag = el('span', 'hidden shrink-0 text-red', 'muted')
  caption.appendChild(name)
  caption.appendChild(mutedFlag)
  wrap.appendChild(video)
  wrap.appendChild(placeholder)
  wrap.appendChild(caption)
  gridEl.appendChild(wrap)

  tile = { wrap, video, placeholder, name, mutedFlag, track: null }
  tiles.set(key, tile)
  return tile
}

function removeTile(identity, source) {
  const key = tileKey(identity, source)
  const tile = tiles.get(key)
  if (!tile) return
  if (tile.track) tile.track.detach(tile.video)
  tile.wrap.remove()
  tiles.delete(key)
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
      tile.wrap.classList.toggle('ring-2', loud.has(identity))
      tile.wrap.classList.toggle('ring-mauve', loud.has(identity))
    }
  })

  room.on(E.Disconnected, () => teardown())
}

async function join(call) {
  const LK = lk()
  if (!LK) {
    statusText = 'The call client did not load.'
    render()
    return
  }
  if (connecting || joined) return
  connecting = true
  activeCall = call
  statusText = 'Connecting'
  render()

  try {
    const grant = await api.get('/api/calls/' + call.id + '/token')
    room = new LK.Room(roomOptions())
    wire()
    await room.connect(livekitUrl(grant.url), grant.token)
    await room.localParticipant.setMicrophoneEnabled(true)
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
    statusText = err.message || 'Could not join the call.'
    room = null
    joined = false
  }
  connecting = false
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
  room = null
  activeCall = null
  screenOn = false
  cameraOn = false
  for (const key of Array.from(tiles.keys())) {
    const index = key.lastIndexOf(':')
    removeTile(key.slice(0, index), key.slice(index + 1))
  }
  audioSinkEl.replaceChildren()
  clearTimeout(idleTimer)
  dimEl.classList.add('hidden')
  releaseWakeLock()
  render()
}

async function start(containerId) {
  const cid = containerId || state.current.containerId
  if (!cid) return
  const existing = state.calls.get(cid)
  if (existing && !existing.ended_at) {
    join(existing)
    return
  }
  statusText = 'Starting'
  render()
  try {
    const call = await api.post('/api/containers/' + cid + '/call', {})
    state.calls.set(cid, call)
    notify('calls')
    await join(call)
  } catch (err) {
    statusText = err.message || 'Could not start a call.'
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
  if (!activeCall) return
  const on = activeCall.recording_state !== 'recording'
  try {
    await api.post('/api/calls/' + activeCall.id + '/recording', { on })
    activeCall.recording_state = on ? 'recording' : 'processing'
    notify('calls')
  } catch (err) {
    statusText = err.message || 'Could not change recording.'
  }
  render()
}

function controlButton(label, active, fn, danger) {
  const base = danger
    ? 'rounded-lg bg-red px-3 py-1.5 text-xs font-semibold text-crust'
    : active
      ? 'rounded-lg bg-mauve px-3 py-1.5 text-xs font-semibold text-crust'
      : 'rounded-lg border border-surface1 px-3 py-1.5 text-xs text-subtext0 hover:bg-surface0 hover:text-text'
  const button = el('button', base, label)
  button.type = 'button'
  button.addEventListener('click', fn)
  return button
}

function renderControls() {
  controlsEl.replaceChildren()
  if (!joined) return

  controlsEl.appendChild(controlButton(micOn ? 'Mute' : 'Unmute', !micOn, toggleMic))
  controlsEl.appendChild(controlButton(cameraOn ? 'Camera off' : 'Camera on', cameraOn, toggleCamera))
  if (screenShareSupported()) {
    controlsEl.appendChild(controlButton(screenOn ? 'Stop sharing' : 'Share screen', screenOn, toggleScreen))
  }
  const recording = activeCall && activeCall.recording_state === 'recording'
  controlsEl.appendChild(controlButton(recording ? 'Stop recording' : 'Record', recording, toggleRecording))
  controlsEl.appendChild(controlButton('Leave', false, leave, true))
}

function renderHeader() {
  headerEl.replaceChildren()
  const bar = el('div', 'flex items-center gap-2 border-b border-surface0 px-3 py-3')
  bar.appendChild(el('h2', 'flex-1 font-display text-sm font-semibold text-text', 'Call'))
  if (activeCall && activeCall.recording_state === 'recording') {
    bar.appendChild(el('span', 'rounded bg-red px-1.5 py-0.5 text-xs font-semibold text-crust', 'recording'))
  }
  headerEl.appendChild(bar)
}

function renderPrompt(call) {
  promptEl.replaceChildren()
  if (joined || !call) {
    promptEl.classList.add('hidden')
    return
  }
  promptEl.classList.remove('hidden')
  const starter = user(call.started_by)
  promptEl.appendChild(el('p', 'text-sm text-subtext0', starter.display_name + ' started a call.'))
  const joinButton = el('button', 'mt-2 rounded-lg bg-mauve px-3 py-1.5 text-xs font-semibold text-crust', 'Join call')
  joinButton.type = 'button'
  joinButton.addEventListener('click', () => start(call.container_id))
  promptEl.appendChild(joinButton)
}

function render() {
  const call = liveCall()
  const visible = joined || connecting || Boolean(call) || Boolean(statusText)
  rootEl.classList.toggle('hidden', !visible)
  if (!visible) return

  renderHeader()
  renderPrompt(call)
  renderControls()
  gridEl.classList.toggle('hidden', !joined)
  statusEl.textContent = statusText
  statusEl.classList.toggle('hidden', !statusText)
}

export function mount(root) {
  rootEl = root
  rootEl.classList.add('hidden')

  panelEl = el('div', 'fixed inset-0 z-30 flex flex-col bg-mantle md:relative md:inset-auto md:z-auto md:w-96 md:shrink-0 md:border-l md:border-surface0')

  headerEl = el('div')
  panelEl.appendChild(headerEl)

  const body = el('div', 'relative flex-1 overflow-y-auto p-3')
  promptEl = el('div', 'hidden rounded-xl border border-surface0 bg-base p-3')
  body.appendChild(promptEl)
  statusEl = el('button', 'hidden w-full py-2 text-left text-xs text-peach')
  statusEl.type = 'button'
  statusEl.addEventListener('click', () => {
    statusText = ''
    render()
  })
  body.appendChild(statusEl)
  gridEl = el('div', 'grid grid-cols-1 gap-2 sm:grid-cols-2')
  body.appendChild(gridEl)
  panelEl.appendChild(body)

  dimEl = el('div', 'absolute inset-0 z-10 hidden bg-crust/90')
  panelEl.appendChild(dimEl)

  controlsEl = el('div', 'flex flex-wrap gap-2 border-t border-surface0 p-3')
  panelEl.appendChild(controlsEl)

  audioSinkEl = el('div', 'hidden')
  panelEl.appendChild(audioSinkEl)

  rootEl.replaceChildren(panelEl)

  window.addEventListener('isane:call-start', (e) => start(e.detail && e.detail.containerId))

  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible' && joined) acquireWakeLock()
  })

  for (const event of ['pointerdown', 'pointermove', 'keydown', 'touchstart']) {
    window.addEventListener(event, () => {
      if (joined) resetIdle()
    }, { passive: true })
  }

  window.addEventListener('pagehide', () => {
    if (room) room.disconnect()
  })

  socket.on('call_ended', (d) => {
    if (activeCall && d.call_id === activeCall.id) leave()
  })

  subscribe(scheduleRender)
  render()
}

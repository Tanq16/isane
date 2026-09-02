import { subscribe, unreadTotal } from './store.js'

const SOURCE_URL = '/static/icons/icon-192.png'
const BASE_TITLE = 'Isane'
const SIZE = 32

let source = null
let painted = ''

function label(count) {
  return count > 9 ? '9+' : String(count)
}

function draw(count) {
  const canvas = document.createElement('canvas')
  canvas.width = SIZE
  canvas.height = SIZE
  const ctx = canvas.getContext('2d')
  if (!ctx) return null
  ctx.drawImage(source, 0, 0, SIZE, SIZE)
  if (count > 0) {
    const style = getComputedStyle(document.documentElement)
    ctx.beginPath()
    ctx.arc(SIZE - 10, 10, 10, 0, Math.PI * 2)
    ctx.fillStyle = style.getPropertyValue('--ctp-red').trim() || '#f38ba8'
    ctx.fill()
    ctx.fillStyle = style.getPropertyValue('--ctp-crust').trim() || '#11111b'
    ctx.font = 'bold 13px sans-serif'
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'
    ctx.fillText(label(count), SIZE - 10, 11)
  }
  return canvas.toDataURL('image/png')
}

function paint() {
  const count = unreadTotal()
  const key = String(count)
  if (key === painted) return
  painted = key
  document.title = count > 0 ? '(' + label(count) + ') ' + BASE_TITLE : BASE_TITLE
  if (!source) return
  const link = document.getElementById('favicon')
  if (!link) return
  const url = draw(count)
  if (!url) return
  link.type = 'image/png'
  link.href = url
}

export function mount() {
  source = new Image()
  source.addEventListener('load', () => {
    painted = ''
    paint()
  })
  source.addEventListener('error', () => {
    source = null
  })
  source.src = SOURCE_URL
  subscribe((keys) => {
    if (keys.includes('containers')) paint()
  })
  paint()
}

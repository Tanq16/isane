const BASE = '/api'

export class ApiError extends Error {
  constructor(status, body, message) {
    super(message || `request failed with status ${status}`)
    this.name = 'ApiError'
    this.status = status
    this.body = body
  }
}

function url(path, params) {
  const raw = String(path ?? '')
  const p = raw === BASE || raw.startsWith(`${BASE}/`)
    ? raw
    : BASE + (raw.startsWith('/') ? raw : `/${raw}`)
  if (!params) return p
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '') continue
    q.set(k, String(v))
  }
  const s = q.toString()
  return s ? `${p}?${s}` : p
}

function decode(text) {
  if (!text) return null
  try {
    return JSON.parse(text)
  } catch {
    return text
  }
}

function messageOf(body, fallback) {
  if (body && typeof body === 'object' && typeof body.error === 'string') return body.error
  return fallback
}

async function request(method, path, body, params) {
  const init = { method, credentials: 'same-origin', headers: { Accept: 'application/json' } }
  if (body !== undefined) {
    init.headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(body)
  }
  let res
  try {
    res = await fetch(url(path, params), init)
  } catch (err) {
    throw new ApiError(0, null, err && err.message ? err.message : 'network unreachable')
  }
  const parsed = decode(await res.text())
  if (!res.ok) throw new ApiError(res.status, parsed, messageOf(parsed, res.statusText))
  return parsed
}

export async function get(path, params) {
  return request('GET', path, undefined, params)
}

export async function post(path, body) {
  return request('POST', path, body ?? {})
}

export async function patch(path, body) {
  return request('PATCH', path, body ?? {})
}

export async function put(path, body) {
  return request('PUT', path, body ?? {})
}

export async function del(path, body) {
  return request('DELETE', path, body)
}

export async function upload(file, onProgress) {
  return new Promise((resolve, reject) => {
    const form = new FormData()
    form.append('file', file, file.name)
    const xhr = new XMLHttpRequest()
    xhr.open('POST', url('/uploads'))
    xhr.withCredentials = true
    if (typeof onProgress === 'function') {
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) onProgress(e.loaded / e.total)
      }
    }
    xhr.onload = () => {
      const parsed = decode(xhr.responseText)
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(parsed)
        return
      }
      reject(new ApiError(xhr.status, parsed, messageOf(parsed, 'upload failed')))
    }
    xhr.onerror = () => reject(new ApiError(0, null, 'network unreachable'))
    xhr.onabort = () => reject(new ApiError(0, null, 'upload cancelled'))
    xhr.send(form)
  })
}

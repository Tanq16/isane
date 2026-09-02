import { drawIcons } from './ui/dom.js'

const DROP_TAGS = new Set([
  'script', 'style', 'iframe', 'frame', 'frameset', 'object', 'embed', 'applet',
  'link', 'meta', 'base', 'form', 'button', 'select', 'option', 'textarea',
  'svg', 'math', 'template', 'noscript', 'canvas', 'audio', 'video', 'source',
  'track', 'portal', 'dialog', 'slot',
])

const ALLOWED_TAGS = new Map(Object.entries({
  a: ['href', 'title'],
  blockquote: [],
  br: [],
  code: ['class'],
  del: [],
  div: ['class'],
  em: [],
  h1: ['id'],
  h2: ['id'],
  h3: ['id'],
  h4: ['id'],
  h5: ['id'],
  h6: ['id'],
  hr: [],
  i: ['class', 'data-lucide'],
  img: ['src', 'alt', 'title', 'width', 'height'],
  input: ['type', 'checked', 'disabled'],
  kbd: [],
  li: [],
  ol: ['start'],
  p: [],
  pre: ['class'],
  s: [],
  span: ['class'],
  strong: [],
  sub: [],
  sup: [],
  table: [],
  tbody: [],
  td: ['align', 'colspan', 'rowspan'],
  tfoot: [],
  th: ['align', 'colspan', 'rowspan'],
  thead: [],
  tr: [],
  ul: [],
}))

const CALLOUT_ICONS = {
  tip: 'lightbulb',
  info: 'info',
  danger: 'triangle-alert',
  warning: 'triangle-alert',
  note: 'sticky-note',
}

const nodeCache = new WeakMap()
const diagrams = new Set()
let markedReady = false
let mermaidTheme = ''

function escapeHtml(value) {
  return String(value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

function slug(text) {
  return String(text).toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)+/g, '')
}

function initMarked() {
  if (markedReady || typeof marked === 'undefined') return
  markedReady = true
  const renderer = {
    code(token) {
      const text = token.text ?? ''
      const language = String(token.lang ?? '').trim().split(/\s+/)[0]
      if (language === 'mermaid') {
        return `<div class="overflow-x-auto my-6"><div class="mermaid">${escapeHtml(text)}</div></div>`
      }
      if (typeof hljs === 'undefined') {
        return `<pre><code class="hljs language-plaintext">${escapeHtml(text)}</code></pre>`
      }
      const validLang = hljs.getLanguage(language) ? language : 'plaintext'
      let highlighted = escapeHtml(text)
      try {
        highlighted = hljs.highlight(text, { language: validLang }).value
      } catch {
        highlighted = escapeHtml(text)
      }
      return `<pre><code class="hljs language-${validLang}">${highlighted}</code></pre>`
    },
    heading(token) {
      const { tokens, depth } = token
      const text = this.parser.parseInline(tokens)
      return `<h${depth} id="${slug(text.replace(/<[^>]*>/g, ''))}">${text}</h${depth}>`
    },
    image(token) {
      return `<img src="${escapeHtml(token.href ?? '')}" alt="${escapeHtml(token.text ?? '')}">`
    },
    blockquote(token) {
      const body = this.parser.parse(token.tokens)
      const match = String(token.text ?? '').match(/^\[!(TIP|NOTE|INFO|WARNING|DANGER)\]/i)
      if (!match) return `<blockquote>${body}</blockquote>`
      const type = match[1].toLowerCase()
      const clean = body.replace(/<p>\s*\[!(TIP|NOTE|INFO|WARNING|DANGER)\]\s*/i, '<p>')
      const icon = CALLOUT_ICONS[type] || 'info'
      return `<div class="callout ${type}"><div class="callout-icon"><i data-lucide="${icon}"></i></div><div class="callout-content">${clean}</div></div>`
    },
    html(token) {
      return escapeHtml(token.text ?? token.raw ?? '')
    },
  }
  marked.use({ gfm: true, breaks: true, renderer })
}

function paletteReader() {
  const css = getComputedStyle(document.documentElement)
  return (name) => css.getPropertyValue(`--ctp-${name}`).trim()
}

function initMermaid() {
  if (typeof mermaid === 'undefined') return
  const theme = document.documentElement.classList.contains('dark') ? 'dark' : 'light'
  if (mermaidTheme === theme) return
  mermaidTheme = theme
  const c = paletteReader()
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: 'strict',
    theme: 'base',
    fontFamily: 'Inter',
    themeVariables: {
      darkMode: theme === 'dark',
      background: c('base'),
      mainBkg: c('base'),

      primaryColor: c('surface0'),
      primaryTextColor: c('text'),
      primaryBorderColor: c('blue'),
      secondaryColor: c('surface1'),
      secondaryTextColor: c('text'),
      secondaryBorderColor: c('overlay1'),
      tertiaryColor: c('surface0'),
      tertiaryTextColor: c('text'),
      tertiaryBorderColor: c('surface2'),

      lineColor: c('blue'),
      arrowheadColor: c('blue'),
      textColor: c('text'),
      titleColor: c('mauve'),
      noteBkgColor: c('surface1'),
      noteTextColor: c('yellow'),
      noteBorderColor: c('surface2'),

      nodeBkg: c('surface0'),
      nodeBorder: c('blue'),
      clusterBkg: c('mantle'),
      clusterBorder: c('surface2'),
      defaultLinkColor: c('blue'),
      edgeLabelBackground: c('surface0'),
      nodeTextColor: c('text'),

      actorBkg: c('surface0'),
      actorBorder: c('blue'),
      actorTextColor: c('text'),
      actorLineColor: c('surface2'),
      signalColor: c('pink'),
      signalTextColor: c('text'),
      labelBoxBkgColor: c('surface1'),
      labelBoxBorderColor: c('surface2'),
      labelTextColor: c('text'),
      loopTextColor: c('yellow'),
      activationBorderColor: c('mauve'),
      activationBkgColor: c('surface1'),
      sequenceNumberColor: c('base'),

      sectionBkgColor: c('mantle'),
      altSectionBkgColor: c('base'),
      sectionBkgColor2: c('crust'),
      taskBkgColor: c('blue'),
      taskBorderColor: c('lavender'),
      taskTextColor: c('base'),
      taskTextLightColor: c('base'),
      taskTextDarkColor: c('text'),
      taskTextOutsideColor: c('text'),
      taskTextClickableColor: c('sky'),
      activeTaskBkgColor: c('mauve'),
      activeTaskBorderColor: c('pink'),
      doneTaskBkgColor: c('surface1'),
      doneTaskBorderColor: c('surface2'),
      critBkgColor: c('red'),
      critBorderColor: c('maroon'),
      gridColor: c('surface0'),
      todayLineColor: c('red'),

      pie1: c('mauve'), pie2: c('blue'), pie3: c('green'), pie4: c('yellow'),
      pie5: c('red'), pie6: c('teal'), pie7: c('peach'), pie8: c('sky'),
      pie9: c('pink'), pie10: c('sapphire'), pie11: c('maroon'), pie12: c('lavender'),
      pieTitleTextColor: c('text'),
      pieSectionTextColor: c('base'),
      pieLegendTextColor: c('text'),
      pieStrokeColor: c('base'),
      pieOuterStrokeColor: c('surface0'),

      git0: c('blue'), git1: c('mauve'), git2: c('green'), git3: c('yellow'),
      git4: c('red'), git5: c('teal'), git6: c('peach'), git7: c('sapphire'),
      gitInv0: c('base'), gitInv1: c('base'), gitInv2: c('base'), gitInv3: c('base'),
      gitInv4: c('base'), gitInv5: c('base'), gitInv6: c('base'), gitInv7: c('base'),
      commitLabelColor: c('subtext1'),
      commitLabelBackground: c('base'),
      tagLabelColor: c('base'),
      tagLabelBackground: c('yellow'),
      tagLabelBorder: c('peach'),

      labelBackgroundColor: c('surface0'),

      cScale0: c('surface0'), cScale1: c('blue'), cScale2: c('mauve'), cScale3: c('green'),
      cScale4: c('yellow'), cScale5: c('red'), cScale6: c('teal'), cScale7: c('peach'),
      cScale8: c('sky'), cScale9: c('pink'), cScale10: c('sapphire'), cScale11: c('lavender'),
    },
  })
}

function safeUrl(value) {
  const v = String(value ?? '').trim()
  if (!v || v.startsWith('//')) return null
  if (v.startsWith('/') || v.startsWith('#') || v.startsWith('?')) return v
  if (/^(https?:\/\/|mailto:)/i.test(v)) return v
  return null
}

function scrub(node) {
  for (const child of Array.from(node.childNodes)) {
    if (child.nodeType === Node.TEXT_NODE) continue
    if (child.nodeType !== Node.ELEMENT_NODE) {
      child.remove()
      continue
    }
    const tag = child.tagName.toLowerCase()
    if (DROP_TAGS.has(tag)) {
      child.remove()
      continue
    }
    const allowed = ALLOWED_TAGS.get(tag)
    if (!allowed) {
      unwrap(child)
      continue
    }
    for (const attr of Array.from(child.attributes)) {
      const name = attr.name.toLowerCase()
      if (!allowed.includes(name)) {
        child.removeAttribute(attr.name)
        continue
      }
      if (name !== 'href' && name !== 'src') continue
      const url = safeUrl(attr.value)
      if (url === null) child.removeAttribute(attr.name)
      else child.setAttribute(name, url)
    }
    scrub(child)
  }
}

function unwrap(el) {
  scrub(el)
  const parent = el.parentNode
  while (el.firstChild) parent.insertBefore(el.firstChild, el)
  el.remove()
}

function sanitizeInto(host, html) {
  const doc = new DOMParser().parseFromString(String(html ?? ''), 'text/html')
  scrub(doc.body)
  while (doc.body.firstChild) host.appendChild(doc.body.firstChild)
}

function addCopyButtons(root) {
  for (const block of root.querySelectorAll('pre')) {
    if (block.querySelector('.copy-code-btn')) continue
    const codeEl = block.querySelector('code')
    if (!codeEl) continue
    const button = document.createElement('button')
    button.className = 'copy-code-btn'
    button.type = 'button'
    button.innerHTML = '<i data-lucide="copy" class="w-4 h-4"></i>'
    button.onclick = async (e) => {
      e.preventDefault()
      e.stopPropagation()
      try {
        await navigator.clipboard.writeText(codeEl.textContent)
      } catch {
        const textarea = document.createElement('textarea')
        textarea.value = codeEl.textContent
        textarea.style.position = 'fixed'
        textarea.style.opacity = '0'
        document.body.appendChild(textarea)
        textarea.select()
        document.execCommand('copy')
        document.body.removeChild(textarea)
      }
      button.innerHTML = '<i data-lucide="check" class="w-4 h-4"></i>'
      button.classList.add('copied')
      drawIcons(button)
      setTimeout(() => {
        button.innerHTML = '<i data-lucide="copy" class="w-4 h-4"></i>'
        button.classList.remove('copied')
        drawIcons(button)
      }, 2000)
    }
    block.appendChild(button)
  }
}

function decorate(root) {
  for (const img of root.querySelectorAll('img')) {
    img.loading = 'lazy'
    img.style.maxWidth = '100%'
    img.style.borderRadius = '0.5rem'
  }
  for (const a of root.querySelectorAll('a[href]')) {
    if (!/^https?:\/\//i.test(a.getAttribute('href'))) continue
    a.target = '_blank'
    a.rel = 'noopener noreferrer'
  }
  addCopyButtons(root)
  drawIcons(root)
}

function scrubDiagram(node) {
  for (const child of Array.from(node.querySelectorAll('*'))) {
    if (child.tagName.toLowerCase() === 'script') {
      child.remove()
      continue
    }
    for (const attr of Array.from(child.attributes)) {
      const name = attr.name.toLowerCase()
      if (name.startsWith('on')) {
        child.removeAttribute(attr.name)
        continue
      }
      if (name !== 'href' && name !== 'xlink:href') continue
      if (safeUrl(attr.value) === null) child.removeAttribute(attr.name)
    }
  }
}

function runDiagrams(root) {
  if (typeof mermaid === 'undefined') return
  const nodes = Array.from(root.querySelectorAll('.mermaid')).filter((n) => !n.dataset.diagram)
  if (!nodes.length) return
  for (const n of nodes) {
    n.dataset.diagram = '1'
    n.dataset.source = n.textContent
    diagrams.add(n)
  }
  initMermaid()
  Promise.resolve(mermaid.run({ nodes }))
    .then(() => {
      for (const n of nodes) scrubDiagram(n)
    })
    .catch((err) => {
      console.error('mermaid render failed', err)
    })
}

function redrawDiagrams() {
  const mounted = []
  for (const node of Array.from(diagrams)) {
    if (!node.isConnected) {
      diagrams.delete(node)
      continue
    }
    node.removeAttribute('data-processed')
    delete node.dataset.diagram
    node.textContent = node.dataset.source || ''
    mounted.push(node)
  }
  if (!mounted.length) return
  runDiagrams(document.body)
}

window.addEventListener('isane:theme', redrawDiagrams)

export function renderMarkdown(src) {
  initMarked()
  const text = String(src ?? '')
  const host = document.createElement('div')
  const html = typeof marked === 'undefined' ? `<p>${escapeHtml(text)}</p>` : marked.parse(text)
  sanitizeInto(host, html)
  decorate(host)
  requestAnimationFrame(() => runDiagrams(host))
  return host
}

export function renderInto(el, src) {
  if (!el) return null
  const text = String(src ?? '')
  const cached = nodeCache.get(el)
  if (cached && cached.src === text && cached.node.parentNode === el) return cached.node
  const node = renderMarkdown(text)
  el.replaceChildren(node)
  nodeCache.set(el, { src: text, node })
  runDiagrams(node)
  return node
}

export function plainText(src) {
  return String(src ?? '')
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/~~~[\s\S]*?~~~/g, ' ')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/^\s{0,3}#{1,6}\s+/gm, '')
    .replace(/^\s{0,3}>\s?/gm, '')
    .replace(/^\s{0,3}([-*+]|\d+\.)\s+/gm, '')
    .replace(/^\s{0,3}([-*_]\s*){3,}$/gm, ' ')
    .replace(/(\*\*|__)(.*?)\1/g, '$2')
    .replace(/(\*|_)(.*?)\1/g, '$2')
    .replace(/~~(.*?)~~/g, '$1')
    .replace(/<[^>]+>/g, '')
    .replace(/\s+/g, ' ')
    .trim()
}

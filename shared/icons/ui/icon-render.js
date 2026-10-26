const ROOT_ATTRIBUTES = ['viewBox', 'fill', 'stroke', 'stroke-width', 'stroke-linecap', 'stroke-linejoin']

export function parseIconMarkup(source) {
  const svg = String(source || '')
  const openingTag = svg.match(/^\s*<svg\b([^>]*)>/i)
  if (!openingTag) return { markup: svg, attributes: { viewBox: '0 0 24 24', fill: 'none' } }

  const attributes = Object.fromEntries([...openingTag[1].matchAll(/([\w-]+)="([^"]*)"/g)]
    .filter(([, key]) => ROOT_ATTRIBUTES.includes(key))
    .map(([, key, value]) => [key, value]))
  const markup = svg.slice(openingTag[0].length).replace(/<\/svg>\s*$/i, '').trim()
  return { markup, attributes }
}

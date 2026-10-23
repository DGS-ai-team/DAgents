import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import { svgToIcon } from 'morphicons/adapters'

const iconPath = name => new URL(`../../shared/icons/desktop/${name}.svg`, import.meta.url)
const morphable = ['panel-left-open', 'panel-left-close', 'settings', 'terminal', 'browser', 'folder-open']

test('the top-left morph whitelist stays compatible with the pinned Morphicons SVG adapter', () => {
  const parsed = new Map(morphable.map(name => {
    const markup = fs.readFileSync(iconPath(name), 'utf8')
    return [name, svgToIcon(markup)]
  }))
  assert.equal(parsed.size, 6)
  assert.notDeepEqual(parsed.get('panel-left-open'), parsed.get('panel-left-close'), 'the divider positions define distinct endpoints')
  assert.match(fs.readFileSync(iconPath('panel-left-open'), 'utf8'), /d="M6\.5 4\.5v15"/, 'expanded-sidebar action keeps the narrow divider on the left')
  assert.match(fs.readFileSync(iconPath('panel-left-close'), 'utf8'), /d="M9 4\.5v15"/, 'collapsed-sidebar action keeps the wider divider on the left')
  assert.match(fs.readFileSync(iconPath('auto'), 'utf8'), /<g transform=/, 'Auto still has transformed source geometry and stays on its mask fallback')
  assert.equal(morphable.includes('auto'), false)
  assert.throws(() => svgToIcon(fs.readFileSync(iconPath('nav-workgroups'), 'utf8')), /fill-drawn/i)
})

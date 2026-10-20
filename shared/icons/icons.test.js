import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { parseIconMarkup } from './ui/icon-render.js'

const iconsRoot = path.dirname(fileURLToPath(import.meta.url))
const aliases = JSON.parse(fs.readFileSync(path.join(iconsRoot, 'aliases.json'), 'utf8'))

test('every shared icon alias and Desktop manifest target resolves to one canonical SVG', () => {
  for (const [alias, target] of Object.entries(aliases)) {
    assert.ok(fs.existsSync(path.join(iconsRoot, target)), `${alias} -> missing ${target}`)
    assert.ok(target.endsWith('.svg'), `${alias} should resolve to an SVG`)
  }
  const manifest = JSON.parse(fs.readFileSync(path.join(iconsRoot, 'desktop/manifest.json'), 'utf8'))
  assert.ok(manifest.icons.length >= 32)
  assert.equal(new Set(manifest.icons.map(entry => entry.name)).size, manifest.icons.length, 'Desktop manifest semantic names are unique')
  assert.ok(manifest.icons.every(entry => !entry.file.includes('/variants/')), 'production manifest points to runtime canonical files')
  for (const entry of manifest.icons) {
    const svgPath = path.join(iconsRoot, entry.file)
    assert.ok(fs.existsSync(svgPath), `${entry.name} -> missing ${entry.file}`)
    const svg = fs.readFileSync(svgPath, 'utf8')
    assert.doesNotMatch(svg, /<style\b|class="icon"/, `${entry.name} has no reusable global style class`)
    assert.match(svg, /currentColor/, `${entry.name} follows the neutral icon color contract`)
  }
})

test('Node and Manage registries cover their former UiIcon names and retain currentColor geometry', () => {
  const semanticTargets = {
    agent: '../desktop/agent.svg',
    workgroup: '../desktop/nav-workgroups.svg',
    auto: '../desktop/auto.svg',
    skills: '../lucide/puzzle.svg',
    browser: '../desktop/browser.svg',
    attachment: '../desktop/attachment.svg',
    paperclip: '../desktop/attachment.svg',
    file: '../desktop/file.svg',
    send: '../desktop/send.svg',
    wrench: '../desktop/tools.svg',
    tool: '../desktop/tools.svg',
    wecom: './wecom.svg',
  }
  for (const app of ['node', 'manage']) {
    const registryPath = path.join(iconsRoot, `ui/${app}-registry.js`)
    const source = fs.readFileSync(registryPath, 'utf8')
    const names = JSON.parse(source.match(new RegExp(`export const ${app}IconNames = (\\[[^\\n]+\\])`))[1])
    for (const semantic of ['agent', 'workgroup', 'auto', 'skills', 'browser', 'attachment', 'wecom']) {
      assert.ok(names.includes(semantic), `${app} exposes the ${semantic} semantic name`)
    }
    const imports = [...source.matchAll(/import (\w+) from '([^']+)\?raw'/g)]
    const importTargets = new Map(imports.map(([, variable, target]) => [variable, target]))
    for (const [, , target] of imports) {
      const filePath = path.resolve(path.dirname(registryPath), target)
      assert.ok(filePath.startsWith(iconsRoot), `${app} raw SVG stays in shared/icons`)
      const svg = fs.readFileSync(filePath, 'utf8')
      assert.match(svg, /currentColor/)
      assert.match(svg, /<(?:path|circle|rect|line|polyline|polygon)\b/)
    }
    const registryText = source.match(new RegExp(`export const ${app}IconRegistry = Object.freeze\\(\\{([\\s\\S]*?)\\n\\}\\)`))[1]
    const entries = [...registryText.matchAll(/^\s*"([^"]+)":\s*([^,\n]+),?$/gm)]
    const entryMap = new Map(entries.map(([, name, value]) => [name, value.trim()]))
    assert.equal(entries.length, entryMap.size, `${app} registry has no duplicate icon keys`)
    for (const name of names) assert.ok(entryMap.has(name), `${app} registry includes ${name}`)
    for (const [name, target] of Object.entries(semanticTargets)) {
      const variable = entryMap.get(name)
      assert.ok(importTargets.has(variable), `${app} ${name} resolves through an imported canonical asset`)
      assert.equal(importTargets.get(variable), target, `${app} ${name} resolves to its canonical geometry`)
    }
    const usedVariables = new Set([...registryText.matchAll(/body\((icon\d+)\)|(\w+Semantic)/g)].flatMap(([, numbered, semantic]) => [numbered, semantic].filter(Boolean)))
    for (const variable of importTargets.keys()) assert.ok(usedVariables.has(variable), `${app} does not import unused icon asset ${variable}`)
    assert.match(source, /"bot": body\(icon\d+\)/, `${app} retains the technical bot icon name`)
    assert.match(source, /"users": body\(icon\d+\)/, `${app} retains the generic users icon name`)
  }
  const nodeFile = fs.readFileSync(path.join(iconsRoot, 'ui/node-file.svg'), 'utf8')
  const manageFile = fs.readFileSync(path.join(iconsRoot, 'ui/manage-file.svg'), 'utf8')
  assert.match(nodeFile, /fill="currentColor"/)
  assert.match(manageFile, /stroke="currentColor"/)
})

test('settings navigation applies a smaller optical size to the full-coverage puzzle glyph', () => {
  const layout = fs.readFileSync(path.resolve(iconsRoot, '../../node/webui/frontend/src/layouts/SettingsLayout.vue'), 'utf8')
  const styles = fs.readFileSync(path.resolve(iconsRoot, '../../node/webui/frontend/src/styles/layout.css'), 'utf8')
  assert.match(layout, /:size="item\.icon === 'skills' \? 14 : 16"/)
  assert.match(styles, /settings-layout__link-icon\s*\{\s*width:\s*16px;\s*height:\s*16px;/)
  assert.match(styles, /settings-layout__link-icon--skills\s*\{\s*width:\s*14px;\s*height:\s*14px;/)
})

test('approved Desktop mask geometry remains filled or currentColor-driven', () => {
  for (const name of ['agent.svg', 'file.svg', 'nav-workgroups.svg']) {
    const svg = fs.readFileSync(path.join(iconsRoot, 'desktop', name), 'utf8')
    assert.match(svg, /fill="currentColor"/)
  }
  assert.match(fs.readFileSync(path.join(iconsRoot, 'desktop/auto.svg'), 'utf8'), /stroke="currentColor"/)
  assert.match(fs.readFileSync(path.join(iconsRoot, 'desktop/settings.svg'), 'utf8'), /stroke="currentColor"/)
})

test('shared app wrappers only bind the static raw icon registry', () => {
  for (const app of ['Node', 'Manage']) {
    const component = fs.readFileSync(path.join(iconsRoot, `ui/${app}UiIcon.vue`), 'utf8')
    assert.match(component, /resolve(?:Node|Manage)Icon\(props\.name\)/)
    assert.match(component, /v-html="iconMarkup"/)
    assert.doesNotMatch(component, /props\.name\s*\|\|\s*['"]<|name\s*\+\s*['"]<|fetch\(/)
  }
})

test('shared renderer preserves SVG viewBox and hyphenated drawing attributes', () => {
  const parsed = parseIconMarkup('<svg width="24" height="24" viewBox="0 0 1024 1024" fill="currentColor"><path d="M1 1"/></svg>')
  assert.deepEqual(parsed.attributes, { viewBox: '0 0 1024 1024', fill: 'currentColor' })
  assert.equal(parsed.markup, '<path d="M1 1"/>')
  const outlined = parseIconMarkup('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M1 1"/></svg>')
  assert.equal(outlined.attributes['stroke-width'], '2')
  assert.equal(outlined.attributes['stroke-linecap'], 'round')
})

test('Node save and upload icons keep stroke attributes when rendered through UiIcon', () => {
  const source = fs.readFileSync(path.join(iconsRoot, 'ui/node-registry.js'), 'utf8')
  assert.match(source, /"save": saveSemantic/)
  assert.match(source, /"upload": uploadSemantic/)
  for (const file of ['legacy-prototype/save.svg', 'desktop/upload.svg']) {
    const parsed = parseIconMarkup(fs.readFileSync(path.join(iconsRoot, file), 'utf8'))
    assert.equal(parsed.attributes.stroke, 'currentColor', `${file} preserves its stroke color`)
    assert.equal(parsed.attributes['stroke-width'], '2', `${file} preserves its stroke width`)
    assert.match(parsed.markup, /<(?:path|polyline|line)\b/)
  }
})

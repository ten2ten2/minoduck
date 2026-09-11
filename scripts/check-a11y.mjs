import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'

const files = execFileSync(
  'git',
  [
    'ls-files',
    '--cached',
    '--others',
    '--exclude-standard',
    '--',
    'apps/**/*.vue',
    'packages/**/*.vue',
  ],
  { encoding: 'utf8' },
)
  .trim()
  .split('\n')
  .filter(Boolean)

const failures = []
const report = (file, source, index, message) => {
  const line = source.slice(0, index).split('\n').length
  failures.push(`${file}:${line}: ${message}`)
}

for (const file of files) {
  const source = readFileSync(file, 'utf8')
  const templateMatch = source.match(/<template(?:\s[^>]*)?>([\s\S]*?)<\/template>/)
  const template = templateMatch?.[1] ?? ''
  const templateOffset =
    templateMatch?.index === undefined
      ? 0
      : templateMatch.index + templateMatch[0].indexOf(template)
  const reportTemplate = (index, message) => report(file, source, templateOffset + index, message)

  for (const match of template.matchAll(/<img\b[^>]*>/g)) {
    if (!/\s(?::)?alt\s*=/.test(match[0])) reportTemplate(match.index, 'image needs alt text')
  }
  for (const match of template.matchAll(/<iframe\b[^>]*>/g)) {
    if (!/\stitle\s*=/.test(match[0])) reportTemplate(match.index, 'iframe needs a title')
  }
  for (const match of template.matchAll(/<button\b([^>]*)>([\s\S]*?)<\/button>/g)) {
    const attributes = match[1]
    const content = match[2].replace(/<[^>]+>/g, '').trim()
    if (!/\s(?::)?aria-label\s*=|\s(?::)?aria-labelledby\s*=/.test(attributes) && !content) {
      reportTemplate(match.index, 'button needs an accessible name')
    }
  }
  for (const match of template.matchAll(/<button\b([^>]*)\/>/g)) {
    if (!/\s(?::)?aria-label\s*=|\s(?::)?aria-labelledby\s*=/.test(match[1])) {
      reportTemplate(match.index, 'button needs an accessible name')
    }
  }
  for (const match of template.matchAll(/<(input|select|textarea)\b([^>]*)>/g)) {
    const attributes = match[2]
    if (/\stype\s*=\s*["']hidden["']/.test(attributes)) continue
    const before = template.slice(0, match.index)
    const nestedInLabel = before.lastIndexOf('<label') > before.lastIndexOf('</label>')
    const id = attributes.match(/\sid\s*=\s*["']([^"']+)["']/)?.[1]
    const labelledByID = id && new RegExp(`<label\\b[^>]*for=["']${id}["']`).test(template)
    const explicitName = /\s(?::)?aria-label\s*=|\s(?::)?aria-labelledby\s*=/.test(attributes)
    if (!nestedInLabel && !labelledByID && !explicitName) {
      reportTemplate(match.index, `${match[1]} needs a label`)
    }
  }
  for (const match of template.matchAll(
    /<(?:a|NuxtLink)\b[^>]*target\s*=\s*["']_blank["'][^>]*>/g,
  )) {
    if (!/\srel\s*=\s*["'][^"']*noopener[^"']*noreferrer[^"']*["']/.test(match[0])) {
      reportTemplate(match.index, 'new-window link needs rel="noopener noreferrer"')
    }
  }
  for (const match of template.matchAll(
    /<(div|span|section|article)\b[^>]*@click\s*=\s*["'][^"']+["'][^>]*>/g,
  )) {
    const tag = match[0]
    if (
      !/\srole\s*=\s*["']button["']/.test(tag) ||
      !/\stabindex\s*=/.test(tag) ||
      !/@key(?:down|up)(?:\.(?:enter|space))?\s*=/.test(tag)
    ) {
      reportTemplate(
        match.index,
        'clickable non-button needs button role, tabindex and keyboard handler',
      )
    }
  }
}

if (failures.length) {
  process.stderr.write(`${failures.join('\n')}\n`)
  process.exitCode = 1
} else {
  process.stdout.write(`Accessibility checks passed (${files.length} Vue files).\n`)
}

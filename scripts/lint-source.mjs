import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'

const files = execFileSync(
  'git',
  ['ls-files', '--cached', '--others', '--exclude-standard', '--', '*.ts', '*.vue', '*.mjs'],
  {
    encoding: 'utf8',
  },
)
  .trim()
  .split('\n')
  .filter((file) => /^(apps|packages|infra)\//.test(file))

const rules = /** @type {const} */ ([
  ['explicit any annotation', /:\s*any\b/g],
  ['explicit any assertion', /\bas\s+any\b/g],
  ['explicit any generic', /(?:<|,\s*)any(?:\s*[,>])/g],
  ['TypeScript checking disabled', /@ts-(?:ignore|nocheck)\b/g],
  ['debug logging', /\bconsole\.(?:log|debug)\s*\(/g],
  ['empty catch block', /catch\s*(?:\([^)]*\))?\s*\{\s*\}/g],
])

const failures = []
for (const file of files) {
  const source = readFileSync(file, 'utf8')
  for (const [message, pattern] of rules) {
    for (const match of source.matchAll(pattern)) {
      const line = source.slice(0, match.index).split('\n').length
      failures.push(`${file}:${line}: ${message}`)
    }
  }
}

if (failures.length) {
  process.stderr.write(`${failures.join('\n')}\n`)
  process.exitCode = 1
} else {
  process.stdout.write(`Source lint passed (${files.length} files).\n`)
}

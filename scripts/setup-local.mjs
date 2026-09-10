import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { randomBytes } from 'node:crypto'
const paths = ['services/backend', 'apps/console', 'apps/site']
const present = paths.filter((path) => existsSync(`${path}/.env`))
if (present.length) {
  console.error(
    'Existing .env files found. Keep them and copy any missing .env.example manually; use the same BFF token on API and console.',
  )
  process.exitCode = 1
} else {
  const token = randomBytes(32).toString('hex')
  for (const path of paths) {
    const source = readFileSync(`${path}/.env.example`, 'utf8')
      .replace('local-development-service-token-change-for-production', token)
      .replace(
        'PROVIDER_ENCRYPTION_MASTER_KEY=\n',
        `PROVIDER_ENCRYPTION_MASTER_KEY=${randomBytes(32).toString('base64')}\n`,
      )
    writeFileSync(`${path}/.env`, source, { mode: 0o600, flag: 'wx' })
  }
  console.log(
    'Local configuration created. Run docker compose up --build, then pnpm dev in another terminal.',
  )
}

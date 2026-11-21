// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

import { execFileSync, spawn, type ChildProcess } from 'node:child_process'
import {
  closeSync,
  existsSync,
  mkdtempSync,
  openSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const web = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repo = resolve(web, '..')

function exited(child: ChildProcess, ms: number): Promise<boolean> {
  if (child.exitCode !== null || child.signalCode !== null) return Promise.resolve(true)
  return new Promise((done) => {
    const timer = setTimeout(() => done(false), ms)
    child.once('exit', () => {
      clearTimeout(timer)
      done(true)
    })
  })
}

async function waitForFile(path: string, child: ChildProcess, ms: number): Promise<void> {
  const deadline = Date.now() + ms
  while (!existsSync(path)) {
    if (child.exitCode !== null) throw new Error(`harness exited with code ${child.exitCode}`)
    if (Date.now() > deadline) throw new Error('harness did not start in time')
    await new Promise((r) => setTimeout(r, 250))
  }
}

export default async function globalSetup() {
  if (!process.env.NEBLA_E2E_SKIP_BUILD)
    execFileSync('pnpm', ['build'], { cwd: web, stdio: 'inherit' })

  const dir = mkdtempSync(join(tmpdir(), 'nebla-e2e-'))
  const out = join(dir, 'out.json')
  const stop = join(dir, 'stop')
  const logPath = join(dir, 'harness.log')
  const log = openSync(logPath, 'w')
  const child = spawn(
    'go',
    [
      'test',
      '-count=1',
      '-v',
      '-tags',
      'uiharness',
      '-run',
      'TestUIHarness',
      './internal/uiharness',
      '-timeout',
      '0',
    ],
    {
      cwd: repo,
      env: { ...process.env, NEBLA_UI_HARNESS_OUT: out, NEBLA_UI_HARNESS_STOP: stop },
      stdio: ['ignore', log, log],
      detached: true,
    },
  )
  // The stop file is the harness's shutdown signal, so write it however this process ends.
  const signalStop = () => {
    if (existsSync(dir)) writeFileSync(stop, '')
  }
  process.once('exit', signalStop)

  let tornDown = false
  const teardown = async () => {
    if (tornDown) return
    tornDown = true
    signalStop()
    if (!(await exited(child, 60_000)) && child.pid) process.kill(-child.pid, 'SIGTERM')
    await exited(child, 10_000)
    closeSync(log)
    if (child.exitCode !== 0) console.error(readFileSync(logPath, 'utf8').slice(-4000))
    rmSync(dir, { recursive: true, force: true })
  }

  try {
    await waitForFile(out, child, 300_000)
  } catch (e) {
    await teardown()
    throw e
  }
  const harness = JSON.parse(readFileSync(out, 'utf8')) as { url: string }
  process.env.NEBLA_E2E_BASE_URL = harness.url
  process.env.NEBLA_E2E_HARNESS = JSON.stringify(harness)
  process.env.NEBLA_E2E_ADMIN_STATE = join(dir, 'admin.json')
  return teardown
}

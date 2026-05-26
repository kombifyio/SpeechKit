#!/usr/bin/env node
// Bundle-size budget gate for the SpeechKit Wails frontend.
//
// This script walks the built dist tree (default
// ../../internal/frontendassets/dist relative to frontend/app),
// computes total + per-category byte counts, and compares them
// against budgets declared in frontend/app/bundle-budgets.json.
//
// Exit codes:
//   0 — all budgets honoured (or all budgets within HEADROOM_PCT)
//   1 — at least one budget exceeded
//   2 — script-level error (missing dist, bad JSON, etc.)
//
// The categories model the v0.40 Track A code-split target without
// requiring the multi-chunk Vite config to be in place yet — the
// initial run uses "always-loaded" (the chunks needed for Dictation-
// only startup) vs "voiceagent-only" (the heavy LiveKit + VA
// visualizer artefacts that should NOT ship when the mode is off).
//
// When M4 lands per-mode chunks, this script's category-matching
// patterns get tightened in bundle-budgets.json without code changes.

import { readFileSync, readdirSync, statSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const HERE = fileURLToPath(new URL('.', import.meta.url))
const FRONTEND_ROOT = path.resolve(HERE, '..')
const REPO_ROOT = path.resolve(FRONTEND_ROOT, '..', '..')
const DEFAULT_DIST = path.resolve(REPO_ROOT, 'internal', 'frontendassets', 'dist')
const BUDGETS_FILE = path.resolve(FRONTEND_ROOT, 'bundle-budgets.json')

const distDir = process.argv[2] || DEFAULT_DIST

function fail(msg, code = 2) {
  console.error(`check-bundle-budgets: ${msg}`)
  process.exit(code)
}

function walk(dir) {
  let entries
  try {
    entries = readdirSync(dir, { withFileTypes: true })
  } catch (err) {
    fail(`cannot read dist dir ${dir}: ${err.message}`)
  }
  const out = []
  for (const entry of entries) {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      out.push(...walk(full))
    } else if (entry.isFile()) {
      out.push(full)
    }
  }
  return out
}

function loadBudgets() {
  let raw
  try {
    raw = readFileSync(BUDGETS_FILE, 'utf8')
  } catch (err) {
    fail(`cannot read budgets file ${BUDGETS_FILE}: ${err.message}`)
  }
  try {
    return JSON.parse(raw)
  } catch (err) {
    fail(`cannot parse budgets JSON: ${err.message}`)
  }
}

function relPathFromDist(file) {
  return path.relative(distDir, file).replace(/\\/g, '/')
}

function categorise(rel, categories) {
  const hits = []
  for (const [name, rules] of Object.entries(categories)) {
    const include = rules.include ?? []
    const exclude = rules.exclude ?? []
    const includes = include.some((p) => new RegExp(p).test(rel))
    if (!includes) continue
    const excluded = exclude.some((p) => new RegExp(p).test(rel))
    if (excluded) continue
    hits.push(name)
  }
  return hits
}

function formatBytes(n) {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(2)} MB`
}

const budgets = loadBudgets()
const headroomPct = Number(budgets.headroom_pct ?? 0)
const categories = budgets.categories ?? {}

try {
  statSync(distDir)
} catch {
  fail(`dist directory not found at ${distDir} — run \`npm run build\` first`)
}

const allFiles = walk(distDir).filter((f) => /\.(js|css)$/i.test(f))
if (allFiles.length === 0) {
  fail(`no JS/CSS assets found under ${distDir}`)
}

const totals = {}
for (const name of Object.keys(categories)) totals[name] = { bytes: 0, files: [] }

let totalBytes = 0
for (const file of allFiles) {
  const size = statSync(file).size
  totalBytes += size
  const rel = relPathFromDist(file)
  for (const cat of categorise(rel, categories)) {
    totals[cat].bytes += size
    totals[cat].files.push({ rel, size })
  }
}

const lines = [
  '',
  `Bundle-size report — dist=${path.relative(REPO_ROOT, distDir)}`,
  `Total: ${formatBytes(totalBytes)} (${allFiles.length} JS/CSS files)`,
  '',
]

let violated = false
for (const [name, info] of Object.entries(totals)) {
  const budget = (categories[name] && categories[name].budget_bytes) || 0
  const headroom = Math.ceil(budget * (1 + headroomPct / 100))
  const status = info.bytes <= headroom ? 'ok' : 'OVER'
  if (status === 'OVER') violated = true
  lines.push(
    `  ${name.padEnd(28)} ${formatBytes(info.bytes).padStart(10)} / ${formatBytes(
      budget,
    ).padStart(10)} (headroom ${formatBytes(headroom)}) — ${status}`,
  )
  if (status === 'OVER') {
    for (const f of info.files.sort((a, b) => b.size - a.size).slice(0, 5)) {
      lines.push(`    top contributor: ${f.rel}  ${formatBytes(f.size)}`)
    }
  }
}

console.log(lines.join('\n'))

if (violated) {
  console.error(
    '\nBundle-size budget exceeded. Either reduce the bundle (preferred) or, if the growth is legitimate, raise the budget in frontend/app/bundle-budgets.json and document the rationale in the PR.',
  )
  process.exit(1)
}

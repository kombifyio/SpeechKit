#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const root = process.argv[2] ? path.resolve(process.argv[2]) : process.cwd();
const ignoredDirs = new Set(['.git', 'node_modules', 'dist', 'build', 'coverage', '.public-export']);
const markdownLinkPattern = /!?\[[^\]]*\]\(([^)]+)\)/g;
const uriSchemePattern = /^[a-zA-Z][a-zA-Z0-9+.-]*:/;
const failures = [];

function walk(dir) {
  const entries = fs.readdirSync(dir, { withFileTypes: true });
  for (const entry of entries) {
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!ignoredDirs.has(entry.name)) {
        walk(fullPath);
      }
      continue;
    }
    if (entry.isFile() && entry.name.toLowerCase().endsWith('.md')) {
      checkMarkdownFile(fullPath);
    }
  }
}

function checkMarkdownFile(filePath) {
  const content = fs.readFileSync(filePath, 'utf8');
  const lines = content.split(/\r?\n/);
  for (let lineIndex = 0; lineIndex < lines.length; lineIndex += 1) {
    const line = lines[lineIndex];
    markdownLinkPattern.lastIndex = 0;
    for (const match of line.matchAll(markdownLinkPattern)) {
      const rawTarget = match[1].trim();
      const localTarget = normalizeLocalTarget(rawTarget);
      if (!localTarget) {
        continue;
      }
      const resolved = path.resolve(path.dirname(filePath), localTarget);
      if (!fs.existsSync(resolved)) {
        failures.push(`${path.relative(root, filePath)}:${lineIndex + 1}: missing ${rawTarget}`);
      }
    }
  }
}

function normalizeLocalTarget(rawTarget) {
  if (!rawTarget || rawTarget.startsWith('#') || uriSchemePattern.test(rawTarget)) {
    return '';
  }
  const withoutTitle = rawTarget.match(/^([^"']+?)(?:\s+["'][^"']*["'])?$/)?.[1] ?? rawTarget;
  const withoutAnchor = withoutTitle.split('#')[0].split('?')[0].trim();
  if (!withoutAnchor) {
    return '';
  }
  try {
    return decodeURIComponent(withoutAnchor);
  } catch {
    return withoutAnchor;
  }
}

walk(root);

if (failures.length > 0) {
  console.error('Markdown local link check failed:');
  for (const failure of failures) {
    console.error(`  ${failure}`);
  }
  process.exit(1);
}

console.log('Markdown local link check passed.');

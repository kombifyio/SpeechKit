#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="${1:-$(dirname "$(dirname "$SCRIPT_DIR")")}"

patterns=(
  "github.com/kombifyio/SpeechKit"
  "https://github.com/kombifyio/SpeechKit"
  "kombination-personal"
  "Private --"
)

search_roots=(
  ".github"
  "README.md"
  "CHANGELOG.md"
  "CONTRIBUTING.md"
  "CODE_OF_CONDUCT.md"
  "SECURITY.md"
  "SUPPORT.md"
  "config.example.toml"
  "docs"
  "frontend/app/README.md"
  "frontend/app/src"
  "installer"
  "scripts"
)

cd "$PROJECT_DIR"

hits=()
for root in "${search_roots[@]}"; do
  [ -e "$root" ] || continue
  while IFS= read -r file; do
    case "$file" in
      docs/plans/*|scripts/public/check-public-surface.sh|scripts/public/check-public-surface.ps1) continue ;;
    esac
    for pattern in "${patterns[@]}"; do
      while IFS= read -r line; do
        hits+=("$line")
      done < <(grep -InF "$pattern" "$file" 2>/dev/null || true)
    done
  done < <(find "$root" -type f 2>/dev/null)
done

while IFS= read -r path; do
  [ -n "$path" ] || continue
  case "$path" in
    dist/windows/*|installer/*) ;;
    *) hits+=("unexpected exe outside release surface: $path") ;;
  esac
done < <(find . -type f -name '*.exe' | sed 's#^\./##')

for internal_file in AGENTS.md CLAUDE.md; do
  if [ -e "$internal_file" ]; then
    hits+=("internal-only file present: $internal_file")
  fi
done

if [ "${#hits[@]}" -gt 0 ]; then
  echo "Public surface check failed:"
  for hit in "${hits[@]}"; do
    echo "  $hit"
  done
  exit 1
fi

echo "Public surface check passed."

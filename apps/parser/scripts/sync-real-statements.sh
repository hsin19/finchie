#!/usr/bin/env bash
# Download the original of every statement the deployment parsed into a folder of real statements
# (ADR 0006), so that `task parser:test-real` checks this checkout's parsers against all of them.
# Goes through the CLI with its settings (apps/cli/README.md). Only `parsed` documents: they reconciled
# in production, so each must still reconcile here; the other states fail the test by definition.
# A document whose bytes are already in the folder, under any name, is not fetched again, and nothing
# already there is removed or renamed.
#
#   sync-real-statements.sh <dir>
set -euo pipefail

dir=${1:?usage: sync-real-statements.sh <dir>}
cli=$(cd "$(dirname "$0")/../../cli" && pwd)

finchie() { uv run --quiet --project "$cli" finchie "$@"; }
sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }

umask 077
mkdir -p "$dir"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

echo "Documents in the backend the CLI is set up for, by state:"
finchie list-documents --limit 1 | jq -r '.byState[] | "  \(.state): \(.count)"'
finchie list-documents --state parsed --all | jq -r '[.id, .sha256, .filename] | @tsv' >"$work/documents"

: >"$work/known"
for file in "$dir"/*.eml; do
  [ -e "$file" ] && sha256 "$file" >>"$work/known"
done

total=$(wc -l <"$work/documents" | tr -d ' ')
fetched=0
while IFS=$'\t' read -r id sha name; do
  grep -qx "$sha" "$work/known" && continue
  name=${name//\//_}
  case $name in *.eml) ;; *) name="$name.eml" ;; esac
  # The id keeps statements whose e-mails share a subject apart, and finds the document in the web app.
  target="$dir/$id $name"
  finchie get-document-content "$id" >"$work/content"
  if [ "$(sha256 "$work/content")" != "$sha" ]; then
    echo "document $id: the bytes received do not match its sha256; nothing written for it" >&2
    exit 1
  fi
  mv "$work/content" "$target"
  echo "$sha" >>"$work/known"
  fetched=$((fetched + 1))
  echo "  + $id $name"
done <"$work/documents"

echo "$total parsed in the deployment: fetched $fetched, $((total - fetched)) already in $dir"
echo "Next: task parser:test-real"

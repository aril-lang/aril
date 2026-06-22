#!/usr/bin/env bash
#
# Highlight-regression test for the Aril Vim syntax file.
#
# Loads syntax/aril.vim against test/highlight_fixture.aril in a headless
# Vim and asserts (via synID) that each representative token highlights as
# the right group — guarding against the last-defined-wins priority bugs
# that are invisible to a "loads without error" smoke test.
#
# Exit: 0 = all checks pass (or vim absent → skipped), 1 = a check failed.

set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
vimdir="$(cd "$here/.." && pwd)"          # editors/vim (holds syntax/, ftdetect/)
fixture="$here/highlight_fixture.aril"
probe="$here/highlight_test.vim"

if ! command -v vim >/dev/null 2>&1; then
  echo "SKIP highlight test: vim not installed"
  exit 0
fi

out="$(mktemp)"
trap 'rm -f "$out"' EXIT
export ARIL_HL_OUT="$out"

set +e
vim -Nu NONE -Es \
  --cmd "set runtimepath^=$vimdir" \
  --cmd 'syntax on' \
  -c "edit $fixture" \
  -c "source $probe" </dev/null >/dev/null 2>&1
code=$?
set -e

cat "$out"
exit "$code"

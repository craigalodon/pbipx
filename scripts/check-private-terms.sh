#!/usr/bin/env bash
# Refuse a commit whose staged paths, staged content or message contain a term
# from the maintainer's private list (ADR-0002). The list names the
# organizations, systems and people of real deployments, so it lives outside
# the repository, one case-insensitive term per line, # for comments:
#
#   ${PBIPX_PRIVATE_TERMS:-$HOME/.config/pbipx/private-terms.txt}
#
# Fails closed: with the hooks installed and no list, nothing is committed.
# Reports which file matched, never the term.
#
#   check-private-terms.sh --staged          staged paths and content
#   check-private-terms.sh --message <file>  a commit message
#   check-private-terms.sh --tracked         every tracked file (before publishing)
set -euo pipefail

list="${PBIPX_PRIVATE_TERMS:-$HOME/.config/pbipx/private-terms.txt}"
if [ ! -r "$list" ]; then
    echo "private-terms: no list at $list; see CONTRIBUTING.md" >&2
    exit 1
fi

patterns="$(mktemp)"
trap 'rm -f "$patterns"' EXIT
grep -v -e '^[[:space:]]*#' -e '^[[:space:]]*$' "$list" >"$patterns" || true
if [ ! -s "$patterns" ]; then
    echo "private-terms: the list at $list is empty" >&2
    exit 1
fi

matches() { grep -qiF -f "$patterns"; }
found=0

case "${1:-}" in
--message)
    if matches <"${2:?a message file}"; then
        echo "private-terms: the commit message contains a private term" >&2
        found=1
    fi
    ;;
--staged | --tracked)
    if [ "$1" = --staged ]; then
        files() { git diff --cached --name-only -z --diff-filter=ACMR; }
        blob() { git cat-file blob ":$1"; }
    else
        files() { git ls-files -z; }
        blob() { cat -- "$1"; }
    fi
    while IFS= read -r -d '' f; do
        if printf '%s\n' "$f" | matches; then
            # The path itself holds the term, so it is not printed.
            echo "private-terms: a path contains a private term" >&2
            found=1
        elif blob "$f" | matches; then
            echo "private-terms: $f contains a private term" >&2
            found=1
        fi
    done < <(files)
    ;;
*)
    echo "usage: $0 --staged | --tracked | --message <file>" >&2
    exit 2
    ;;
esac

exit "$found"

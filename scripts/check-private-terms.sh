#!/usr/bin/env bash
# Refuse a commit whose staged paths, staged content or message contain a term
# from the maintainer's private list (ADR-0002). The list names the
# organizations, systems and people of real deployments, so it lives outside
# the repository, one case-insensitive term per line, # for comments:
#
#   ${PBIPX_PRIVATE_TERMS:-$HOME/.config/pbipx/private-terms.txt}
#
# Fails closed: with the hooks installed and no list, nothing is committed; a
# read that fails is a failure, never "no match". Reports which file or object
# matched, never the term.
#
#   check-private-terms.sh --staged          staged paths and content
#   check-private-terms.sh --message <file>  a commit message
#   check-private-terms.sh --tracked         every tracked file (before publishing)
#   check-private-terms.sh --history         everything reachable from any ref
#   check-private-terms.sh --github [owner/repo]  text on GitHub, read with gh
#
# --history is for the phase 7 check. Run it in a clone made with
# `git clone --mirror`, so refs/pull/* is included. It checks every blob, every
# path, every commit and tag message, every author, committer and tagger
# identity, and every ref name. One pass over the objects keeps it linear, and
# nothing is held in memory but one line or one object at a time. grep sees
# bytes as git stores them: it cannot see inside compressed content (zip, xlsx,
# pbix, gzip), so a term in an archive is not found.
#
# --github needs gh (logged in) and jq. It reads pull request and issue titles
# and bodies, issue and review comments, reviews, commit comments and release
# notes. An API error, including a token without the permission, is a failure.
# It does not see discussions, wikis, or edit history.
set -euo pipefail

usage() {
    echo "usage: $0 --staged | --tracked | --message <file> | --history | --github [owner/repo]" >&2
    echo "  --history cannot see inside compressed content" >&2
    exit 2
}

case "${1:-}" in
--staged | --tracked | --history | --message | --github) ;;
*) usage ;;
esac

list="${PBIPX_PRIVATE_TERMS:-$HOME/.config/pbipx/private-terms.txt}"
if [ ! -r "$list" ]; then
    echo "private-terms: no list at $list; see CONTRIBUTING.md" >&2
    exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
patterns="$work/patterns"
grep -v -e '^[[:space:]]*#' -e '^[[:space:]]*$' "$list" >"$patterns" || true
if [ ! -s "$patterns" ]; then
    echo "private-terms: the list at $list is empty" >&2
    exit 1
fi

found=0

# scan <command...>: run the command and look for a term in its output.
# Returns 0 for a match, 1 for none, 2 if the command or grep failed.
#
# grep reads all of its input, with no -q, so the producer is never cut off by
# SIGPIPE; a producer that fails is then a real failure, found in PIPESTATUS.
# Call it as `scan ... || rc=$?`, so that errexit does not end the script
# before PIPESTATUS is read.
scan() {
    local -a st
    "$@" | grep -aiF -f "$patterns" >/dev/null
    st=("${PIPESTATUS[@]}")
    [ "${st[0]}" -eq 0 ] || return 2
    case "${st[1]}" in
    0) return 0 ;;
    1) return 1 ;;
    *) return 2 ;;
    esac
}

# check <what> <command...>: scan, and report <what> on a match or a failure.
check() {
    local what=$1 rc=0
    shift
    scan "$@" || rc=$?
    case "$rc" in
    0)
        echo "private-terms: $what contains a private term" >&2
        found=1
        ;;
    1) ;;
    *)
        echo "private-terms: could not read $what" >&2
        found=1
        ;;
    esac
}

# fail <message>: a failure of the check itself. Never "clean".
fail() {
    echo "private-terms: $1" >&2
    exit 1
}

# lines_matching <file> <out>: write the numbers of the lines of <file> that
# hold a term to <out>, without printing any text. Returns 2 on an error.
lines_matching() {
    local -a st
    grep -n -aiF -f "$patterns" "$1" | cut -d: -f1 >"$2"
    st=("${PIPESTATUS[@]}")
    [ "${st[0]}" -le 1 ] && [ "${st[1]}" -eq 0 ]
}

staged_or_tracked() {
    if [ "$1" = --staged ]; then
        files() { git diff --cached --name-only -z --diff-filter=ACMR; }
        blob() { git cat-file blob ":$1"; }
    else
        files() { git ls-files -z; }
        blob() {
            if [ -L "$1" ]; then
                readlink -- "$1"
            elif [ -d "$1" ]; then
                :
            else
                cat -- "$1"
            fi
        }
    fi
    # The list goes to a file, so a failing git is not an empty list.
    files >"$work/files" || fail "could not list files"
    while IFS= read -r -d '' f; do
        local rc=0
        scan printf '%s\n' "$f" || rc=$?
        if [ "$rc" -eq 0 ]; then
            # The path itself holds the term, so it is not printed.
            echo "private-terms: a path contains a private term" >&2
            found=1
        elif [ "$rc" -ne 1 ]; then
            echo "private-terms: could not read a path" >&2
            found=1
        else
            check "$f" blob "$f"
        fi
    done <"$work/files"
}

history() {
    local objs="$work/objs" typed="$work/typed" paths="$work/paths"
    local pathhits="$work/pathhits" oid type rc

    git rev-list --objects --all >"$objs" || fail "could not list objects"
    # "<type> <oid> <path>", one line per line of $objs, in order.
    git cat-file --batch-check='%(objecttype) %(objectname) %(rest)' \
        <"$objs" >"$typed" || fail "could not read objects"
    if awk '$2 == "missing" { f = 1 } END { exit !f }' "$typed"; then
        fail "an object is missing (a shallow or partial clone?)"
    fi

    # Paths, of every blob and tree.
    cut -d' ' -f3- "$typed" >"$paths"
    lines_matching "$paths" "$pathhits" || fail "could not search paths"
    awk 'FILENAME == ARGV[1] { h[$1] = 1; next } FNR in h { print $1, $2 }' \
        "$pathhits" "$typed" |
        while read -r type oid; do
            echo "private-terms: path of $type $oid contains a private term" >&2
        done
    if [ -s "$pathhits" ]; then found=1; fi

    # Blobs. One pass over all content finds out whether any blob matches; only
    # then is each blob read on its own, to say which.
    awk '$1 == "blob" { print $2 }' "$typed" | sort -u >"$work/blobs"
    rc=0
    scan git cat-file --batch <"$work/blobs" || rc=$?
    : >"$work/blobhits"
    case "$rc" in
    0)
        while read -r oid; do
            rc=0
            scan git cat-file blob "$oid" || rc=$?
            case "$rc" in
            0) echo "$oid" >>"$work/blobhits" ;;
            1) ;;
            *) fail "could not read blob $oid" ;;
            esac
        done <"$work/blobs"
        ;;
    1) ;;
    *) fail "could not read blobs" ;;
    esac
    if [ -s "$work/blobhits" ]; then
        found=1
        # Name a path for each, unless that path holds a term itself.
        awk '
            FILENAME == ARGV[1] { want[$1] = 1; next }
            FILENAME == ARGV[2] { bad[$1] = 1; next }
            $1 == "blob" && ($2 in want) && !(FNR in bad) && !($2 in seen) {
                p = $0
                sub(/^[^ ]+ [^ ]+ ?/, "", p)
                if (p != "") seen[$2] = p
            }
            END {
                for (o in want) {
                    if (o in seen) printf "private-terms: blob %s (path %s) contains a private term\n", o, seen[o]
                    else printf "private-terms: blob %s contains a private term\n", o
                }
            }' "$work/blobhits" "$pathhits" "$typed" >&2
    fi

    # Commits and tags: identities and messages.
    awk '$1 == "commit" || $1 == "tag" { print $2 " " $1 }' "$typed" >"$work/cobjs"
    cut -d' ' -f1 "$work/cobjs" >"$work/coids"
    rc=0
    scan git cat-file --batch <"$work/coids" || rc=$?
    case "$rc" in
    0)
        while read -r oid type; do
            check "identity of $type $oid" git_headers "$type" "$oid"
            check "message of $type $oid" git_message "$type" "$oid"
        done <"$work/cobjs"
        ;;
    1) ;;
    *) fail "could not read commits and tags" ;;
    esac

    # Ref names, including refs/pull/*.
    git for-each-ref --format='%(objectname) %(refname)' >"$work/refs" ||
        fail "could not list refs"
    cut -d' ' -f2- "$work/refs" >"$work/refnames"
    lines_matching "$work/refnames" "$work/refhits" || fail "could not search refs"
    awk 'FILENAME == ARGV[1] { h[$1] = 1; next } FNR in h { print $1 }' \
        "$work/refhits" "$work/refs" |
        while read -r oid; do
            echo "private-terms: a ref pointing at $oid contains a private term" >&2
        done
    if [ -s "$work/refhits" ]; then found=1; fi
}

# The header lines of a commit or tag (identities, tag name), without the
# object ids, then its message.
git_headers() {
    git cat-file "$1" "$2" | sed -n '1,/^$/{/^\(tree\|parent\|object\) /d;p;}'
}
git_message() { git cat-file "$1" "$2" | sed '1,/^$/d'; }

github() {
    command -v gh >/dev/null || fail "gh is not installed"
    command -v jq >/dev/null || fail "jq is not installed"
    local repo=${1:-}
    if [ -z "$repo" ]; then
        repo="$(gh repo view --json nameWithOwner --jq .nameWithOwner)" ||
            fail "could not tell which repository to read"
    fi
    case "$repo" in
    */*/* | */ | /* | *[!A-Za-z0-9._/-]*) usage ;;
    */*) ;;
    *) usage ;;
    esac

    # fetch <endpoint> <file>: an API error is a failure, not an empty page.
    fetch() {
        gh api --paginate "$1" >"$2" 2>/dev/null || {
            echo "private-terms: the GitHub API refused a request for $3" >&2
            found=1
            return 1
        }
    }
    # records <file> <jq program>: scan each {label, text} the program emits.
    records() {
        jq -c "$2" "$1" >"$work/recs" || {
            echo "private-terms: unexpected GitHub response" >&2
            found=1
            return 1
        }
        local rec label
        while IFS= read -r rec; do
            label="$(jq -r .label <<<"$rec")"
            check "$label" jq -r .text <<<"$rec"
        done <"$work/recs"
    }
    local text='([.title, .body, .name, .tag_name] | map(select(. != null)) | join("\n"))'
    local num='(split("/") | last)'
    local raw="$work/raw"

    if fetch "repos/$repo/issues?state=all&per_page=100" "$work/issues.json" "issues and pull requests"; then
        records "$work/issues.json" ".[] | {label: ((if .pull_request then \"pull request #\" else \"issue #\" end) + (.number | tostring)), text: $text}"
        jq -r '.[] | select(.pull_request) | .number' "$work/issues.json" >"$work/prs" ||
            fail "unexpected GitHub response"
        local n
        while read -r n; do
            if fetch "repos/$repo/pulls/$n/reviews?per_page=100" "$raw" "reviews"; then
                records "$raw" ".[] | {label: (\"review \" + (.id | tostring) + \" on #$n\"), text: $text}"
            fi
        done <"$work/prs"
    fi
    if fetch "repos/$repo/issues/comments?per_page=100" "$raw" "issue comments"; then
        records "$raw" ".[] | {label: (\"issue comment \" + (.id | tostring) + \" on #\" + (.issue_url | $num)), text: $text}"
    fi
    if fetch "repos/$repo/pulls/comments?per_page=100" "$raw" "review comments"; then
        records "$raw" ".[] | {label: (\"review comment \" + (.id | tostring) + \" on #\" + (.pull_request_url | $num)), text: $text}"
    fi
    if fetch "repos/$repo/comments?per_page=100" "$raw" "commit comments"; then
        records "$raw" ".[] | {label: (\"commit comment \" + (.id | tostring)), text: $text}"
    fi
    if fetch "repos/$repo/releases?per_page=100" "$raw" "releases"; then
        records "$raw" ".[] | {label: (\"release \" + (.id | tostring)), text: $text}"
    fi
}

case "$1" in
--message)
    check "the commit message" cat -- "${2:?a message file}"
    ;;
--staged | --tracked)
    staged_or_tracked "$1"
    ;;
--history)
    history
    ;;
--github)
    github "${2:-}"
    ;;
esac

exit "$found"

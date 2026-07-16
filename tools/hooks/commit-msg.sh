#!/bin/sh
# commit-msg hook: commit messages are a single Conventional Commits subject.
# Called by lefthook with the path to the commit message file as $1.
#
# Rejected: any description/body, and any Co-Authored-By trailer.
# Merge/Revert/fixup!/squash! subjects pass untouched — git writes those bodies
# itself (`git revert` always adds "This reverts commit ...").

msg_file="$1"
if [ -z "$msg_file" ] || [ ! -f "$msg_file" ]; then
    echo "commit-msg: commit message file not found: '$msg_file'" >&2
    exit 1
fi

first_line=$(head -n 1 "$msg_file")

# Allow git-generated commit subjects untouched.
case "$first_line" in
    "Merge "*|"Revert "*|"fixup!"*|"squash!"*)
        exit 0
        ;;
esac

pattern='^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-zA-Z0-9._/ -]+\))?!?: .+'

if ! printf '%s\n' "$first_line" | grep -Eq "$pattern"; then
    cat >&2 <<EOF
commit-msg: invalid commit message subject:

    $first_line

Expected Conventional Commits format:

    <type>[(scope)][!]: <subject>

  type    one of: feat fix docs style refactor perf test build ci chore revert
  scope   optional, in parentheses, e.g. feat(vaultd): ...
  !       optional, marks a breaking change, e.g. feat!: ...

Examples:

    feat(extension): add autofill for login forms
    fix: handle empty vault on first unlock
    chore!: drop support for legacy keyring format

Merge/Revert/fixup!/squash! subjects are allowed as-is.
EOF
    exit 1
fi

# What git will actually keep: everything below its scissors line is the
# --verbose diff (uncommented, so it would otherwise read as a body), and '#'
# lines are git's own template. Drop both, then the subject, then blank lines;
# whatever survives is a real description the author typed.
body=$(awk '/^#+[[:space:]]*-+[[:space:]]*>8[[:space:]]*-+/ { exit }
            /^#/ { next }
            { print }' "$msg_file" |
    sed -e '1d' -e '/^[[:space:]]*$/d')

if printf '%s\n' "$body" | grep -Eiq '^[[:space:]]*co-authored-by[[:space:]]*:'; then
    cat >&2 <<EOF
commit-msg: Co-Authored-By trailers are not allowed.

Remove the trailer and keep the commit to its subject line only.
EOF
    exit 1
fi

if [ -n "$body" ]; then
    cat >&2 <<EOF
commit-msg: commit descriptions (message bodies) are not allowed:

$(printf '%s\n' "$body" | sed 's/^/    /')

Keep the commit to a single Conventional Commits subject line. If the change
needs explaining at length, split it into smaller commits or put the detail in
the code, docs, or the pull request.
EOF
    exit 1
fi

exit 0

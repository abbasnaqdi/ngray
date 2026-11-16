#!/usr/bin/env bash
# Bump the patch component of internal/version/version.go.
#
#   bash scripts/bump-version.sh           # print + write the new version
#   bash scripts/bump-version.sh --stage   # also git add the file (pre-commit)
#
# Minor/major bumps stay manual: edit version.go, stage it, and the pre-commit
# hook will leave it alone. Set NGRAY_NO_VERSION_BUMP=1 to skip the hook.
set -euo pipefail

root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$root"
file="internal/version/version.go"

[ -f "$file" ] || { echo "bump-version: $file not found" >&2; exit 1; }

old=$(sed -n 's/.*Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$file" | head -n 1)
if [ -z "$old" ]; then
    echo "bump-version: no Version = \"x.y.z\" in $file" >&2
    exit 1
fi
case "$old" in
    *.*.*) ;;
    *) echo "bump-version: version $old is not x.y.z" >&2; exit 1 ;;
esac

major=${old%%.*}
rest=${old#*.}
minor=${rest%%.*}
patch=${rest#*.}
num=${patch%%[!0-9]*}
suffix=${patch#"$num"}
if [ -z "$num" ]; then
    echo "bump-version: version $old has no numeric patch" >&2
    exit 1
fi

new="$major.$minor.$((num + 1))$suffix"
tmp=$(mktemp "${TMPDIR:-/tmp}/ngray-version.XXXXXX")
trap 'rm -f "$tmp"' EXIT
sed "s/Version[[:space:]]*=[[:space:]]*\"$old\"/Version = \"$new\"/" "$file" > "$tmp"
# cat preserves the existing file mode/ownership; mv from /tmp may not.
cat "$tmp" > "$file"

if [ "${1:-}" = "--stage" ]; then
    git add -- "$file"
fi
printf 'version %s → %s\n' "$old" "$new"

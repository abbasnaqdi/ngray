#!/usr/bin/env bash
# Install the repo's versioned git hooks (currently pre-commit auto-bump).
# Run once per clone:  make hooks
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
chmod +x scripts/git-hooks/* scripts/bump-version.sh

old=$(git config --get core.hooksPath || true)
if [ -n "$old" ] && [ "$old" != "scripts/git-hooks" ]; then
    echo "replacing core.hooksPath=$old"
fi
git config core.hooksPath scripts/git-hooks

echo "✓ git hooks installed (core.hooksPath=scripts/git-hooks)"
echo "  every commit bumps the patch version in internal/version/version.go"
echo "  manual minor/major: edit + stage version.go; hook respects it"
echo "  skip once: NGRAY_NO_VERSION_BUMP=1 git commit ..."

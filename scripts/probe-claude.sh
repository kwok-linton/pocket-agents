#!/bin/sh
set -eu

# Uses the current user's existing native login/config; never copies credentials
# or modifies native logs. Set an absolute binary if multiple CLIs are installed.
POCKET_CLAUDE_BINARY=${POCKET_CLAUDE_BINARY:-$(command -v claude)}
export POCKET_CLAUDE_BINARY
case "${1:-doctor}" in
  doctor)
    "$POCKET_CLAUDE_BINARY" --version
    "$POCKET_CLAUDE_BINARY" auth status
    "$POCKET_CLAUDE_BINARY" daemon status
    ;;
  loop)
    : "${2:?Usage: scripts/probe-claude.sh loop /absolute/dedicated-trusted-directory}"
    export POCKET_CLAUDE_REAL=1 POCKET_CLAUDE_CWD="$2"
    go test ./internal/claudenative -run '^TestRealNativeLoop$' -v -count=1
    ;;
  resume)
    : "${2:?Usage: scripts/probe-claude.sh resume NATIVE_ID /absolute/original-directory}"
    : "${3:?Original directory required}"
    export POCKET_CLAUDE_RESUME="$2" POCKET_CLAUDE_CWD="$3"
    go test ./internal/claudenative -run '^TestRealResume$' -v -count=1
    ;;
  existing)
    : "${2:?Usage: scripts/probe-claude.sh existing NATIVE_ID [terminal|send|interrupt|terminate] [text]}"
    export POCKET_CLAUDE_EXISTING="$2" POCKET_CLAUDE_ACTION="${3:-terminal}" POCKET_CLAUDE_TEXT="${4:-}"
    go test ./internal/claudenative -run '^TestRealExistingSession$' -v -count=1
    ;;
  approval)
    : "${2:?Usage: scripts/probe-claude.sh approval DEDICATED_MANUAL_MODE_NATIVE_ID}"
    export POCKET_CLAUDE_APPROVAL="$2"
    go test ./internal/claudenative -run '^TestRealApproval$' -v -count=1
    ;;
  *) printf '%s\n' 'Commands: doctor, loop CWD, resume ID CWD, existing ID ACTION TEXT, approval ID' >&2; exit 2 ;;
esac

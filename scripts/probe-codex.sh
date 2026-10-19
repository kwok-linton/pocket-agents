#!/bin/sh
# Explicit opt-in probes against the current user's official native daemon.
# No daemon restart, history deletion or credential changes are performed.
set -eu
cd "$(dirname "$0")/.."
case "${1:-}" in
 approval)
  mkdir -p /tmp/pocket-codex-m0
  POCKET_CODEX_APPROVAL_PROBE=1 go test -v ./internal/codexnative -run '^TestNativeApprovalProbe$'
  ;;
 question)
  mkdir -p /tmp/pocket-codex-m0
  POCKET_CODEX_QUESTION_PROBE=1 go test -v ./internal/codexnative -run '^TestNativeQuestionProbe$'
  ;;
 interrupt)
  mkdir -p /tmp/pocket-codex-m0
  POCKET_CODEX_INTERRUPT_PROBE=1 go test -v ./internal/codexnative -run '^TestNativeInterruptProbe$'
  ;;
 resume)
  : "${POCKET_CODEX_RESUME_THREAD:?Set the ID of a stopped Pocket M0 test thread}"
  go test -v ./internal/codexnative -run '^TestNativeStoppedResumeProbe$'
  ;;
 local-approval)
  : "${POCKET_CODEX_LOCAL_APPROVAL_THREAD:?Set the ID of a Pocket M0 read-only approval test thread, open in the native TUI}"
  go test -v ./internal/codexnative -run '^TestNativeTerminalApprovalProbe$'
  ;;
 *) printf '%s\n' 'Usage: scripts/probe-codex.sh approval|question|interrupt|resume|local-approval' >&2; exit 2 ;;
esac

# Validation checklist

Use isolated data and disposable sessions. Do not publish raw transcripts, device identities, credentials, private URLs, screenshots of unrelated work, or local deployment logs.

For each release record the version/commit, OS and CLI versions, test method, and result. Distinguish passed, failed, and not tested.

## Automated checks

`make check` runs Go vet, Go race tests, the frontend build, and frontend tests. `make cross` checks target compilation only.

## Real-device checks

- Discover existing sessions without modifying native records.
- Create in the selected device/profile/directory; verify native cwd and session identity.
- Continue from phone and CLI without creating a second writer.
- Test approvals, multi-select prompts, interruption, and stale execution rejection.
- Verify long text/voice drafts, attachments, and result downloads.
- Restart the controller and nodes; verify identity and deduplication.
- Disconnect/reconnect the private network without losing drafts or treating it as logout.
- Verify notification receipt, deduplication, read state, and deep links on physical phones.
- Verify permission-denied, upstream-error, and unknown-result recovery.
- Check fresh installation, upgrade, and uninstall without deleting user projects or native history.

Historical development included real macOS multi-device and Android notification testing. These are not a release-wide guarantee. Full iOS and Linux acceptance remains to be established for each supported release. Private evidence must be kept outside the repository and release archives.

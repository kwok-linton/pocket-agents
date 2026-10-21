# Compatibility

Native integration depends on the installed CLI version and launch method. A discovered transcript does not imply that an existing process can be controlled.

| Component | Validation boundary |
| --- | --- |
| macOS arm64 | Real native sessions and multi-device operation exercised during development |
| Linux amd64 / arm64 | Build targets; full runtime and fresh-install acceptance still required |
| iPhone Safari / Home Screen app | Intended client; full physical-device acceptance still required |
| Android Chrome / PWA | Real notification receipt and deep-link flow exercised; broader acceptance is release-specific |
| Windows | Not supported |

Development has exercised Claude Code 2.1.302 and Codex CLI 0.153.4/0.162.1 with shared app-server versions up to 0.163.0. This is a historical baseline, not a guarantee for every version or feature. Run `pocket doctor` against the actual configured binary.

## Known limitations

- Codex single-thread process termination is not generally available through the validated API. Do not stop the shared daemon to simulate it.
- Claude live input requires the supported native bridge/launch path. Old externally launched sessions may have limited control.
- Leaving a `pocket claude` terminal (Ctrl+Z, closing the window, a dropped connection) keeps the background instance running. Only a native `/exit` submitted and recorded from that same connection stops it.
- Workspace trust, OS file permissions, and native authentication may require local interaction.
- Process memory is not guaranteed to survive OS or native runtime restarts. History can be resumed separately.
- Unknown delivery is not automatically retried.
- iPhone push requires installation to the Home Screen and notification permission. Push-service acceptance is not proof of physical delivery.
- UI translations and README translations have separate coverage; a Japanese README does not imply a Japanese application UI.

See [validation](ACCEPTANCE.md) and [installation](INSTALL.md).

### Desktop reconnect display

Default Pocket-managed Claude desktop connections retain the previous terminal frame during native reconnects and show at most one bottom-line reconnect notice per reconnect cycle. Native input, session identity and reconnection remain unchanged. Explicit native subcommands bypass this display adapter. Existing proxy processes must detach and reattach to load an updated binary. Fake CLI/PTY tests and the full checks pass; real-terminal reconnect behavior remains to be verified.

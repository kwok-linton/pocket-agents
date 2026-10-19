# Pocket Agents development guide

Applies to the whole repository; read it before making changes, whether you are a person or a coding agent. It holds stable development rules. The product overview is in `README.md`; validated versions and limits are in `docs/COMPATIBILITY.md` and `docs/ACCEPTANCE.md`.

## Project layout

A self-hosted, mobile-first console for native Claude Code and Codex sessions. A Go backend connects several execution devices over Tailscale; a React + TypeScript PWA provides the phone and desktop UI.

- `cmd/pocket`: CLI entry point; the same binary runs as Hub, Node, or both.
- `internal/core`: shared protocol types for sessions, executions, actions and results.
- `internal/hub`, `internal/store`: authentication, HTTP/WebSocket, action routing, SQLite index and state.
- `internal/node`: execution devices, action validation, attachments.
- `internal/claudenative`, `internal/codexnative`: adapters for the official native runtimes. Keep lifecycle, history and terminal interaction inside the matching provider.
- `internal/config`, `internal/service`, `internal/network`: configuration, background service, networking.
- `internal/workspacefiles`: the file-access boundary. Do not add paths in the UI or routes that bypass it.
- `web/src`: the PWA. `main.tsx` is large; put new logic in small, separately tested modules.
- `web/public/sw.js`, `web/src/app-update.tsx`: offline shell, notifications, updates. API responses must never enter the offline cache.
- `internal/web/dist`: build output embedded in the Go binary. Never edit it by hand; the frontend build syncs it.
- `scripts`: packaging, install tests, licenses, macOS signing, README artwork.

Further reading: `docs/ARCHITECTURE.md`, `docs/DECISIONS.md`, `docs/COMPATIBILITY.md`, `docs/ACCEPTANCE.md`. Before deploying, read `docs/INSTALL.md`, `docs/ONBOARDING.md` and `docs/MACOS-SIGNING.md`. Some docs are written in Chinese.

## Build and develop

Requires Go 1.24+, Node.js 22.12+ and npm; exact dependency versions come from `go.mod` and `web/package-lock.json`. Release binaries do not need frontend tooling. Installing and signing in to the agent CLIs is a separate check.

```sh
# Build the frontend, sync embedded assets and build the Go binary
make build

# Frontend development
cd web
npm ci
npm run dev
```

Run `./bin/pocket doctor` from the repository root to check the environment. Before starting a development server, check ports, data directories and running services: a development process must not share a database or port with a Hub someone is using. Use the CLI's `--help` for current flags.

Never hard-code local usernames, IPs, device IDs or absolute CLI paths. Check the CLI path and version actually configured; the one on your shell `PATH` may not be the one the service uses. The service environment may come from a private `node-env.json`; log variable names only, never values.

## Testing

Run the relevant package or module tests for small changes. Before committing, run the full check:

```sh
make check
# go vet ./..., go test -race ./..., the frontend build and npm test
```

Run `npm --prefix web ci` before the first check. New frontend test files must be added to the `test` script in `web/package.json`; it does not discover them automatically.

Other checks as needed:

```sh
go test ./internal/claudenative ./internal/codexnative ./internal/node ./internal/hub
npm --prefix web test
npm --prefix web run build
python3 scripts/test-install.py --help
python3 scripts/test-package-release.py
make cross
```

- Prefer temporary directories, fake CLIs and PTY/socket fixtures. Automated tests must not touch real working sessions.
- Native tests gated by `POCKET_*` environment variables may create, message, rename or delete real sessions. Read the test first; never point it at a real session ID, and do not inherit unknown test switches.
- Native tests use dedicated, recognisable sessions. Stop the instances you created and hide them from the everyday list afterwards. Never approve real uninstalls, deletions, pushes or other task actions to make a test pass.
- **Isolate stdin when starting the Claude CLI from a script.** Use `stdin=subprocess.DEVNULL` for a plain subprocess and a dedicated PTY for PTY tests. Otherwise the CLI may read the launching script as a user message. Never launch tests with an inherited heredoc stdin.
- Never start a second writer for the same session ID. Resuming history, attaching to a running instance and creating a new session are three different operations.
- A successful build, HTTP 200, a written keystroke or a queued message does not prove the model ran. Check the native record, the execution ID, the actual screen or the result.
- State exactly what was verified on real iPhone/Android, multiple machines, push and reconnection. Simulated viewports and unit tests do not count as device testing.

## Product and state rules

- The native provider's session record is the source of truth; this app's cache can be rebuilt. Continuing a conversation must keep the native session/thread ID. Never copy history into a new session and present it as a resume.
- Keep these states distinct: device online, instance alive, turn working, waiting for an answer, waiting for approval, request failed. Claude's `blocked` is not the same as approval: an API error can also be blocked + idle.
- Actions have stable IDs and are deduplicated. Never resend when the result is unknown. A terminal Enter being written does not mean the native side submitted it.
- Terminal keys: arrows and Tab (including runs such as "↓↓") cannot submit, are exempt from the screen token and may repeat. Enter, text, Esc and Ctrl+C must match the current screen token. `terminal_input` returns the redrawn screen and the next token (after up to ~450 ms of quiet output); that is for display only and does not mean the native side processed the key. The phone terminal panel turns a native menu at the bottom of the screen (`❯ 1. …`) into large buttons: the first tap only moves the highlight, tapping the highlighted option sends Enter (`web/src/terminal-menu.ts`).
- In a multi-select question one Enter may only toggle an option; it must not consume the rest of the question. Keep stale-screen/instance checks, and test individual selection, focus movement and repeated submission.
- The native grey suggested reply is not input. Terminal rendering must keep enough styling that users do not press Enter repeatedly on an empty prompt.
- Never click through someone's real approval prompt to verify a fix. Read-only inspection is fine; changing the outcome of real work is the user's call.
- On the phone, explain the problem and offer the next step. Do not substitute hidden "unlock" entries, raw JSON or a generic terminal button for a clear flow.
- Voice input never sends automatically: finishing puts text in the draft, cancelling discards that dictation. Keep interim text that was shown and ask the user to check it. Test recognition restarts, a missing final callback, cancel, duplicate results and long text.
- Drafts must be viewable in full. Showing only four lines does not mean the content is complete, and visual truncation is not lost recognition.
- Navigation: "Devices" and "New chat"; phone access is separate from device management; projects and connection status have their own pages, not personal settings. In the Chinese UI the Hub is always called 控制端, never "Hub". Use the actual navigation labels (for example "Phone access").
- An unreachable network (Tailscale down, Hub powered off) is not a logout. Only a 401 or `authenticated:false` shows the login page; network errors show "Can't reach the Hub right now" and retry automatically. While unreachable, keep the loading state rather than showing "no devices configured".
- When the user is at the bottom of a conversation, follow new output and mark it read; show the "new messages" hint only while they read older messages. Read markers are broadcast to all clients, so throttle them (currently 30 seconds).
- The active overview sorts by urgency: waiting for answer/approval → request failed → working → finished but unread → everything else, then by time (`sortSessions` in `overview.ts`).
- `rejected` means the native side never received it, so the text goes back into the composer. `unknown` is neither restored nor resent, and its recovery instructions are expanded by default, not hidden in a disclosure.
- Irreversible operations (deleting a native session, ending an instance) are kept apart from everyday organising buttons and use the `danger` style. Command approvals show the command expanded by default; Codex `cancel` is labelled "Deny and stop this turn".
- The terminal panel shows key status in one place only and never says "received" or "processed". Accepted `terminal` and `terminal_input` actions do not show "delivered" under the conversation.
- On phones (≤760px), inputs and selects use a font size of at least 16px (otherwise iOS zooms the page). Buttons that change a session and error messages use at least 12px text and a 44px tap height. Decorative labels may stay compact.

## Performance

Design for real scale: two Macs with about 1,400 native sessions, a full session list of about 1.15 MB of JSON, and a state change every 2–3 seconds when busy.

- **Large list endpoints use `replyConditional`** (`internal/hub/http.go`): ETag, 304 when unchanged, gzip for large bodies. It covers sessions, devices, history, events and notifications. New GET endpoints that may be large or polled should use it too.
- **The frontend `api()` sends If-None-Match automatically** and returns the same object reference on 304, so React's setState skips re-rendering. Never mutate what `api()` returns (copy before sorting). The cache is cleared on logout or 401, lives only in memory, and never enters the HTTP cache or the service worker.
- **Polling and event refreshes pause while the page is hidden** (`document.visibilityState`) and refresh once on return. New timers must do the same.
- **Markdown and attachment components in the conversation are memoised.** Key message lists by `message.id`, never by array index (loading older messages would remount and re-parse everything). Do not create `Intl.DateTimeFormat` repeatedly outside module scope.
- **A Node sends a full snapshot only when its content changes** (hash of sessions sorted by ID, plus a 30-second fallback); otherwise it sends `heartbeat`. Never put fields that change every time, such as `LastSeen`, into the snapshot hash.
- **`last-sessions` is written immediately only when turn state changes**, otherwise at most once a minute; sessions the provider no longer lists are removed. Hub device records are written only when their content changes or a connection drops.
- `snapshot` events only tell browsers to refresh, and only the latest ~2,000 are kept. The push loop wakes only for events that need attention; the browser event stream broadcasts wake-ups by closing a channel.
- The notifications page reads `Store.Inbox` (latest 200). Cursor catch-up (Node forwarding, push, event stream) keeps using `Store.Events` ascending pagination. Do not mix the two.
- Claude discovery runs every second. The "API Error" check for blocked+idle sessions is cached by transcript modification time (`latestReplyFailed`); do not read history on every scan.
- Codex discovery is incremental (`codexnative.threads`): normally it reads only the first page sorted by update time each second and merges it into the last full list. A full list runs every 30 seconds, after any Execute action, or when the whole first page changed (deletions are found only by full lists). Loaded threads get their own `thread/read` each second so approvals and waits stay timely. Writer locks are checked with one read of the `thread-writer-locks/` directory instead of per-thread flock.
- Frontend bundle: Markdown rendering is lazy-loaded (`lazy(() => import("./message-markdown"))`, plain text until loaded) and React is a separate chunk. `seal-pwa.mjs` pre-caches every file under `dist/assets`, so new lazy modules need no extra work. Do not go back to caching only what `index.html` references, or lazy modules fail on the first offline open.

## Phone UI conventions

- The first screen of the overview must show sessions. On phones the title is smaller, stat cards are compact, project chips scroll in one row, and device/agent filters sit behind a "Filter" button (expanded automatically when a filter is set). Before adding an overview module, make sure it does not push the first session card off the first screen.
- Session cards have three lines: agent + status + time / title / device and directory. A fourth line appears only for "Marked done" or pending requests. Cards inside a device or project group do not repeat the device and directory. Times use `formatRelative` (just now / N minutes ago / time today / yesterday) with the full time in `title`.
- Notifications are grouped by day (`dayLabel`) and coloured by type: waiting (amber), turn finished (green), error (red), interrupted (grey). A waiting event is highlighted only if it is the session's latest event and the session is still waiting; otherwise it is dimmed and marked "no longer waiting".
- Conversation view: while the user is at the bottom, any growth (new messages, progress lines, code blocks finishing layout, panels expanding) keeps it pinned to the bottom; while reading older messages, nothing moves.
- The waiting count appears in the browser title `(N) Pocket Agents` and on the home-screen icon badge (`navigator.setAppBadge`, ignored where unsupported).
- New chat: choose the agent first, then that agent's model and effort (switching agent resets the model choice).
- Visual layer `web/src/polish.css` is loaded after the entry script in `index.html`, so it lands after `style.css` and unifies the finish without changing component rules. Use its variables for colours, shadows and easing (`--hairline`, `--elev-1..3`, `--ease-out`, `--focus-ring`). List specific transition properties (never `transition: all`) and animate only transform/opacity. Pressed clickable elements scale to 0.96, large cards to 0.985. Hover effects go only inside `@media (hover: hover) and (pointer: fine)`. Each page has a single staggered entrance (`.page > *`); other motion responds only to user actions or real state (the breathing ring while working). Always keep the `prefers-reduced-motion` fallback. Use system fonts (the CSP allows only same-origin fonts and none are bundled) and `tabular-nums` for numbers. No all-caps English eyebrow above page titles. Reuse these class names in new components to get press feedback automatically.
- **Never override the top padding of the header bars** (`.conversation-header`, `.app-header`, `.app-update`). `style.css` uses `env(safe-area-inset-top)` and at least 44px in iOS home-screen mode so buttons stay clear of the status bar, where the system takes taps and buttons appear dead. When adjusting these elements write only `padding-right/bottom/left`, never the `padding` shorthand. Desktop Chrome screenshots do not show this problem; check computed styles or a real device.
- UI verification: Playwright may drive a local Chrome at 390×844 against a real Hub, but must intercept every non-GET API except login/logout (opening a session writes read markers; directory browsing is a POST) and log that browser out at the end. Screenshots do not replace device testing.

## UI language (Chinese and English)

- All UI text is written as `t("中文原文")` (`web/src/i18n.ts`). Chinese mode returns the source text; English mode looks it up in `web/src/locales/en.ts`. With values use `t("{0} 个会话正在工作", { 0: n })`; English may use `{0|session|sessions}` for plurals. Make each sentence one key; never build a sentence from fragments (English word order differs).
- **After adding or changing Chinese text**, run `node web/scripts/i18n-wrap.mjs` (wraps unwrapped Chinese in `t()`, safe to repeat), then `node web/scripts/i18n-keys.mjs --missing` to list keys missing English and add them to `en.ts`. `npm test` fails on unwrapped Chinese, missing translations, or `en.ts` entries for text that no longer exists (delete the translation when you delete UI text).
- Text that must not be translated (language names, brands) goes in an element with `translate="no"`. User- or agent-generated text such as session titles and conversation content stays as is.
- Backend text shown directly (`capabilities.reason`, `session.error`, action result `message`, diagnostic `details`, API errors) goes through `tServer()`: the English UI looks it up in `en.ts`, the Chinese UI in `locales/zh-server.ts` (backend English → Chinese). Keys ending in `": "` match by prefix. When the backend adds user-facing text, update both tables.
- The language setting lives in browser `localStorage` (`pocket:language`, default Chinese); switching reloads the page, and only the current language's table is downloaded. The service worker reads the language from Cache Storage `pocket-preferences-v1` and swaps push bodies to English by event type (tag suffix).
- English is longer on a 390px phone: after adding buttons, labels or placeholders, check an English screenshot for truncation.

## Deployment and macOS permissions

Finish code and tests first. Update a running installation only when asked to deploy or when the task explicitly includes deployment; ordinary commits do not need a service restart.

**Frontend-only changes:**

```sh
make deploy-ui
```

This updates the installed service's UI without replacing the backend binary, avoiding needless restarts, actions turning unknown, and changes to the macOS permission identity. Version checks must bypass the service worker cache: verify the asset version the Hub actually serves, not just the local `dist`.

**Backend changes on a local macOS machine:**

```sh
make deploy-local
```

This builds, signs with the persistent local identity from `scripts/sign-macos-local.py`, installs the `both` service, restarts it and syncs the UI. Confirm the target role; never install a standalone Node as `both`.

- Do not recreate the signing identity on every update, and do not present development signing as Developer ID or notarisation.
- Each additional Mac signs with its own persistent identity and is verified separately. Replace executables with a temporary file in the same directory plus an atomic rename, never by overwriting a running file.
- Never commit signing keys, keychains or passwords, and never change system trust roots to hide a permission problem.
- Before restarting, consider native work in progress and unconfirmed actions. Afterwards check that every device reconnected, native sessions still exist and the served UI version is correct. An install command returning 0 is not proof the deployment works.
- Avoid replacing the backend frequently: repeated macOS file-permission prompts when uploading images are tied to the executable's identity. Respect system authorisation; do not reach for broad permissions as a shortcut.

## Security, Git and delivery

- Start with `git status` and keep other people's uncommitted changes. Never reset, clean or delete material without being asked.
- Never commit `.pocket-attachments`, runtime databases, CLI history, owner tokens, Node pairing secrets, browser cookies, push endpoints, environment files or signing material, and never print them to logs, reports or chat.
- Review what is staged: `git diff --check` and `git diff --stat`, then `git diff --cached` and any new files.
- Maintainer-authorized fixes are committed and pushed directly to `main` in both the public and private Pocket Agents repositories after verification. Do not create a PR unless explicitly requested. Keep their independent histories and publication boundaries intact; never force-push or delete `main`. External contributors may still submit PRs.
- Respect the existing license; do not change the license or repository visibility without the maintainers.
- In the commit description and delivery summary, state what changed, how it was verified and what remains unverified (without private addresses). Distinguish "code changed", "tests pass", "deployed" and "verified on a real device". Do not describe an upstream API outage as fixed.

## What belongs in the repository

Repository docs record only public material: product, architecture, development practice and validation conclusions. Personal directories, customer/employer material, real session and device IDs, internal addresses, raw screenshots and deployment logs stay in local ignored directories and never go into the repository, PRs or release archives. Check the diff before adding public material; deleting a file does not remove it from history.

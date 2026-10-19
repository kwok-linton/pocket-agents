# README visuals

- `logo.svg`: app icon (dark tile). `logo-mark.svg` / `logo-mark-light.svg`: the bare mark
  for dark and light backgrounds — a small agent peeking out of a pocket. The same shapes
  are used by `web/public/icon*.{svg,png}` and the in-app `Brand` component.
- `how-it-works.svg` / `how-it-works-zh.svg`: hand-written architecture diagram.
- `overview*.png`: the actual desktop frontend at 2x with fictional demo fixtures.
- `hero*`, `phones*`, `feature-*`: composites built from 2x captures of the actual frontend.
  `-zh` uses the Chinese UI; `-ja` frames the English UI with Japanese copy, since there is
  no Japanese UI yet. The lock-screen image is a mockup using the real push title and texts.
- `social-preview.png`: 1280×640 image for the repository's Settings → Social preview.

These images are marketing examples, not evidence of real-provider acceptance tests.

To refresh, build the frontend, make Playwright and Chrome available, then run
`node scripts/readme-screenshots.cjs` from the repository root (optionally `en` or `zh`).
It captures into a temporary directory and writes only the files above. The script hosts
static output on an ephemeral loopback port, mocks API/WebSocket responses (including
terminal, model list and QR actions), blocks other origins, and closes the browser/server
afterwards. `scripts/fixtures/demo-qr.png` encodes an example.com URL and cannot sign in.
Never substitute production data or a real authenticated browser profile.

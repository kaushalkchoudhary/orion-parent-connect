# Included third-party assets

## hls.js

`hls.js/hls.min.js` is the existing deployed browser player, version **1.7.3**,
copied unchanged from the source application's installed hls.js package. Its
upstream copyright notice is preserved at `hls.js/LICENSE`; the full Apache-2.0
terms are in `hls.js/APACHE-2.0.txt`. The player also embeds the regenerator-runtime
MIT notice; the corresponding Babel helper license is in
`hls.js/BABEL-HELPERS-LICENSE.txt`. This checkout does not need an
npm install to build or run the portal. The dependency is deliberately pinned to
the tested version; upgrades should repeat the browser playback checks.

- Upstream: https://github.com/video-dev/hls.js
- SHA-256 (`hls.min.js`): `a12e7ee1cd64a69dcdb314157e45dafcba705bfb0b1440b7935cb265d374423e`

## Embedded visual assets

- `internal/parentportal/web/geist.woff2`: Geist font, with its existing
  `GEIST-LICENSE.txt` beside it.
- `internal/parentportal/web/orion.svg`: the existing Orion console mark,
  exported from `webapp/src/components/app-logo.tsx` in the original application.
  `orion.png` is a raster rendering of that same SVG for browser/email use.
- `internal/parentportal/web/orchids.webp`: school branding from the official
  Orchids website's `headerLogoNew.824ca5ca.webp` asset.
- The school illustration is SVG included in `internal/parentportal/web/index.html`.

Logos remain their owners' branding. This repository does not grant a separate
license to reuse school or Orion trademarks. No open-source license is assigned
to the application by this extraction.

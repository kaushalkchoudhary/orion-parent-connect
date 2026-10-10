# Orion Parent Connect

An invitation-only classroom camera portal for Orchids International School, powered by Orion. Parents sign in with their invited email address and a fresh email code, then see the one camera assigned to their account. The service has its own accounts, database and process; it does not share control-console sessions.

This standalone repository contains the portal source, browser assets, tests and deployment templates. Its extraction does not change the running service, camera configuration or account database. The source repository is [kaushalkchoudhary/orion-parent-connect](https://github.com/kaushalkchoudhary/orion-parent-connect), on branch `main`.

## Repository layout

```text
cmd/parent-portal/         Service and operator command entry point
internal/parentportal/    Authentication, email, camera proxy and tests
  web/                   Embedded HTML, CSS, JavaScript, logos and font
config/                   Safe example configuration; private settings ignored
deploy/
  systemd/                Dedicated portal service unit
  nginx/                  HTTPS path routing and legacy redirect templates
docs/                     Architecture, security and operations
third_party/hls.js/        Pinned browser player and its license
bin/                      Local build output (ignored)
go.mod, go.sum             Standalone module and dependency checksums
Makefile                  Build and verification commands
```

The module is `orion/parent-connect`. There is no frontend build or production npm dependency. Go embeds the application web assets; the separately bundled HLS player must be available at the configured `hlsJS` path.

## Setup

Install Go 1.22 or newer and a native C compiler. SQLite uses CGO, so builds require `CGO_ENABLED=1`; build for the deployment operating system and architecture with the appropriate compiler. Dependencies remain pinned in `go.mod` and `go.sum`.

```sh
make build
cp config/portal.example.json config/portal.json
chmod 600 config/portal.json
```

For a new environment, fill in the public HTTPS origin, relay origin, state path and SMTP sender settings. Create the SMTP password file separately with mode `0600`, readable only by the service account. The example contains no usable credentials or camera assignment. Never overwrite an existing production configuration with it.

The application serves routes at `/`; the reverse proxy removes the configured `basePath`, normally `/classroom`. `origin` must be the browser's HTTPS origin without a trailing slash. The portal listener is private, so the link emailed to parents has no internal service port. Follow [operations](docs/OPERATIONS.md) to install the unit, configure nginx and start the service.

For compatibility, the deployment templates retain `/opt/wantara/parent-portal` for installed binaries and the HLS player, `/etc/orion-parent` for private configuration, and `/var/lib/orion-parent` for state. These paths are deployment locations, not dependencies on the former repository.

## Accounts and access

An operator creates an account with one classroom label and one relay camera path. The invitation contains the portal link, account email and classroom assignment; no password is issued. Account creation is not public.

```sh
sudo -u orion-parent /opt/wantara/parent-portal/parent-portal \
  -config /etc/orion-parent/portal.json -action invite \
  -email parent@example.com -class 'Classroom A' -camera classroom-a
```

`invite` refuses an existing account. Use `reissue` with the same arguments to resend access instructions and update an active account's classroom assignment; this invalidates existing sessions and pending codes. `notify` sends a link reminder without changing the assignment. `revoke` disables access. Each action uses the private configuration and sends mail only when explicitly invoked. See [operations](docs/OPERATIONS.md) for the commands and recovery behavior.

Leave `smtp.teamInbox` empty to send login codes to the invited account. Setting it explicitly routes codes to a school-team mailbox for approval instead; the login page shows that destination. Codes expire after five minutes, and authenticated sessions last 30 minutes. The server chooses the camera from the account on every media request. Verification emails show a selectable code; the portal offers **Paste code** with a manual-paste fallback. Gmail decides whether its own copy-code card appears.

The live player shows status, pause/resume and fullscreen controls without a playback timer or seek bar.

## Verification

```sh
make test
make vet
```

The optional browser check needs Node, Playwright, Chrome and FFmpeg. Point `PLAYWRIGHT_MODULE` at an installed Playwright module:

```sh
PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs make browser-test
```

It uses a temporary server, generated video and a fake mail sender. It sends no email and does not access deployed cameras. It covers login, code verification, protected playback, mobile layout and logout. Screenshots stay in the test's temporary directory. The bundled HLS player is the default; `PARENT_HLS_JS` can override its path. Set `PARENT_BROWSER_TEAM=1` to exercise school-team code delivery.

Unit and integration checks cover authorization, account changes, expiry, throttling, email formatting and media isolation. See [architecture](docs/ARCHITECTURE.md) for the request flow and [security](docs/SECURITY.md) for the trust boundaries and deployment limitations.

## Branding and distribution

The Orion mark is preserved from the existing console; its PNG rendition is used in HTML email. The Orchids logo is from the school's official site. The Geist font license accompanies the font in `internal/parentportal/web/`; the HLS player license is in `third_party/hls.js/`. HTML email currently loads the Orion image from the existing public portal URL in `internal/parentportal/mail.go`; update that asset URL when deploying under a different hostname. Brand assets are not a grant to reuse either organization's trademarks. Do not add credentials, account databases, camera footage or authenticated screenshots to this repository.

Native iOS playback, sustained load and recipient inbox placement require environment-specific verification before wider enrollment. The camera's source resolution determines playback quality; the portal does not increase it.

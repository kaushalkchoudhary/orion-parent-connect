# Operations

## Service and filesystem boundaries

Parent Connect is a standalone Go service with its own SQLite accounts and sessions.
It proxies the one relay camera assigned to each account. It does not share the
control console's accounts, API or database, and does not operate cameras or DVRs.
The existing installation paths are retained for compatibility:

| Location | Purpose |
| --- | --- |
| `/opt/wantara/parent-portal/parent-portal` | Installed executable |
| `/opt/wantara/parent-portal/hls.min.js` | Pinned HLS player from `third_party/hls.js/` |
| `/etc/orion-parent/portal.json` | Private, machine-specific configuration |
| `/etc/orion-parent/smtp-password` | SMTP app password |
| `/var/lib/orion-parent/portal.db` | Account, challenge and session database |
| `orion-parent-portal.service` | Dedicated service running as `orion-parent` |
| `/etc/nginx/snippets/orion-classroom.conf` | Location snippet inside the existing HTTPS server |

The checkout is source only. Installed binaries, private configuration, databases,
mail, camera captures and rollback backups stay outside Git. This repository
extraction does not deploy anything, move live state or restart a service.

## Build and verify

From the repository root, with Go and a C compiler installed (SQLite uses CGO):

```sh
make build
make test
make vet
```

The executable is `bin/parent-portal`. Build on the target OS/architecture or a
compatible Linux builder; a macOS binary cannot be installed on Linux. There is
no frontend build step: application assets are embedded, while the pinned HLS
player is distributed separately with its license in `third_party/hls.js/`.

The optional browser test needs Node, Playwright, Chrome and FFmpeg:

```sh
PARENT_BROWSER_CHECK=1 \
PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs \
PARENT_HLS_JS="$PWD/third_party/hls.js/hls.min.js" \
go test -run TestBrowserFlow -v ./internal/parentportal
```

It runs against a temporary database, a fake mail sender and generated test video;
it sends no real invitations or OTPs. Set `PARENT_BROWSER_TEAM=1` to exercise team
approval mode. Browser checks are not evidence that the deployment's actual relay
or recipient mailbox is available.

## Configure a new installation

These instructions are for a new installation. **Never replace an existing live
configuration or database with example files.** Use a dedicated `orion-parent`
system user/group with no login shell. Create `/etc/orion-parent` and
`/var/lib/orion-parent` owned by that user, mode `0700`. Keep the executable and HLS
player root-owned in `/opt/wantara/parent-portal` (directory `0755`, executable
`0755`, player and license `0644`). Copy the bundled HLS license alongside the
player as `HLS-LICENSE.txt`, and include `APACHE-2.0.txt` and
`BABEL-HELPERS-LICENSE.txt` with that distribution.

Copy `config/portal.example.json` to `/etc/orion-parent/portal.json`, then fill
these deployment-specific values:

| Setting | Meaning |
| --- | --- |
| `origin` | Public HTTPS origin, no path or trailing slash, e.g. `https://portal.example.com` |
| `basePath` | `/classroom`, matching the nginx location, with no trailing slash |
| `listen` | Loopback listener, normally `127.0.0.1:8096` |
| `dbPath` | Dedicated private SQLite path |
| `hlsOrigin` | Trusted relay origin, no credentials, query or camera path |
| `hlsJS` | Absolute path to the installed HLS player |
| `smtp.host`, `smtp.port` | Authenticated SMTP server: 587 STARTTLS or 465 implicit TLS |
| `smtp.username`, `smtp.from` | Authorized sender account/address |
| `smtp.passwordFile` | Private file containing the SMTP app password |
| `smtp.teamInbox` | Empty sends each OTP to the account email; an explicit address sends it to the school team instead |

The example domains and email addresses are placeholders. The relay must already
provide browser-compatible HLS for each assigned camera path. This service does
not generate a classroom feed or transcode an incompatible source. Use a private
network for an HTTP relay; HTTPS is supported. Do not expose the relay itself to
parents or place DVR passwords in invitation emails.

Write the SMTP app password to the configured secret file through a private
editor or secret-management process. Set both configuration and secret files to
mode `0600`, owned by `orion-parent`. Do not put the secret on a command line,
in the repository or in logs.

Install `deploy/systemd/orion-parent-portal.service` to
`/etc/systemd/system/orion-parent-portal.service`. The unit grants write access
only to `/var/lib/orion-parent`; it retains the existing binary/config paths.
For a fresh installation:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now orion-parent-portal.service
sudo systemctl status orion-parent-portal.service
```

## HTTPS routing

Provision the public hostname and its TLS certificate first. Customize the
hostname in `deploy/nginx/classroom-location.conf` to match `origin`, then install
it as `/etc/nginx/snippets/orion-classroom.conf`. Add only this line **inside the
existing matching HTTPS `server` block**:

```nginx
include /etc/nginx/snippets/orion-classroom.conf;
```

Do not replace the whole host configuration: it may serve the control console
and other applications. The snippet strips `/classroom/` before proxying to
loopback and forwards the actual client IP. If another trusted proxy sits in
front of nginx, configure nginx's real-IP handling for that proxy explicitly;
do not trust arbitrary client-supplied forwarding headers.

```sh
sudo nginx -t
sudo systemctl reload nginx
```

The public invitation URL is `https://portal.example.com/classroom/`, with no
explicit port. Do not open port 8096 publicly. The optional
`deploy/nginx/legacy-8443-redirect.conf.example` is only for an installation that
already issued port-8443 links. Fill its hostname and certificate paths, check for
conflicting listeners and install it only if that compatibility is needed.

## Account administration

Use the installed binary with the private configuration. Replace the example
recipient, class and camera path with an authorized assignment verified against
the relay. The following command sends a real email:

```sh
sudo -u orion-parent /opt/wantara/parent-portal/parent-portal \
  -config /etc/orion-parent/portal.json -action invite \
  -email parent@example.com -class 'Classroom A' -camera classroom-a
```

`invite` creates a new account and sends one email containing the portal link,
account email and classroom label, with email-and-OTP sign-in instructions. It refuses an existing account. A mail
failure does not leave an active account.

`reissue` takes the same arguments and deliberately replaces an existing active
account's camera assignment. It emails the access instructions and link,
and revokes prior sessions and pending OTPs in the same database transaction. If
sending fails, the old assignment remains. SMTP acceptance followed
by a database commit failure needs operator repair; the reported error must not
be treated as a completed invitation or access update.

To revoke access:

```sh
sudo -u orion-parent /opt/wantara/parent-portal/parent-portal \
  -config /etc/orion-parent/portal.json -action revoke -email parent@example.com
```

`notify` takes `-email` and sends the current link without changing the assignment.
OTPs are not printed by these commands. Existing accounts use email-and-OTP login
automatically; no reissue or new invitation is needed.

## Verify and troubleshoot

```sh
sudo systemctl status orion-parent-portal.service
sudo journalctl -u orion-parent-portal.service -n 60 --no-pager
curl -sS -o /dev/null -w '%{http_code}\n' https://portal.example.com/classroom/
curl -i https://portal.example.com/classroom/media/index.m3u8
```

The page should load and unauthenticated media should return `401`. An authorized
end-to-end check must enter the invited email address, receive the OTP at the configured
destination, play the assigned real classroom feed, then verify logout blocks new
media requests. Also check that another camera path and control-console API are
unavailable through this portal. Check mobile layout separately.

For missing email, inspect SMTP acceptance/errors and confirm `teamInbox`: leaving
it empty delivers the OTP to the invited account. SMTP acceptance and the sender's
Sent folder do not prove recipient inbox placement; check recipient spam and mail
provider delivery policies. Never add OTP/password logging to diagnose delivery.
For missing video, verify the relay's camera path, network reachability and codec
before changing the parent account. Never use a different classroom as an
unannounced fallback.

## Copying and pasting login codes

Verification emails put one contiguous six-digit code first in the plain-text
body and in a selectable HTML code block, with the subject "Verification code".
Parents can copy it normally, then choose **Paste code** in the portal or paste
into the existing code field. Spaces and hyphens are accepted as separators;
leading zeroes are preserved. Pasting does not submit or verify automatically.
Clipboard access is requested only when the parent clicks the button. If the
browser denies it or lacks support, ordinary paste and typing remain available.

The Gmail "Copy code" summary card is controlled by Gmail. Google's
[email markup reference](https://developers.google.com/workspace/gmail/markup/reference)
does not document a sender-controlled OTP copy action. Clear wording is a
compatibility measure, not a guarantee that Gmail will show the card. No scripts,
fake copy links, OTP-in-URL links, or unsupported schema are added to the email.
The clipboard button uses the browser's
[Clipboard readText API](https://developer.mozilla.org/en-US/docs/Web/API/Clipboard/readText)
over HTTPS. Its permission behavior varies by browser.

## Update, backup and rollback

Keep a private rollback directory outside the checkout. Before an update, save
the installed binary, HLS player/license, unit, relevant nginx snippet and private
configuration. Backups containing configuration or database state must be readable
only by the operator. Use SQLite's online backup API for a running database, or
stop the service and copy the database with its sidecars. Copying just the database
while it is running is not a safe general backup procedure.

Deploy only the tested binary and its matching player assets. Stop this service,
replace its files, then start it; a portal restart briefly interrupts viewing.
Run `daemon-reload` only if the unit changes, and validate nginx before reloading
if routing changes. Never restart camera workers, the relay or the control console
for a parent portal source update. Application startup performs database migrations,
so preserve a compatible database backup before a release that changes the schema.

Rollback by stopping this service, restoring the previous binary and affected
configuration/assets, validating nginx if changed, then starting the service and
repeating the access/playback checks. Restore a database only when required for
schema compatibility or intentional state recovery: an old database also restores
old camera assignments and sessions, and can undo revocations. Review
that consequence before restoring it.

To retire the portal, stop and disable `orion-parent-portal.service`, remove only
its nginx include/optional legacy redirect, validate nginx and reload. Retain or
delete private state according to the operator's retention decision; do not remove
shared server configuration or camera services.

## Security and operating limits

Each account has one server-side camera assignment. Every media request checks an
active session and account; browser parameters cannot choose another camera.
Relay session cookies stay server-side, scoped to a parent session. Parents receive
neither camera credentials nor control-console permissions.

OTP challenges and browser tokens are hashed at rest; OTPs
expire after five minutes and five incorrect attempts. Sessions expire after 30
minutes, and completing a new login invalidates previous sessions for the account.
HTTPS cookies are host-only, Secure, HttpOnly and SameSite Strict. Cross-origin
POSTs are rejected. OTP cooldowns persist; the per-IP throttle
is in memory and resets on restart. Run one service instance against its dedicated
SQLite database; this is not a distributed session/rate-limit architecture.

Email OTP is a single authentication factor and depends on the security of the
recipient mailbox (or the configured school-team approval process). Authorized viewers can still record their
screen, and already buffered video may remain briefly after logout or revocation.
This service has no roster integration, self-service signup, administrative web
UI or recording/archive subsystem. Native iOS playback, sustained load and recipient
mail delivery need deployment-specific verification before wider enrollment.

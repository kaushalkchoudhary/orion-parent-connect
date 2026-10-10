# Architecture

Parent Connect is one Go service with a SQLite database and an embedded browser interface. It uses a trusted camera relay for HLS and authenticated SMTP for invitations and login codes. The control console is a separate application.

## Request flow

```text
Parent browser
    | HTTPS /classroom/
    v
nginx: TLS termination and path-prefix removal
    | loopback HTTP
    v
Parent Connect
    |-- SQLite: accounts, challenges and sessions
    |-- TLS SMTP: invitations and login codes
    `-- Private HLS relay: the account's assigned camera
```

The configured `origin` is the public browser origin, and `basePath` is the external mount path. nginx removes that prefix before forwarding to the service. Assets, API calls and media requests use relative URLs, so the page and player remain under the same mount. The service's internal listening port is not part of emailed links.

## Code boundaries

`cmd/parent-portal` loads the private JSON configuration and dispatches `serve`, `invite`, `reissue`, `notify` or `revoke`. `internal/parentportal` owns configuration validation, database models, HTTP handling, authentication, email composition and relay access. Its `web` directory is embedded into the binary. The HLS player remains a separate, pinned asset referenced by `hlsJS`.

The application has no dependency on another repository's source tree, frontend build or control API. Deployment keeps the existing service filesystem paths to allow installing a binary from this repository without moving state.

## Authentication lifecycle

1. An operator invites an account with an email address, classroom label and relay camera path. The service mails the portal link and email-only sign-in instructions. A failed send leaves no active account.
2. An active invited email address creates a browser-bound challenge and sends a six-digit code. The destination is the account email unless `smtp.teamInbox` explicitly selects team approval.
3. Verification consumes the challenge atomically and creates a 30-minute session. Other sessions for that account are invalidated.
4. Each account and media request checks the active account and session expiry. Logout removes the current session; revocation disables the account and removes its sessions and challenges.

`reissue` replaces the camera assignment for an existing active account, sends an access email and invalidates sessions and challenges in the same database transaction. Sending failure rolls back the change. SMTP acceptance followed by a database failure is reported for operator repair because email delivery cannot be rolled back.

## Media flow

The browser requests `/media/<filename>` through the mounted portal. The proxy obtains the camera path from the authenticated database account and builds a request to the configured relay origin. It accepts only supported HLS filenames and rejects path traversal; the client cannot select a different camera. Parent cookies and credentials are not forwarded to the relay.

The proxy supports the relay's cookie check and HLS session cookie. It keeps the relay session token in the authenticated parent session, rather than exposing it to the browser or sharing it across accounts. Redirects are constrained to the assigned camera. Media responses are not cached by the portal.

The portal does not ingest RTSP directly, transcode, record or provide a camera inventory. Those responsibilities remain with the existing relay and camera infrastructure. A relay outage therefore prevents playback without granting access to any fallback camera.

## Persistence and process model

SQLite stores accounts, assignments, code challenges and sessions. Legacy password/lockout columns remain unused for database compatibility. The process uses one database connection. Per-IP login throttling is in memory and resets on restart; code cooldowns persist. Run a single service instance with its private state directory. Multi-instance operation and shared rate limiting are not implemented.

Tests use temporary databases and fake mail senders. The optional browser test adds a generated HLS fixture and a mailbox endpoint only to its test handler. No mailbox/debug route is part of the production service.

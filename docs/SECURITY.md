# Security and access boundaries

The portal grants a signed-in account access to one configured camera. It does not share the control console's accounts, cookies or administrative API. Account assignments are made by an operator and must be verified against the intended classroom before sending invitations.

## Authentication controls

- Passwords are randomly generated and stored as bcrypt hashes. They appear in the explicit invitation or reissue email, not in service logs or a recoverable database field.
- Codes expire after five minutes and permit five wrong attempts. The stored challenge and code are hashed and tied to the browser that requested them. Successful verification consumes a challenge atomically.
- Sessions expire after 30 minutes. A completed login replaces previous sessions for that account. Logout, reissue and revocation invalidate the relevant stored access.
- HTTPS cookies are host-only, HttpOnly, Secure and SameSite Strict. Cross-origin POST requests are rejected.
- Login attempts are limited to 10 per IP per 15 minutes. Five wrong account passwords lock that account for 15 minutes. Code requests have a one-minute account cooldown.

An empty `smtp.teamInbox` sends codes to the parent account. A configured team inbox changes this to school-team approval and must be deliberate. Access to the recipient mailbox provides both the delivered password and subsequent codes; this is not an independent hardware or authenticator factor.

## Camera isolation

Every media request rechecks authentication and resolves its camera from the active account. Browser input cannot select another camera. The proxy validates filenames, limits forwarding to the configured relay, strips parent credentials and keeps relay session cookies on the server. No anonymous media route or camera directory is provided by this service.

The relay must be private and reachable only through intended infrastructure. Protecting the portal does not secure a separately exposed relay URL. Already buffered frames may remain visible briefly after logout or revocation. An authorized viewer can make a screen recording; camera restrictions do not prevent this.

## Deployment responsibilities

Terminate TLS at nginx and keep the application listener on loopback. Configure the correct public `origin` and `basePath`; preserve the proxy's client-IP and host handling. SMTP requires authenticated TLS using STARTTLS on port 587 or implicit TLS on port 465. There is no plaintext SMTP fallback.

Run the dedicated service as `orion-parent`. Keep configuration and state directories private, with mode `0700`, and configuration, SMTP password and database files mode `0600`. Restrict backups to the same standard: the database contains email addresses, camera assignments and session material even though passwords are hashed. Do not copy live state into development or commit it to Git.

Examples contain placeholders. Actual credentials belong in a separately configured password file; runtime configuration, databases, footage, authenticated screenshots and local build outputs are not source artifacts. Review staged files before publishing, including any newly added assets or operational notes.

## Operational limits

This is a single-instance service. The IP limiter is process-local, and reverse-proxy configuration determines the client IP it sees. Do not deploy multiple replicas or expose the loopback handler directly without addressing those assumptions.

SMTP acceptance does not prove recipient inbox delivery. A credentials message accepted by SMTP followed by a database failure needs operator repair; see [operations](OPERATIONS.md). The service logs recipient addresses and message IDs for delivery diagnosis, not passwords or codes, so restrict log access as well.

No external security audit, native iOS certification, or sustained-load guarantee is implied by the automated tests. Verify camera assignments, account lifecycle, mailbox delivery and the deployment network before enrolling families. Agree an account and backup retention policy with the service owner; automatic lifecycle retention is not implemented.

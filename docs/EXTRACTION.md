# Standalone source provenance

This repository was extracted on 10 October 2026 (IST) from the working Orion
monorepo revision `d5c2c8f5f62fc6c5decd684a1472bf9f733c9c20`.

| Original location | Standalone location |
| --- | --- |
| `backend/cmd/parent-portal/` | `cmd/parent-portal/` |
| `backend/internal/parentportal/` | `internal/parentportal/` |
| `deploy/parent-portal/` | `config/`, `deploy/`, `docs/` |
| Installed webapp hls.js player | `third_party/hls.js/` |

The Go module is `orion/parent-connect`; it does not depend on a future GitHub
organization or repository name. The module contains only the portal's direct
and transitive dependencies. Tests remain next to their Go package, where they
can exercise internal authentication and proxy behavior. The optional browser
check uses the bundled player and temporary test artifacts rather than paths in
a developer's other checkouts.

The original checkout and the live service were left intact. This source-only
extraction does not move a running process, reset accounts, send invitations,
change camera configuration, or migrate the private database. Future portal work
should use this standalone repository. Deploy an independently built binary by
following [operations](OPERATIONS.md), preserving runtime configuration and state.

No monorepo Git history, unrelated products, production configuration, passwords,
private account databases, camera frames, or authenticated screenshots are included.
The new repository starts with a clean standalone commit. Its GitHub remote must
be supplied by the repository owner; no remote URL is assumed.

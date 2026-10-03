# Security Policy

## Supported versions

Security fixes are provided for the latest release only (the most recent
`v*` tag on `main`).

| Version | Supported |
|---------|-----------|
| latest  | ✅        |
| older   | ❌        |

## Reporting a vulnerability

Please **do not** report security vulnerabilities through public GitHub
issues.

Report a potential vulnerability by opening a
[private security advisory](https://github.com/WelcomeToTheWeb/OurWay/security/advisories/new)
and we will confirm receipt within **5 business days**, triage within
**14 days**, and coordinate a fix and release where one is needed.

Include, where possible:

- A description of the issue and its impact
- Steps to reproduce or a minimal proof of concept
- Affected version(s)

## Scope

In scope: the Go server, Go agent, the web dashboard, and the installer
binaries as shipped from this repository.

Out of scope (third-party): PostgreSQL, Redis, Nginx, Vite, React, and
other dependencies — report those to their respective projects.

## Key areas to keep in mind

- **Agent authentication** uses per-device API keys (`X-Device-Key`);
  user-facing API keys support scopes, rotation and revocation
  (`/api/v2/api-keys`).
- **User authentication** is JWT-based (15 min access / 7 d refresh, with
  JTI revocation and a logout endpoint) and RBAC roles; the SSO session
  cookie is set with the `Secure` flag.
- **Token storage** uses `localStorage` in the dashboard, which is readable
  by any script on the page (XSS). This is a deliberate trade-off for a
  self-hosted admin tool: tokens are short-lived and refreshable, and the
  same-origin policy plus CSP keep third-party scripts out. A same-site
  `HttpOnly` cookie would close the XSS vector but breaks the SPA's
  multi-tab/token-refresh flow without extra plumbing.
- **Webhooks** are HMAC-signed with `X-OurWay-Signature`
  (HMAC-SHA256 over the delivery payload).
- **TLS** is terminated at the reverse proxy (see
  `docs/configuration.md`, "TLS/HTTPS Configuration"); the server itself
  listens on plain HTTP by default.

## Contact

Report details are handled through the GitHub security advisory system
above.

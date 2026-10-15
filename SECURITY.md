# Security Policy

## Reporting a vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Please report security issues privately:

- Use [GitHub's private security advisory](https://github.com/masseo1/go-admin-mysql/security/advisories/new)
- Or email the maintainers directly

We aim to acknowledge reports within 2 business days and provide a fix or mitigation plan within 14 days for confirmed issues.

## Scope

Issues in scope:

- Authentication bypass or privilege escalation in the web UI
- SQL injection or other injection vulnerabilities
- Exposure of stored credentials or the encryption key
- Insecure defaults that could lead to unintended data exposure

Out of scope:

- Issues requiring physical access to the host machine
- Theoretical attacks with no practical exploit path
- Vulnerabilities in MySQL itself

## Security design notes

- MySQL server passwords are encrypted with AES-256-GCM before being written to disk. The encryption key is never stored alongside the data.
- The API never returns the stored password field to the browser.
- Privilege values submitted by the browser are validated against a server-side allowlist before being interpolated into SQL.
- The Kubernetes ingress manifest restricts access to RFC-1918 ranges by default.
- This tool is designed for internal/VPN-only deployment. Do not expose it to the public internet.

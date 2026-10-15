 Contributing to go-admin-mysql

Thank you for your interest in improving go-admin-mysql. This document covers how to get set up, how we work, and what we expect from contributions.

---

## Getting started

### Prerequisites

- Go 1.21+
- Docker (optional, for container testing)
- `openssl` for generating credentials

### Local setup

```bash
git clone https://github.com/masseo1/go-admin-mysql.git
cd go-admin-mysql
make deps
make dev   # generates temporary credentials and starts the server
```

`make dev` prints the generated credentials so you can log in at `http://localhost:8080`.

---

## Making changes

### Branch naming

```
feature/<short-description>
fix/<short-description>
chore/<short-description>
```

### Commit style

Use short, present-tense imperative messages:

```
add role revocation endpoint
fix nil pointer when server store is empty
update Dockerfile to Go 1.22
```

For anything non-trivial, add a body explaining **why** (not what — the diff shows what).

### Before opening a PR

- `go build ./...` passes
- `go vet ./...` is clean
- Manual smoke test: add a server, create a user, grant/revoke a privilege, delete the user

---

## Pull request process

1. Open a PR against `main`
2. Fill in the PR template — link any relevant issues
3. A maintainer will review within a few business days
4. Address review comments; mark resolved threads as resolved
5. Once approved, the maintainer will squash-merge

---

## Reporting issues

Use the GitHub issue templates:

- **Bug report** — unexpected behavior, crashes, incorrect SQL
- **Feature request** — new functionality or improvements

Before filing, search existing issues to avoid duplicates.

---

## Security issues

Do **not** open a public issue for security vulnerabilities. Email the maintainers directly or use GitHub's private security advisory feature.

---

## Code style

- Standard `gofmt` formatting — run `gofmt -w .` before committing
- No external dependencies without discussion — the binary size and supply-chain risk matter
- Prefer clarity over cleverness; this tool is maintained by platform engineers, not necessarily Go specialists

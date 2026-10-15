# Changelog

All notable changes to go-admin-mysql are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased]

## [1.0.0] - 2026-10-13

### Added
- Initial release
- Multi-server registry with AES-256-GCM encrypted credential storage
- User management: create and delete MySQL users
- Role management: create, delete, assign, and revoke roles (MySQL 8.0+)
- Permission management: grant and revoke privileges at global, database, and table scope
- `SHOW GRANTS` viewer per user
- Connection testing with live status indicators
- HTTP Basic Auth protecting all routes
- Docker multi-stage build (UID 1001, read-only root filesystem)
- Kubernetes manifests: namespace, PVC, secret, deployment, service, ingress

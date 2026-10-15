# REST API Reference

All endpoints require HTTP Basic Auth (`ADMIN_USER` / `ADMIN_PASS`).

Base URL: `http://localhost:8080` (or your deployment host).

---

## Server registry

### List servers

```
GET /api/servers
```

Returns all registered servers. Passwords are never included in the response.

**Response 200:**
```json
[
  {
    "id": "d7082648-...",
    "name": "Production DB",
    "host": "mysql.internal.example.com",
    "port": 3306,
    "username": "admin_user",
    "tls_enabled": false,
    "created_at": "2026-10-01T12:00:00Z"
  }
]
```

---

### Register a server

```
POST /api/servers
```

**Request body:**
```json
{
  "name": "Production DB",
  "host": "mysql.internal.example.com",
  "port": 3306,
  "username": "admin_user",
  "password": "the-mysql-password",
  "tls_enabled": false
}
```

`port` defaults to `3306` if omitted. `tls_enabled` defaults to `false`.

**Response 200:** The created server object (without password).

---

### Update a server

```
PUT /api/servers/:id
```

Same body as `POST /api/servers`. Omit `password` to keep the existing one.

---

### Remove a server

```
DELETE /api/servers/:id
```

Closes the connection pool and removes the server entry.

**Response 200:**
```json
{ "message": "server removed" }
```

---

### Test connectivity

```
POST /api/servers/:id/test
```

Forces a fresh connection attempt.

**Response 200:**
```json
{ "ok": true }
```

**Response 502:**
```json
{ "ok": false, "error": "connection refused" }
```

---

## Per-server operations

All paths below are prefixed with `/api/servers/:id/`.

---

### Users

#### List users

```
GET /api/servers/:id/users
```

**Response 200:**
```json
[
  { "host": "%", "username": "alice", "is_locked": false },
  { "host": "localhost", "username": "root", "is_locked": false }
]
```

#### Create user

```
POST /api/servers/:id/users
```

```json
{ "username": "alice", "host": "%", "password": "strong-password" }
```

#### Delete user

```
DELETE /api/servers/:id/users
```

```json
{ "username": "alice", "host": "%" }
```

Uses `DROP USER IF EXISTS` — safe to call even if the user does not exist.

---

### Grants

#### Show grants for a user

```
GET /api/servers/:id/grants?username=alice&host=%25
```

**Response 200:**
```json
[
  "GRANT SELECT ON `mydb`.* TO `alice`@`%`"
]
```

#### Grant a privilege

```
POST /api/servers/:id/grants
```

```json
{
  "username": "alice",
  "host": "%",
  "privilege": "SELECT",
  "database": "mydb",
  "table": "*"
}
```

`table` defaults to `*`. Use `"database": "*"` for global privileges.

Supported privileges: `ALL PRIVILEGES`, `ALL`, `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `CREATE`, `DROP`, `ALTER`, `INDEX`, `REFERENCES`, `EXECUTE`, `CREATE VIEW`, `SHOW VIEW`, `TRIGGER`, `CREATE ROUTINE`, `ALTER ROUTINE`, `LOCK TABLES`, `CREATE TEMPORARY TABLES`, `GRANT OPTION`, `RELOAD`, `REPLICATION SLAVE`, `REPLICATION CLIENT`, `PROCESS`, `SHOW DATABASES`, `SUPER`, `USAGE`, `EVENT`, `FILE`, `CREATE USER`.

#### Revoke a privilege

```
POST /api/servers/:id/revoke
```

Same body as grant.

---

### Roles (MySQL 8.0+)

#### List roles

```
GET /api/servers/:id/roles
```

#### Create role

```
POST /api/servers/:id/roles
```

```json
{ "name": "readonly" }
```

#### Delete role

```
DELETE /api/servers/:id/roles
```

```json
{ "name": "readonly" }
```

#### Assign role to user

```
POST /api/servers/:id/assign-role
```

```json
{ "username": "alice", "host": "%", "role": "readonly" }
```

#### Revoke role from user

```
POST /api/servers/:id/revoke-role
```

Same body as assign.

---

### Utilities

#### List databases

```
GET /api/servers/:id/databases
```

**Response 200:**
```json
["information_schema", "myapp", "mysql", "performance_schema"]
```

#### Get server version

```
GET /api/servers/:id/version
```

**Response 200:**
```json
{ "version": "8.0.36" }
```

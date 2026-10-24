# Sentrix — Repo Conventions

Read `docs/PLAN.md` before making architectural changes. Progress per phase is
in `docs/LOG.md`.

## Repo hygiene — non-negotiable

Enforced by `go run ./scripts/ci all`, not by review. A violation is a bug.

### 1. 300 lines per file, maximum

Applies to `.go`, `.ts`, `.tsx`, `.sql`. Exempt: generated code (`*.gen.ts`,
`*_gen.go`) and `_test.go` / `*.test.ts`. A file growing past 300 lines is
doing more than one thing — split it along that seam, do not shuffle lines
into a helper to get under the number.

### 2. Single Responsibility Principle

Every file, type and function has one reason to change.

- One collector per subsystem: `cpu.go`, `mem.go`, `disk.go` — never a
  `collectors.go`.
- A type that both parses input and writes to the database is two types.
- If describing a function needs the word "and", split it.

### 3. Domain-Driven Design layering

```
internal/
  domain/   entities + value objects. stdlib only.
  app/      use cases + port interfaces (defined by the consumer).
            imports domain only.
  infra/    adapters implementing ports. The only place drivers appear.
  wire/     agent<->server ingest types. stdlib only.
  agent/    agent runtime. may import wire, infra/collector, infra/logtail,
            infra/tlscert (pinning).
```

- Import direction is inward, always. `scripts/ci` fails the build otherwise.
- Ports live in `app/`, named after the need (`IngestStore`), implemented in
  `infra/` named after the driver (`sqlite.IngestStore`).
- Domain types carry no JSON or database tags. Map at the boundary.
- **No SQL outside `internal/infra/sqlite`.** CI fails on SQL string literals
  elsewhere.

## Correctness rules

**R1 — Durable state advances only after the write it describes is stored.**
Compute on a draft; commit after every fallible write has succeeded. In SQLite
that means: same transaction, or it did not happen.

**R2 — A check must fail when the thing it checks is broken.** Write the test
first and watch it fail. Tests that need something absent **fail**, they do
not skip.

## Tenant isolation

SQLite has no row-level security; the repository layer is the boundary.

- Every tenant-scoped port method takes a `domain.Scope`. A `Scope` cannot be
  built from a zero `TenantID`.
- Handlers derive the scope from the session or the agent credential — never
  from a parameter, header or body.
- Every statement on a tenant table binds `tenant_id`, including `UPDATE` and
  `DELETE` by id.
- Cross-tenant jobs (janitor, metering, backup) take `domain.OpsScope`, which
  is deliberately easy to grep for.

## Go

- `gofmt`, `go vet` and `golangci-lint` clean before every commit.
- Wrap errors with context: `fmt.Errorf("open store: %w", err)`.
- `context.Context` is the first parameter on anything doing I/O.
- No `panic` outside `main()` and `init()`.
- Table-driven tests.
- Platform code behind build tags (`//go:build linux`), never
  `runtime.GOOS`. Linux builds must not import `golang.org/x/sys/windows`.

## SQL (SQLite)

- Migrations in `migrations/`, numbered, **immutable once merged** — the
  runner stores a checksum and refuses to start if an applied file changed.
- Connection pragmas live in open code (`infra/sqlite/open.go`), never in
  migrations: `journal_mode` cannot change inside a transaction.
- All writes go through the single writer connection.

## Frontend

- One component per file. Hooks in `hooks/`, API clients in `api/`.
- No business logic in components.
- API types generated from `docs/openapi.yaml`, never hand-copied.

## Security

- Never commit secrets, server IPs or usernames. `.env*`, `*.key`, `*.pem`,
  `id_*` and `data/` are gitignored.
- The server derives `tenant_id` and `host_id` from the authenticated
  credential, never from the payload.

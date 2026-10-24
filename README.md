# Sentrix

Lightweight server monitoring: a Go agent for Linux and Windows, a
single-binary server with an embedded React dashboard, and SQLite storage
behind swappable ports.

**Status:** under construction — see [`docs/PLAN.md`](docs/PLAN.md) and the
per-phase [`docs/LOG.md`](docs/LOG.md).

## Development

```sh
go test ./...            # Go tests
go run ./scripts/ci all  # repo checks: line limit, layering, SQL placement
cd web && npm ci && npm test && npm run build
```

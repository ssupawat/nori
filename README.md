# nori

**nori** - import a spreadsheet, get an API.

Run one binary, sign in as admin, import an .xlsx - every sheet becomes a table with a CRUD API, accounts, and an admin dashboard behind login. One private place for your data, and a stable contract any frontend can be built against. No external database, and every value is a string - same as a spreadsheet cell.

- **Import becomes API** - typed columns, search, sort, pagination, append imports, .xlsx export
- **Accounts and keys** - admin dashboard, human sessions, revocable API keys for agents and scripts (admin / editor / viewer / builder)
- **Audit and backups** - audit trail of every change, scheduled JSON backups, `nori check` / `nori compact` / `nori restore`

## Quick start

```sh
(cd ui && npm ci && npm run build)   # builds the dashboard (Node 22)
go build -o nori .                   # or download a binary from GitHub Releases
ADMIN_PASS=secret ./nori -addr :8080 -data ./data
```

Open http://localhost:8080, sign in as admin, then import an .xlsx (no file at hand: `GET /api/sample.xlsx` generates one).

## Documentation

- [docs/api.md](docs/api.md) - the whole API: endpoints, auth and roles, column types, import format
- [docs/deploy.md](docs/deploy.md) - deploying (Fly.io and anywhere else), day-2 operations, backups
- [docs/configuration.md](docs/configuration.md) - flags, subcommands, white labeling, limits, performance model
- [ARCHITECTURE.md](ARCHITECTURE.md) - the core ideas

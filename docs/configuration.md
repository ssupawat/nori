# Configuration

Every flag has an environment-variable twin where one makes sense; nothing else to configure - no config file, no restart-required settings beyond the flags.

| Flag | Default | Env |
|---|---|---|
| `-addr` | `:8080` | - |
| `-data` | `./data` | - |
| `-admin-user` | `admin` | `ADMIN_USER` |
| `-admin-pass` | required | `ADMIN_PASS` |
| `-brand-name` | `nori` | `BRAND_NAME` |
| `-brand-tagline` | `import a spreadsheet, get an API` | `BRAND_TAGLINE` |
| `-brand-color` | `#18181b` | `BRAND_COLOR` |
| `-brand-logo-url` | (generated art mark) | `BRAND_LOGO_URL` |
| `-backup-dir` | off | - |
| `-backup-every` | `24h` | - |
| `-backup-keep` | `7` | - |
| `-pprof` | off | - |
| `-trace` | off | - |
| `-trust-proxy` | off | - |

First run creates the admin account (`admin` / value of `ADMIN_PASS`, required: without it nori exits with an error instead of guessing). `nori -version` prints the build version.

## Subcommands

Subcommands that operate on a data dir (the `-data` flag is inherited from the global flags, so both `nori -data ./data check` and `nori check -data ./data` work). Run them while the server is stopped - the data file is locked while nori runs:

- `nori check` - read-only integrity scan: every document is decoded, row counts are compared against the metadata, and seq ordering, ids, types and meta/collection consistency are verified. Exit 0 = clean, 1 = problems listed.
- `nori compact` - rebuild the data file from live content. bbolt never returns freed pages, so after heavy deletes the file keeps its high-water size; compact writes the surviving data into a fresh file and swaps, keeping the old dir as `data.bak-<timestamp>` (delete it after verifying). Runs an automatic `check` on the result.
- `nori restore` - rebuild a data dir from a scheduled dump (see [deploy.md](deploy.md)). Dumps from older nori versions are rejected; regenerate them with the current binary.

The data directory holds one bbolt file (`data.db`) with everything - tables, column metadata, settings, users, sessions, audit trail - plus nothing else. Copy the whole directory (while nori is stopped) to move an instance.

## White labeling

Run nori under your own brand - the dashboard name, title, tagline, favicon, logo and accent color all follow the flags:

```sh
ADMIN_PASS=secret ./nori \
  -brand-name acme -brand-tagline "your data, your brand" \
  -brand-color "#2563eb" -brand-logo-url https://example.com/logo.png
```

The logo mark is deterministic generative art seeded from the brand name (set `-brand-logo-url` to use your own image instead), and the accent color drives the primary buttons via `GET /api/branding` (public). With no flags, everything stays plain nori.

Branding is also editable in the dashboard (admin, **Settings**): it stores an override in the database via `PATCH /api/branding` that wins over the flags and survives restarts; `DELETE /api/branding` (or the Reset button) reverts to the startup flags.

Data lives in a single embedded database under `-data` (clover/BoltDB). Backups have their own section in [deploy.md](deploy.md) - a raw directory copy is only safe while the server is stopped.

## Limits

Chosen so a full table still fits the row cache, below the xlsx format limits (1,048,576 × 16,384):

| Limit | Value | Enforced at |
|---|---|---|
| Rows per table | 200,000 (also the largest table the row cache holds; 400,000 rows total across cached tables) | add row, import replace/append → `400 table is full` |
| Columns per table | 256 | create table, add column, import → `400 too many columns` |
| Import file size | 64 MB (`413` over) | multipart body of `POST /api/import` |
| Worksheet decompression | 64 MB per file, 256 MB per archive | import parsing (zip-bomb guard) |

Limits are plain package vars (`store.MaxTableRows`, `store.MaxTableCols`, `store.MaxCachedRowsTotal`, `api.MaxImportBytes`) - change and rebuild if you know what you're trading away. Agents read the same numbers from `/llms.txt`.

## Performance model

Reads are served from an in-memory decoded-row cache that single-row writes keep in sync in place (no rebuild after a write; the one full decode pass costs tens of ms per 10k rows on first access). With a warm cache: a first page in well under a millisecond, a sorted page in single-digit ms, a 50k-row search in tens of ms (search is a linear scan). The cache is a shared budget across tables (400,000 rows total, checked on every load and insert); when a table pushes past it, the largest cached table drops back to per-request scans. Writes are committed with fsync by the storage engine (single-digit ms on a local disk, more on network volumes). These are order-of-magnitude figures measured on an Apple M2 - a shared vCPU is slower; the orders hold, the exact numbers depend on the hardware. Best suited to read-heavy workloads with tables up to ~200k rows.

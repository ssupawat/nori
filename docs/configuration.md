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
| Rows per table | 200,000 (tables up to this size may be row-cached, within the shared budget: 400,000 rows or 200 MB total across cached tables) | add row, import replace/append → `400 table is full` |
| Columns per table | 256 | create table, add column, import → `400 too many columns` |
| Import file size | 64 MB (`413` over) | multipart body of `POST /api/import` |
| Worksheet decompression | 64 MB per file, 256 MB per archive | import parsing (zip-bomb guard) |

Limits are plain package vars (`store.MaxTableRows`, `store.MaxTableCols`, `store.MaxCachedRowsTotal`, `api.MaxImportBytes`) - change and rebuild if you know what you're trading away. Agents read the same numbers from `/llms.txt`.

## Performance model

Reads are served from an in-memory decoded-row cache that single-row writes keep in sync in place (no rebuild after a write). The cache is a shared budget across tables (400,000 rows or 200 MB total, whichever fills first, checked on every load and insert); past either limit the largest cached table drops back to per-request scans. Imports and exports stream: a 200,000-row file is parsed and written in pages, so memory stays flat regardless of table size. Measured on a Fly.io shared-cpu-1x instance (1 GB, Singapore) with a full 200,000-row table, end to end: page reads ~120-140 ms, sorted pages p95 ~240 ms, full-table substring search p50 ~125 ms, a 200,000-row import ~110 s, a full export under 8 s. A 6-hour sustained write-and-search run completed with zero failed requests and no growth in the data file (~219 MB; deleted rows reuse freed pages). Writes are committed with fsync by the storage engine (single-digit ms on a local disk, more on network volumes). Best suited to read-heavy workloads with tables up to ~200k rows; a 1 GB instance is the supported size for a full-cap table at typical widths. One caveat from load testing: at the absolute ceiling (200,000 rows × 256 columns) the table itself is only ~1.8 GB on disk, but reading, checking or dumping it decodes the whole table into ~8-10 GB of RAM - build such max-width tables, but browse them on a 16 GB machine.

# Deploy and operations

nori is one process over one file, so any host that hands you a disk works.

## Fly.io

```sh
fly launch --no-deploy                              # reads fly.toml
fly volumes create nori_data --size 3 --region sin  # same region as the app
fly secrets set ADMIN_PASS=...                      # required on first boot
fly deploy --dockerfile Dockerfile.flyio --build-arg NORI_VERSION=v0.1.0-rc.1
```

`Dockerfile.flyio` downloads the release zip and checksum-verifies it, so
Fly's builder needs no Go or Node toolchain - a new version is a new
`NORI_VERSION`. arm64 machines add `--build-arg NORI_ARCH=arm64`. Plain
`Dockerfile` builds from source for hosts without releases.

The volume mounts at `/data`; the data file and the dumps share it (`-data /data/nori -backup-dir /data/backups`). Three things to know:

- **One machine, always.** Two machines cannot share a volume; nori is single-node by design, and needing a cluster is the migration signal.
- **Size the volume, not the schedule.** Dumps are full copies and `-backup-keep` (default 7) trims the oldest, so backup space stays at 7 dumps no matter how often they run. Verified at the ceiling (200k rows × 256 columns, 2026-10): the table itself is ~1.8 GB on disk, so disk was never the constraint - memory is. Reads, `nori check`, and dumps of a max-width table decode the whole table in RAM (~8-10 GB), which exceeds the 1 GB tier; keep max-width tables to a few thousand rows on small instances, or run wide workloads on a machine with 16 GB. For realistic widths the numbers from [configuration.md](configuration.md) apply.
- **Every deploy backs up too.** Stopping the server (deploy included) writes a shutdown dump to the volume before the old machine goes away.

Scale memory to your data: the row cache holds about 0.9 KB per row, so large tables want RAM (`fly scale memory 1024`). To try the image locally, podman builds and runs it the same way: `podman build -f Dockerfile.flyio --build-arg NORI_VERSION=v0.1.0-rc.1 -t nori . && podman run --rm -v ./data:/data -p 8080:8080 nori`.

The dashboard is admin-only; editor and viewer accounts are API identities for the frontends you build (see [api.md](api.md)).

## Day 2 operations

- **Upgrades**: stop the server (if `-backup-dir` is configured, a dump is written on shutdown), replace the binary, start again. The data dir keeps working across versions; no migration step.
- **Keep it running**: run under systemd / launchd with restart-on-crash. Logs go to stdout.
- **Monitor**: point any uptime checker at `/api/health` (public). Request volume and table capacity: `GET /api/usage` or the Dashboard. Server version, data size, free disk and last backup: `GET /api/status` or the status line at the top of the Dashboard. Watch free disk space too - reads keep working from cache when the disk is full, writes return 500 until space is freed.
- **Rotate a password**: `PATCH /api/users/{name}` with `{"password": "..."}` (admin). Changing a password signs that user out everywhere, so a leaked credential stops working immediately. The change is recorded in the audit trail. Your own password: Settings > Account (other sessions are signed out, the current one stays).
- **Investigate a change**: `GET /api/audit?table=X` (or add `rowId=`) shows who changed what, newest first.
- **Troubleshoot**: add `-trace` for per-stage timings, `-pprof 127.0.0.1:6060` for CPU/heap profiles.
- **Maintenance** (`nori check` / `nori compact` / `nori restore`): run these with the server stopped - the data file is locked while nori runs, and a second process is refused. Stop, run the command, start again; the window is seconds, and a `-backup-dir` setup leaves a fresh dump behind on the way down.
  - `nori check` - integrity scan, after an incident or before an upgrade. Read-only; exit 1 means problems (see [configuration.md](configuration.md)).
  - `nori compact` - when the file stays large after heavy deletes; keeps a `.bak` of the old dir and verifies itself.
  - `nori restore` - rebuild from a dump into an empty dir (see Backups below).
  - Want an integrity check without stopping the server? Dump first (`POST /api/backup`), restore the dump into a temp dir, and run `nori check` on that copy - it validates every row and metadata record, though not the live file itself.

## Backups

Two mechanisms, both safe to use while the server runs:

- **Scheduled dumps**: start the server with `-backup-dir ./backups` and it writes a gzipped dump of every collection on a timer (`-backup-every`, default 24h) and once more on shutdown. Keep the last N files (`-backup-keep`, default 7). Dumps are written 0600 and include users, schema, settings and all data. Need one right away (before a risky change): the Back up now button in the Dashboard's Server strip, or `POST /api/backup`. Each collection is read in its own pass, so a dump during heavy writes is per-collection consistent, not one point in time.
- **Restore**: stop the server, move the old data dir away, then `nori restore -data ./data -dump ./backups/nori-dump-....json.gz`. Restore refuses a data dir that already holds documents, so it can never mix with live data. Last dump time and any failure show in `GET /api/status` (the status line at the top of the Dashboard).

A raw copy of `data.db` is only safe while the server is stopped; use dumps for running servers.

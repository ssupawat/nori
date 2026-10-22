# API reference

Authenticate with `Authorization: Bearer <token>` from `POST /api/login`. All values are strings - same as Sheets. One grammar everywhere: missing resource → 404, invalid input → 400, existing name on create or rename → 409, server or storage failure → 500 (retry later), create → 201 with the resource, update → 200 with the updated resource (password changes: `{"status":"ok"}`), delete → `{"status":"ok"}`, errors → `{"error":"..."}`.

## Roles

| Role | Can do |
|---|---|
| `admin` | everything: the dashboard, edit data, import/export, add columns, delete tables, manage users |
| `editor` | create/update/delete rows on all tables (API only - no dashboard access) |
| `viewer` | read-only, GET requests (API only - no dashboard access) |
| `builder` | builds against the API without seeing data: table names, columns, types, `llms.txt`/`openapi.json`; every row read or write is refused (API only - no dashboard access) |

The dashboard is admin-only. Create editor/viewer/builder accounts for browser frontends and scripts that talk to the API (Access page, admin only); agents should use API keys instead. New keys default to **builder** (private by default: an agent develops the frontend without reading a single row; hand it `viewer`/`editor` only when you decide it may see data). New accounts get a random password shown once and downloadable as a login file; the person signs in through the app they were given and rotates it themselves via `POST /api/me/password` (frontends should offer a change-password screen).

## Column types

Each column has a type hint: `text` (default), `number`, `date`, or `datetime`. Storage stays string, but values are normalized on write (`"1,234.50"` → `"1234.5"`, `"02/01/2006"` → `"2006-01-02"`, `"2026-09-21 14:30"` → `"2026-09-21T14:30:00Z"`) and **strictly validated** - writing `"abc"` to a number column returns 400. Sorting is numeric for number columns (rendered right-aligned), and imports auto-detect types from the values. Change a column's type in the UI (pencil icon next to the column name) or via `PATCH .../columns/{col} {"type": "number", "convert": true}` - with `convert`, all stored values are rewritten to the new canonical form in one batch (values that don't fit stay as-is). Types are included in `/api/tables` and `/llms.txt`. Writing a value that doesn't fit its column type returns 400, and the grid highlights the offending cell.

Time rules: `date` is a calendar date with no zone. `datetime` is stored as **RFC3339 UTC** (offsets convert, naive input counts as UTC) - the dashboard shows it in the browser's timezone, agents get the raw UTC string. Which side of an ambiguous slash date is the day (`3/4/2026`) and whether years ≥ 2400 are read as Buddhist Era (`21/09/2567` → `2024-09-21`) are settings: **Settings → Dates**, default D/M + Buddhist Era on, applies to imports and writes. Numbers never eat leading zeros or 16+ digit integers: values like `0812345678` or `4111111111111111` are kept as text, unchanged.

## Endpoints

```
POST   /api/login                {"username","password"} → {"token","username","role"}
POST   /api/logout
GET    /api/health               public

GET    /api/branding             white-label branding (public)
GET    /api/settings/dates       date settings · PUT same shape (admin)

GET    /api/tables                              → [{"name","columns","types"}]
POST   /api/tables                              {"name":"New","columns":[{"name":"c","type":"number"}]}   admin
GET    /api/tables/{t}/rows                     list - see query params below
POST   /api/tables/{t}/rows                     {"col":"value",...}        editor+
GET    /api/tables/{t}/rows/{id}
PATCH  /api/tables/{t}/rows/{id}                partial update             editor+
DELETE /api/tables/{t}/rows/{id}                                           editor+
GET    /api/tables/{t}/export                   download .xlsx
POST   /api/tables/{t}/columns                  {"name":"New col","type":"number"}   admin
PATCH  /api/tables/{t}/columns/{col}            {"name":"Renamed","type":"date"}     admin
DELETE /api/tables/{t}/columns/{col}            drop column and its values     admin
PATCH  /api/tables/{t}                          {"name":"Renamed"}         admin
DELETE /api/tables/{t}                          drop table                 admin

POST   /api/import             multipart: file=.xlsx, mode=replace|append   admin
GET    /api/sample.xlsx        generated sample workbook (admin) - import it to try the loop without a file
GET    /api/users · POST /api/users (no password = generated, returned once) ·
       PATCH /api/users/{name} {"password"} and/or {"role"} · DELETE /api/users/{name}   admin
       (a password change revokes that user's sessions; a role change applies at once; the last admin can't be demoted)
POST   /api/me/password   {"currentPassword","password"} - change own password, any role (other sessions revoked)
GET    /api/keys · POST /api/keys · DELETE /api/keys/{id}                  admin
GET    /api/usage              {requests24h, byPath, byHour, tables:[{name,rows}], maxRows}   admin
                               hourly aggregates persisted in _usage (restart-safe); the dashboard's own calls
                               are not counted; nothing per-request, no actors
GET    /api/status             {version, uptimeSeconds, dataDir, dataBytes,           admin
                               diskFreeBytes, backup{dir, everySeconds, lastAt, lastPath, lastError}}
POST   /api/backup             trigger a dump now, returns {path} (disabled without -backup-dir)   admin
GET    /api/audit?table=&rowId=&limit=   who changed what, newest first   admin
                               actions: create_row, update_row, delete_row, create_table,
                               import, login, login_failed (with client IP), ...; writes made with an
                               API key carry "key:<name>" (key names are unique)
GET    /openapi.json            OpenAPI 3.1 spec of the whole API (login required)
GET    /llms.txt                plain-text handbook written for AI agents (login required)
```

## Humans vs machines

- **Humans** sign in with username/password (min 8 chars) and get a 7-day **sliding** session: it renews on use, so re-login only happens after a full week of inactivity. Browser frontends never embed credentials - they show a login form and use the human's session token.
- **Machines** (AI agents, server-side scripts, cron jobs) use **API keys**: `nri_<hex>`, no expiry, revocable anytime, role attached (builder/viewer/editor/admin; new keys default to builder, which never sees a row). Created on the Access page (admin only); the full key is shown once and only its SHA-256 hash is stored. Same `Authorization: Bearer nri_...` header. Never ship a key inside browser frontend code.
- `POST /api/login` is rate-limited per IP (10 attempts/minute → 429). Deploy behind TLS - tokens travel in plain headers otherwise. Rate limiting counts by client IP: directly exposed, that is RemoteAddr; behind a reverse proxy start with `-trust-proxy` so it counts visitors (X-Forwarded-For) instead of the proxy.

## List query params

- `limit` (default 50, max 1000), `offset`
- `q` - case-insensitive substring across all columns
- `sort` + `order=asc|desc` - sort by a column
- any **column name** as a param = contains-filter on that column, e.g. `?email=user001` matches `user001@example.com`

Example:

```sh
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/tables/Products/rows?status=active&sort=price&order=asc&limit=20"
```

Response of a list:

```json
{"columns":["name","price"],"rows":[{"id":"…","name":"Tea","price":"120"}],"total":1,"limit":50,"offset":0}
```

CORS is `*` on `/api` so local dev frontends (any port) can call it directly.

## Import format

Each sheet becomes a table (named after the sheet). Row 1 = column names; every value is stored as displayed text. Empty rows are skipped. `mode=replace` drops existing tables with the same name; `mode=append` keeps rows and merges new columns.

Columns named `id`, `seq` or `_id` are storage-internal; an import renames them with a `_` suffix instead of dropping the sheet.

// Small display formatters shared across views.

// Compact relative time for RFC3339 timestamps ("3m ago").
export function ago(rfc) {
  if (!rfc) return "never";
  const s = Math.max(0, (Date.now() - new Date(rfc).getTime()) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

// Human byte size ("1.2 MB").
export function bytes(n) {
  if (n == null) return "";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

// Human duration from seconds ("3d 4h", "52m").
export function duration(s) {
  if (s == null) return "";
  if (s < 60) return `${Math.floor(s)}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  return h > 0 ? `${d}d ${h}h` : `${d}d`;
}

// datetime cells are stored as RFC3339 UTC; editors run in the browser's
// local zone and commit back to UTC
export function utcToLocalInput(value) {
  if (!value) return "";
  const d = new Date(value);
  if (isNaN(d)) return "";
  const off = d.getTimezoneOffset() * 60000;
  return new Date(d.getTime() - off).toISOString().slice(0, 16);
}

export function localInputToUtc(value) {
  const d = new Date(value);
  return isNaN(d) ? value : d.toISOString();
}

<script>
  import { api } from "./api.js";
  import { showToast } from "./toast.svelte.js";
  import { ago, bytes, duration } from "./fmt.js";
  import { actionCls } from "./cls.js";
  import Activity from "@lucide/svelte/icons/activity";
  import ArrowRight from "@lucide/svelte/icons/arrow-right";
  import DatabaseBackup from "@lucide/svelte/icons/database-backup";
  import FileSpreadsheet from "@lucide/svelte/icons/file-spreadsheet";
  import Plus from "@lucide/svelte/icons/plus";
  import TriangleAlert from "@lucide/svelte/icons/triangle-alert";
  import Upload from "@lucide/svelte/icons/upload";

  import RefreshCw from "@lucide/svelte/icons/refresh-cw";
  import Sparkles from "@lucide/svelte/icons/sparkles";

  let { ongoto, onimport, onnewtable, onsample, sampleBusy = false } = $props();

  let usage = $state(null);
  let status = $state(null);
  let recent = $state([]);
  let backingUp = $state(false);

  async function backupNow() {
    backingUp = true;
    try {
      const res = await api("/api/backup", { method: "POST" });
      showToast(`Backup written: ${String(res.path).split("/").pop()}`);
      status = await api("/api/status");
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      backingUp = false;
    }
  }

  // the one table the app leans on hardest, summed across all of its paths
  // (reads, writes, exports): worth watching its capacity first
  const hottest = $derived.by(() => {
    if (!usage) return null;
    const perTable = new Map();
    for (const [path, count] of Object.entries(usage.byPath)) {
      const m = path.match(/\/api\/tables\/([^/]+)/);
      if (!m) continue;
      let name = m[1];
      try {
        name = decodeURIComponent(name);
      } catch {}
      perTable.set(name, (perTable.get(name) ?? 0) + count);
    }
    let best = null;
    for (const [name, count] of perTable) {
      if (!best || count > best.count) best = { name, count };
    }
    return best;
  });

  const totalRows = $derived(
    (usage?.tables ?? []).reduce((sum, t) => sum + t.rows, 0)
  );

  const sectionCls = "rounded-lg border border-zinc-200 bg-white p-4";
  const tileCls = "flex flex-col gap-1 rounded-lg border border-zinc-200 bg-white p-4";

  let refreshing = $state(false);
  let loadedAt = $state(null);

  // quiet: no error toasts
  async function refresh(quiet = false) {
    refreshing = true;
    const fail = (err) => {
      if (!quiet) showToast(err.message, "error");
    };
    await Promise.all([
      api("/api/usage").then((u) => (usage = u)).catch(fail),
      api("/api/status").then((s) => (status = s)).catch(fail),
      api("/api/audit?limit=8").then((r) => (recent = r)).catch(fail),
    ]);
    loadedAt = new Date();
    refreshing = false;
  }

  // refresh every 60s while the page is visible
  $effect(() => {
    refresh();
    const timer = setInterval(() => {
      if (document.visibilityState === "visible") refresh(true);
    }, 60000);
    return () => clearInterval(timer);
  });
</script>

<div class="flex w-full flex-col gap-4">
  <div class="flex flex-wrap items-end justify-between gap-2">
    <div>
      <h2 class="text-xl font-semibold tracking-tight text-zinc-900">Dashboard</h2>
      <p class="mt-0.5 flex items-center gap-1.5 text-sm text-zinc-500">
        Traffic, capacity, and server health
        <button
          class="inline-flex items-center gap-1 rounded px-1 py-0.5 hover:bg-zinc-100 hover:text-zinc-900 disabled:opacity-50"
          title={loadedAt ? `Updated ${loadedAt.toLocaleTimeString()} - refreshes every minute` : "Refresh"}
          aria-label="Refresh dashboard"
          disabled={refreshing}
          onclick={() => refresh()}
        ><RefreshCw class="size-3 {refreshing ? 'animate-spin' : ''}" /></button>
      </p>
    </div>
    {#if status}
      <!-- server metadata as a status line, not a strip: the full paths
           stay in GET /api/status, this keeps the glanceable tail -->
      <div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-zinc-500" title="data in {status.dataDir}{status.diskFreeBytes > 0 ? `, ${bytes(status.diskFreeBytes)} disk free` : ''}">
        <span>version <span class="font-medium text-zinc-700">{status.version}</span></span>
        <span>uptime <span class="font-medium text-zinc-700">{duration(status.uptimeSeconds)}</span></span>
        <span>data <span class="font-medium text-zinc-700 tabular-nums">{bytes(status.dataBytes)}</span></span>
        {#if status.backup}
          <span>
            backups <span class="font-medium text-zinc-700">{status.backup.lastAt ? `last ${ago(status.backup.lastAt)}` : "no dump yet"}</span>{#if status.backup.lastError}<span class="text-red-600"> - failed: {status.backup.lastError}</span>{/if}
          </span>
        {:else}
          <span class="cursor-help text-amber-600" title="Scheduled backups are off - start nori with -backup-dir <dir> to turn them on">backups off</span>
        {/if}
        <button
          class="flex shrink-0 items-center gap-1.5 rounded-lg border border-zinc-300 bg-white px-2.5 py-1.5 hover:bg-zinc-50 disabled:opacity-50"
          title="Write a backup dump immediately"
          onclick={backupNow}
          disabled={backingUp}
        ><DatabaseBackup class="size-3.5" /> {backingUp ? "Backing up…" : "Back up now"}</button>
      </div>
    {/if}
  </div>

  <!-- fresh install: importing is the front door, so it gets the page -->
  {#if usage && usage.tables.length === 0}
    <div class={sectionCls}>
      <div class="flex flex-col items-center gap-3 py-12 text-center">
        <FileSpreadsheet class="size-8 text-zinc-300" />
        <div>
          <p class="font-semibold text-zinc-900">No tables yet</p>
          <p class="text-sm text-zinc-500">
            Import an .xlsx file; every sheet becomes a table with a CRUD API.
          </p>
        </div>
        <div class="flex flex-wrap justify-center gap-2">
          <button
            class="bg-brand flex items-center gap-1.5 rounded-lg px-3 py-2 text-sm font-medium text-brand-on"
            onclick={() => onimport?.()}
          ><Upload class="size-4" /> Import xlsx</button>
          <button
            class="flex items-center gap-1.5 rounded-lg border border-zinc-300 bg-white px-3 py-2 text-sm hover:bg-zinc-50 disabled:opacity-60"
            title="Generated workbook: Products, Customers, Orders"
            disabled={sampleBusy}
            onclick={() => onsample?.()}
          ><Sparkles class="size-4 {sampleBusy ? 'animate-pulse' : ''}" /> {sampleBusy ? "Loading sample…" : "Try sample data"}</button>
          <button
            class="flex items-center gap-1.5 rounded-lg border border-zinc-300 bg-white px-3 py-2 text-sm hover:bg-zinc-50"
            onclick={() => onnewtable?.()}
          ><Plus class="size-4" /> New table</button>
        </div>
      </div>
    </div>
  {:else if usage}
    <!-- KPI tiles: two columns on phones, three from sm -->
    <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4">
    <div class={tileCls}>
      <span class="text-xs text-zinc-500">Requests, last 24h</span>
      <span class="text-2xl font-semibold tabular-nums">{usage.requests24h.toLocaleString()}</span>
    </div>
    <div class={tileCls}>
      <span class="text-xs text-zinc-500">Busiest table</span>
      {#if hottest}
        <span class="truncate text-2xl font-semibold">{hottest.name}</span>
        <span class="text-xs text-zinc-500">{hottest.count.toLocaleString()} requests{usage.tables.some((t) => t.name === hottest.name) ? "" : " · table deleted"}</span>
      {:else}
        <span class="mt-1 text-sm text-zinc-500">No API traffic yet</span>
      {/if}
    </div>
    <div class="{tileCls} col-span-2 sm:col-span-1">
      <span class="text-xs text-zinc-500">Rows stored</span>
      <span class="text-2xl font-semibold tabular-nums">{usage ? totalRows.toLocaleString() : "…"}</span>
      <span class="text-xs text-zinc-500">across {usage.tables.length} table{usage.tables.length === 1 ? "" : "s"}</span>
    </div>
  </div>

  <div class="flex flex-col gap-4 lg:grid lg:grid-cols-3">
    <div class="{sectionCls} flex flex-col lg:col-span-2">
      <div class="mb-3 flex items-baseline justify-between gap-2">
        <span class="text-sm font-semibold">Requests per hour</span>
        {#if usage?.byHour}
          <span class="text-xs text-zinc-500 tabular-nums">peak {Math.max(...usage.byHour).toLocaleString()}/h</span>
        {/if}
      </div>
      {#if usage?.byHour}
        {@const max = Math.max(...usage.byHour, 1)}
        <div class="flex min-h-24 flex-1 items-end gap-[3px] border-b border-zinc-200">
          {#each usage.byHour as n, i (i)}
            <div
              class="min-w-1 flex-1 rounded-t-sm {n === 0 ? '' : 'bg-brand opacity-75 hover:opacity-100'}"
              style="height: {n === 0 ? 0 : Math.max(4, (n / max) * 100)}%"
              title="{n} request{n === 1 ? '' : 's'}, {24 - i === 1 ? 'this hour' : `${24 - i - 1}h ago`}"
            ></div>
          {/each}
        </div>
        <div class="mt-1 flex justify-between text-[11px] text-zinc-500">
          <span>24h ago</span><span>12h ago</span><span>now</span>
        </div>
      {:else}
        <p class="text-xs text-zinc-500">Loading…</p>
      {/if}
    </div>

    <div class="{sectionCls}">
      <div class="mb-3 flex items-baseline justify-between gap-2">
        <span class="text-sm font-semibold">Capacity</span>
        <span class="text-xs text-zinc-500 tabular-nums">of {usage.maxRows.toLocaleString()} rows / table</span>
      </div>
      <!-- inner scroll window inset from the card padding: sliced bars stay
           off the border -->
      <div class="p-1">
        <div class="flex max-h-60 flex-col gap-5 overflow-y-auto pb-2">
        {#each usage?.tables ?? [] as t (t.name)}
          {@const pct = Math.min(100, (t.rows / usage.maxRows) * 100)}
          <div>
            <div class="flex items-center justify-between gap-2 text-xs">
              <span class="min-w-0 truncate font-medium">{t.name}</span>
              <span class="shrink-0 tabular-nums text-zinc-500">{t.rows.toLocaleString()} rows · {pct < 1 ? "<1%" : `${Math.round(pct)}%`}</span>
            </div>
            <!-- no fill below 1% -->
            <div class="mt-2 h-2 overflow-hidden rounded-full bg-zinc-100" title="{t.rows.toLocaleString()} / {usage.maxRows.toLocaleString()} rows">
              {#if pct >= 1}
                <div class="h-2 rounded-full {pct >= 100 ? 'bg-red-500' : pct >= 75 ? 'bg-amber-500' : 'bg-brand'}" style="width: {pct}%"></div>
              {/if}
            </div>
            {#if pct >= 100}
              <p class="mt-1.5 flex items-start gap-1.5 text-xs text-red-600">
                <TriangleAlert class="mt-px size-3.5 shrink-0" /> Full - new rows and imports into this table are rejected. Export is in the table menu.
              </p>
            {:else if pct >= 75}
              <p class="mt-1.5 flex items-start gap-1.5 text-xs text-amber-600">
                <TriangleAlert class="mt-px size-3.5 shrink-0" /> Getting close to the cap - time to plan a migration. Export is in the table menu.
              </p>
            {/if}
          </div>
        {/each}
        {#if (usage?.tables?.length ?? 0) === 0}
          <p class="text-xs text-zinc-500">No tables yet.</p>
        {/if}
        </div>
      </div>
    </div>
  </div>

  <div class={sectionCls}>
    <div class="mb-2 flex items-center justify-between">
      <span class="flex items-center gap-2 text-sm font-semibold"><Activity class="size-4" /> Recent changes</span>
      <button
        class="flex items-center gap-1 text-xs text-zinc-500 hover:text-zinc-900"
        onclick={() => ongoto?.("audit")}
      >View all <ArrowRight class="size-3" /></button>
    </div>
    <ul class="divide-y divide-zinc-100">
      {#each recent as e (e.at + e.action + (e.rowId ?? ""))}
        <li class="flex flex-wrap items-center gap-2 py-2.5 text-xs">
          <span class="rounded-full px-2 py-0.5 font-medium {actionCls(e.action)}">{e.action}</span>
          <span class="font-medium text-zinc-900">{e.table}</span>
          <span class="ml-auto flex shrink-0 items-center gap-2 text-zinc-500">
            {e.user} · {ago(e.at)}
          </span>
        </li>
      {:else}
        <li class="py-2 text-xs text-zinc-500">No changes yet - edits will show up here.</li>
      {/each}
    </ul>
  </div>
  {:else}
    <p class="py-12 text-center text-sm text-zinc-500">Loading dashboard…</p>
  {/if}
</div>

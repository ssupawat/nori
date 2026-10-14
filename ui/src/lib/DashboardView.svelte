<script>
  import { api } from "./api.js";
  import { showToast } from "./toast.svelte.js";
  import { ago, bytes, duration } from "./fmt.js";
  import { actionCls } from "./cls.js";
  import Activity from "@lucide/svelte/icons/activity";
  import ArrowRight from "@lucide/svelte/icons/arrow-right";
  import DatabaseBackup from "@lucide/svelte/icons/database-backup";
  import Download from "@lucide/svelte/icons/download";
  import FileSpreadsheet from "@lucide/svelte/icons/file-spreadsheet";
  import Plus from "@lucide/svelte/icons/plus";
  import TriangleAlert from "@lucide/svelte/icons/triangle-alert";
  import Upload from "@lucide/svelte/icons/upload";

  let { ongoto, onimport, onnewtable, onsample } = $props();

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
      perTable.set(m[1], (perTable.get(m[1]) ?? 0) + count);
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

  $effect(() => {
    api("/api/usage")
      .then((u) => (usage = u))
      .catch((err) => showToast(err.message, "error"));
    api("/api/status")
      .then((s) => (status = s))
      .catch((err) => showToast(err.message, "error"));
    api("/api/audit?limit=8")
      .then((r) => (recent = r))
      .catch((err) => showToast(err.message, "error"));
  });
</script>

<div class="mx-auto flex w-full max-w-4xl flex-col gap-4">
  <div class="mb-3 flex flex-wrap items-end justify-between gap-2">
    <div>
      <h2 class="text-sm font-semibold">Dashboard</h2>
      <p class="text-xs text-zinc-500">Traffic, capacity, and server health</p>
    </div>
    {#if status}
      <!-- server metadata as a status line, not a strip: the full paths
           stay in GET /api/status, this keeps the glanceable tail -->
      <div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-zinc-500" title="version {status.version} - data {bytes(status.dataBytes)} in {status.dataDir}{status.diskFreeBytes > 0 ? `, ${bytes(status.diskFreeBytes)} disk free` : ''}">
        <span class="font-mono text-zinc-400">{status.version}</span>
        <span>up {duration(status.uptimeSeconds)}</span>
        <span class="tabular-nums">{bytes(status.dataBytes)}</span>
        {#if status.backup}
          <span>
            {status.backup.lastAt ? `last dump ${ago(status.backup.lastAt)}` : "no dump yet"}{#if status.backup.lastError}<span class="text-red-600"> - failed: {status.backup.lastError}</span>{/if}
          </span>
        {:else}
          <span class="text-amber-600">Backups off</span>
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
            class="flex items-center gap-1.5 rounded-lg border border-zinc-300 bg-white px-3 py-2 text-sm hover:bg-zinc-50"
            title="Generated workbook: Products, Customers, Orders"
            onclick={() => onsample?.()}
          ><Download class="size-4" /> Try sample data</button>
          <button
            class="flex items-center gap-1.5 rounded-lg border border-zinc-300 bg-white px-3 py-2 text-sm hover:bg-zinc-50"
            onclick={() => onnewtable?.()}
          ><Plus class="size-4" /> New table</button>
        </div>
      </div>
    </div>
  {:else if usage}
    <!-- KPI tiles: uniform, one glance each -->
    <div class="flex flex-col gap-4 sm:grid sm:grid-cols-3">
    <div class={tileCls}>
      <span class="text-xs text-zinc-500">Requests, last 24h</span>
      <span class="text-2xl font-semibold tabular-nums">{usage ? usage.requests24h.toLocaleString() : "…"}</span>
    </div>
    <div class={tileCls}>
      <span class="text-xs text-zinc-500">Busiest table</span>
      {#if hottest}
        <span class="truncate text-2xl font-semibold">{hottest.name}</span>
        <span class="text-xs text-zinc-400">{hottest.count.toLocaleString()} requests</span>
      {:else}
        <span class="text-2xl font-semibold text-zinc-300">-</span>
      {/if}
    </div>
    <div class={tileCls}>
      <span class="text-xs text-zinc-500">Rows stored</span>
      <span class="text-2xl font-semibold tabular-nums">{usage ? totalRows.toLocaleString() : "…"}</span>
      <span class="text-xs text-zinc-400">across {usage?.tables?.length ?? 0} tables</span>
    </div>
  </div>

  <div class="flex flex-col gap-4 lg:grid lg:grid-cols-3">
    <div class="{sectionCls} lg:col-span-2">
      <div class="mb-3 text-sm font-semibold">Requests per hour, last 24h</div>
      {#if usage?.byHour}
        {@const max = Math.max(...usage.byHour, 1)}
        <div class="flex h-20 items-end gap-[4px]" title="oldest left, current hour right">
          {#each usage.byHour as n, i (i)}
            <div
              class="min-w-1 flex-1 rounded-t-sm bg-brand"
              style="height: {n === 0 ? 3 : Math.max(4, Math.round((n / max) * 80))}px"
              title="{n} requests"
            ></div>
          {/each}
        </div>
      {:else}
        <p class="text-xs text-zinc-400">Loading…</p>
      {/if}
    </div>

    <div class="{sectionCls}">
      <div class="mb-3 text-sm font-semibold">Capacity</div>
      <!-- inner scroll window inset from the card padding: sliced bars stay
           off the border -->
      <div class="p-1">
        <div class="flex max-h-60 flex-col gap-5 overflow-y-auto pb-2">
        {#each usage?.tables ?? [] as t (t.name)}
          {@const pct = Math.min(100, (t.rows / usage.maxRows) * 100)}
          <div>
            <div class="flex items-center justify-between text-xs">
              <span class="font-medium">{t.name}</span>
              <span class="tabular-nums text-zinc-500">{t.rows.toLocaleString()} / {usage.maxRows.toLocaleString()} rows</span>
            </div>
            <div class="mt-2 h-2 rounded bg-zinc-100">
              <div class="h-2 rounded bg-brand" style="width: {pct}%"></div>
            </div>
            {#if pct >= 75}
              <p class="mt-1 flex items-center gap-1 text-xs text-amber-600">
                <TriangleAlert class="size-3" /> Getting close to the cap - time to plan a migration. Export is in the table menu.
              </p>
            {/if}
          </div>
        {/each}
        {#if (usage?.tables?.length ?? 0) === 0}
          <p class="text-xs text-zinc-400">No tables yet.</p>
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
          <span class="ml-auto flex shrink-0 items-center gap-2 text-zinc-400">
            {e.user} · {ago(e.at)}
          </span>
        </li>
      {:else}
        <li class="py-2 text-xs text-zinc-400">No changes yet - edits will show up here.</li>
      {/each}
    </ul>
  </div>
  {/if}
</div>

<script>
  import { Dialog, Pagination, DropdownMenu } from "bits-ui";
  import { SvelteSet } from "svelte/reactivity";
  import Upload from "@lucide/svelte/icons/upload";
  import Download from "@lucide/svelte/icons/download";
  import Plus from "@lucide/svelte/icons/plus";
  import Trash from "@lucide/svelte/icons/trash";
  import ChevronLeft from "@lucide/svelte/icons/chevron-left";
  import ChevronRight from "@lucide/svelte/icons/chevron-right";
  import Search from "@lucide/svelte/icons/search";
  import Rows3 from "@lucide/svelte/icons/rows-3";
  import FileSpreadsheet from "@lucide/svelte/icons/file-spreadsheet";
  import Ellipsis from "@lucide/svelte/icons/ellipsis";
  import ArrowUpDown from "@lucide/svelte/icons/arrow-up-down";
  import ArrowRight from "@lucide/svelte/icons/arrow-right";
  import Copy from "@lucide/svelte/icons/copy";
  import X from "@lucide/svelte/icons/x";
  import Pencil from "@lucide/svelte/icons/pencil";
  import { api } from "./api.js";
  import { auth, canEdit } from "./auth.svelte.js";
  import { tables, refreshTables, openTable, closeTable } from "./tables.svelte.js";
  import { showToast } from "./toast.svelte.js";
  import { askConfirm } from "./confirm.svelte.js";
  import Grid from "./Grid.svelte";
  import RecordCards from "./RecordCards.svelte";
  import AddColumnDialog from "./AddColumnDialog.svelte";
  import PromptDialog from "./PromptDialog.svelte";
  import GenArt from "./GenArt.svelte";

  let { onimport, ongoto } = $props();

  const PAGE_SIZE = 50;

  const menuItem =
    "flex cursor-pointer items-center gap-2 px-3 py-2 text-sm data-highlighted:bg-zinc-100";
  const menuItemDanger = menuItem + " text-red-600";

  let rows = $state([]);
  let columns = $state([]);
  let types = $state([]);
  let total = $state(0);
  let q = $state("");
  let sort = $state("");
  let order = $state("asc");
  let page = $state(1);
  let addColumnOpen = $state(false);
  let focusRowId = $state(null);
  // $state doesn't wrap built-ins like Set - SvelteSet is the reactive one.
  const selected = new SvelteSet();

  let seq = 0;
  let searchTimer;

  // every table is an API resource; the strip under the toolbar keeps that
  // visible where the data lives, not only on the API page
  async function copyCurl() {
    const curl = `curl "${location.origin}/api/tables/${encodeURIComponent(tables.current)}/rows?limit=10" -H "Authorization: Bearer ${auth.token}"`;
    try {
      await navigator.clipboard.writeText(curl);
      showToast("curl copied");
    } catch {}
  }

  // Active "col=value" filter, shown as a chip next to the search box.
  const filterChip = $derived.by(() => {
    const m = q.match(/^(\S+)=(.*)$/);
    return m && columns.includes(m[1]) ? { col: m[1], val: m[2] } : null;
  });

  async function loadRows() {
    selected.clear();
    invalidCell = null;
    if (!tables.current) {
      rows = [];
      columns = [];
      types = [];
      total = 0;
      return;
    }
    const my = ++seq;
    const params = new URLSearchParams({
      limit: PAGE_SIZE,
      offset: (page - 1) * PAGE_SIZE,
    });
    // the active chip becomes a contains-filter on that column (backend
    // supports it); anything else in the box is a substring search.
    if (filterChip) {
      params.set(filterChip.col, filterChip.val);
    } else if (q) {
      params.set("q", q);
    }
    if (sort) {
      params.set("sort", sort);
      params.set("order", order);
    }
    try {
      const data = await api(`/api/tables/${encodeURIComponent(tables.current)}/rows?${params}`);
      if (my !== seq) return;
      columns = data.columns;
      types = data.types ?? [];
      rows = data.rows;
      total = data.total;
      // rows shrank under us (import replace, delete): clamp instead of
      // showing an out-of-range empty page
      const pages = Math.max(1, Math.ceil(data.total / PAGE_SIZE));
      if (page > pages) page = pages;
    } catch (err) {
      if (my === seq) showToast(err.message, "error");
    }
  }

  // Reload whenever the table, search, sort or page changes. A saved table
  // that no longer exists (stale localStorage, fresh data dir) is skipped
  // until refreshTables has validated it.
  $effect(() => {
    void q;
    void sort;
    void order;
    void page;
    const name = tables.current;
    if (!name) {
      rows = [];
      columns = [];
      total = 0;
      return;
    }
    if (tables.list.length > 0 && !tables.list.some((t) => t.name === name)) {
      return;
    }
    loadRows();
  });

  function onsort(col) {
    if (sort === col) {
      order = order === "asc" ? "desc" : "asc";
    } else {
      sort = col;
      order = "asc";
    }
    page = 1;
  }

  function onsearch(e) {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => {
      q = e.target.value;
      page = 1;
    }, 200);
  }

  async function addRow() {
    const empty = Object.fromEntries(columns.map((c) => [c, ""]));
    try {
      const created = await api(`/api/tables/${encodeURIComponent(tables.current)}/rows`, {
        method: "POST",
        body: JSON.stringify(empty),
      });
      // the new row sits at the end of the default order, so clear any
      // filter and sort first - otherwise it lands outside the view - then
      // jump past the last page; loadRows clamps back onto it
      q = "";
      sort = "";
      page = Number.MAX_SAFE_INTEGER;
      focusRowId = created.id;
      showToast("Row added");
      await loadRows();
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  // ---- shared prompt dialog (rename table/column, add N rows) ----
  let promptOpen = $state(false);
  let promptTitle = $state("");
  let promptLabel = $state("");
  let promptValue = $state("");
  let promptSubmit = null;

  let promptWithType = $state(false);
  let promptColType = $state("text");

  function askPrompt(title, label, value, fn, withType = false, colType = "text") {
    promptTitle = title;
    promptLabel = label;
    promptValue = value;
    promptSubmit = fn;
    promptWithType = withType;
    promptColType = colType;
    promptOpen = true;
  }

  function renameTable(name) {
    return api(`/api/tables/${encodeURIComponent(tables.current)}`, {
      method: "PATCH",
      body: JSON.stringify({ name }),
    })
      .then(async () => {
        openTable(name);
        await refreshTables();
        showToast(`Renamed to ${name}`);
      })
      .catch((err) => showToast(err.message, "error"));
  }

  function renameColumn(oldCol, newCol, newType = "", convert = false) {
    const body = { name: newCol };
    if (newType) body.type = newType;
    if (convert) body.convert = true;
    return api(
      `/api/tables/${encodeURIComponent(tables.current)}/columns/${encodeURIComponent(oldCol)}`,
      { method: "PATCH", body: JSON.stringify(body) }
    )
      .then(() => {
        showToast(
          newCol !== oldCol
            ? `Renamed to ${newCol}`
            : `Column type set to ${newType}`
        );
        return loadRows();
      })
      .catch((err) => showToast(err.message, "error"));
  }

  let invalidCell = $state(null);

  async function saveCell(row, col, value) {
    try {
      const updated = await api(
        `/api/tables/${encodeURIComponent(tables.current)}/rows/${row.id}`,
        { method: "PATCH", body: JSON.stringify({ [col]: value }) }
      );
      Object.assign(row, updated);
      invalidCell = null;
    } catch (err) {
      // keep the typed value visible and mark the offending cell
      invalidCell = { id: row.id, col };
      showToast(err.message, "error");
    }
  }

  async function deleteRow(row) {
    if (!(await askConfirm("This row will be permanently removed.", "Delete row?"))) return;
    try {
      await api(`/api/tables/${encodeURIComponent(tables.current)}/rows/${row.id}`, {
        method: "DELETE",
      });
      loadRows();
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  async function duplicateRow(row) {
    const data = Object.fromEntries(columns.map((c) => [c, row[c] ?? ""]));
    try {
      await api(`/api/tables/${encodeURIComponent(tables.current)}/rows`, {
        method: "POST",
        body: JSON.stringify(data),
      });
      showToast("Row duplicated");
      loadRows();
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  function toggleSelect(id, checked) {
    if (checked) selected.add(id);
    else selected.delete(id);
  }

  function askRenameColumn(col) {
    const origType = types[columns.indexOf(col)] ?? "text";
    askPrompt(
      `Column "${col}"`,
      "Rename and/or change type",
      col,
      async (v, t) => {
        let convert = false;
        if (t !== origType) {
          convert = await askConfirm(
            `Convert existing values in "${col}" to the ${t} format?`,
            "Convert values?"
          );
        }
        await renameColumn(col, v, t, convert);
      },
      true,
      origType
    );
  }

  function toggleSelectAll(checked) {
    for (const r of rows) {
      if (checked) selected.add(r.id);
      else selected.delete(r.id);
    }
  }

  async function deleteSelected() {
    const ids = [...selected];
    if (!(await askConfirm(`${ids.length} rows will be permanently removed.`, `Delete ${ids.length} rows?`))) return;
    try {
      await Promise.all(
        ids.map((id) =>
          api(`/api/tables/${encodeURIComponent(tables.current)}/rows/${id}`, { method: "DELETE" })
        )
      );
      showToast(`Deleted ${ids.length} rows`);
    } catch (err) {
      showToast(err.message, "error");
    }
    loadRows();
  }

  // called by App after an import finishes; rows may have changed under a
  // table whose name stayed the same, so nothing else would trigger a reload
  export function reload() {
    loadRows();
  }

  async function exportXlsx() {
    try {
      const res = await fetch(
        `/api/tables/${encodeURIComponent(tables.current)}/export`,
        { headers: { Authorization: "Bearer " + auth.token, "X-Requested-With": "nori-dashboard" } }
      );
      if (!res.ok) throw new Error("Export failed");
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = tables.current + ".xlsx";
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  async function deleteTable() {
    if (!(await askConfirm(`"${tables.current}" and all its rows will be removed.`, "Delete table?"))) return;
    try {
      await api(`/api/tables/${encodeURIComponent(tables.current)}`, { method: "DELETE" });
      showToast(`Deleted ${tables.current}`);
      closeTable();
      await refreshTables();
    } catch (err) {
      showToast(err.message, "error");
    }
  }
</script>

<div class="mb-3 hidden flex-wrap items-center gap-2 md:flex">
  <label class="relative block w-48">
    <Search class="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-zinc-500" />
    <input
      class="rounded-lg border border-zinc-300 bg-white py-1.5 pr-3 pl-8 text-sm focus:outline-none"
      type="search"
      placeholder="Search… or col=value"
      oninput={onsearch}
    />
  </label>
  {#if filterChip}
    <button
      class="flex items-center gap-1 rounded-full bg-brand px-2.5 py-1 text-xs text-brand-on"
      title="Clear filter"
      onclick={() => (q = "")}
    >{filterChip.col}={filterChip.val}<X class="size-3" /></button>
  {/if}
  <span class="flex-1"></span>
  {#if canEdit()}
    {#if selected.size > 0}
      <button
        class="rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50 flex items-center gap-1.5"
        onclick={deleteSelected}
      ><Trash class="size-3.5" /> Delete {selected.size} rows</button>
    {/if}
    <button
      class="flex items-center gap-1.5 rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50"
      onclick={addRow}
    ><Plus class="size-3.5" /> Add row</button>
  {/if}
  {@render tableMenuButton()}
</div>

{#snippet tableMenuButton()}
  <DropdownMenu.Root>
    <DropdownMenu.Trigger
      class="rounded-lg border border-zinc-300 bg-white p-2.5 text-zinc-900 hover:bg-zinc-50"
      title="Table actions"
    >
      <Ellipsis class="size-4" />
    </DropdownMenu.Trigger>
    <DropdownMenu.Portal>
      <DropdownMenu.Content class="z-50 min-w-48 rounded-md border border-zinc-200 bg-white py-1 shadow-md" align="end">
        {#if !tables.current}
          <!-- nothing selected: importing is the only action that works -->
          {#if auth.role === "admin"}
            <DropdownMenu.Item class={menuItem} onSelect={() => onimport?.()}>
              <Upload class="size-3.5" /> Import xlsx…
            </DropdownMenu.Item>
          {/if}
        {:else}
          {#if auth.role === "admin"}
            <DropdownMenu.Item class={menuItem} onSelect={() => onimport?.()}>
              <Upload class="size-3.5" /> Import xlsx…
            </DropdownMenu.Item>
            <DropdownMenu.Item class={menuItem} onSelect={() => (addColumnOpen = true)}>
              <Plus class="size-3.5" /> Add column
            </DropdownMenu.Item>
            <DropdownMenu.Item
              class={menuItem}
              onSelect={() => askPrompt("Rename table", "New table name", tables.current, renameTable)}
            >
              <Pencil class="size-3.5" /> Rename table
            </DropdownMenu.Item>
          {/if}
          <DropdownMenu.Item class={menuItem} onSelect={exportXlsx}>
            <Download class="size-3.5" /> Export xlsx
          </DropdownMenu.Item>
          {#if auth.role === "admin"}
            <DropdownMenu.Item class={menuItemDanger} onSelect={deleteTable}>
              <Trash class="size-3.5" /> Delete table
            </DropdownMenu.Item>
          {/if}
        {/if}
      </DropdownMenu.Content>
    </DropdownMenu.Portal>
  </DropdownMenu.Root>
{/snippet}

<!-- Mobile: search + overflow menu -->
<div class="mb-3 flex items-center gap-2 md:hidden">
  <label class="relative flex-1">
    <Search class="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-zinc-500" />
    <input
      class="w-full rounded-lg border border-zinc-300 bg-white py-2 pr-3 pl-8 text-sm focus:outline-none"
      type="search"
      placeholder="Search… or col=value"
      oninput={onsearch}
    />
  </label>
  {#if filterChip}
    <button
      class="flex items-center gap-1 rounded-full bg-brand px-2.5 py-1 text-xs text-brand-on"
      title="Clear filter"
      onclick={() => (q = "")}
    >{filterChip.col}={filterChip.val}<X class="size-3" /></button>
  {/if}
  {#if sort}
    <button
      class="flex items-center gap-1 rounded-full bg-brand px-2.5 py-1 text-xs text-brand-on"
      title="Clear sort"
      onclick={() => (sort = "")}
    >{sort} {order === "asc" ? "↑" : "↓"}<X class="size-3" /></button>
  {/if}
  <DropdownMenu.Root>
    <DropdownMenu.Trigger
      class="rounded-lg border border-zinc-300 bg-white p-2.5 text-zinc-900 hover:bg-zinc-50"
      title="Sort rows"
    >
      <ArrowUpDown class="size-4" />
    </DropdownMenu.Trigger>
    <DropdownMenu.Portal>
      <DropdownMenu.Content
        class="z-50 max-h-72 min-w-44 overflow-y-auto rounded-md border border-zinc-200 bg-white py-1 shadow-md"
        align="end"
      >
        <DropdownMenu.Item
          class={menuItem}
          onSelect={() => {
            sort = "";
            page = 1;
          }}
        >Default order</DropdownMenu.Item>
        {#each columns as col (col)}
          <DropdownMenu.Item
            class={menuItem}
            onSelect={() => {
              sort = col;
              order = "asc";
              page = 1;
            }}
          >{col} ↑</DropdownMenu.Item>
          <DropdownMenu.Item
            class={menuItem}
            onSelect={() => {
              sort = col;
              order = "desc";
              page = 1;
            }}
          >{col} ↓</DropdownMenu.Item>
        {/each}
      </DropdownMenu.Content>
    </DropdownMenu.Portal>
  </DropdownMenu.Root>
  {@render tableMenuButton()}
</div>

{#if tables.current}
  <!-- the table's read path, kept next to the data -->
  <div class="mb-3 flex items-center gap-2 overflow-hidden rounded-lg border border-zinc-200 bg-zinc-50 px-3 py-1.5">
    <span class="shrink-0 rounded border border-zinc-200 bg-white px-1.5 py-0.5 text-[11px] font-semibold text-zinc-500">API</span>
    <code class="min-w-0 flex-1 truncate font-mono text-xs text-zinc-600" title="Read this table over the API">
      GET /api/tables/{tables.current}/rows
    </code>
    <button
      class="flex shrink-0 items-center gap-1 rounded px-1.5 py-1 text-xs text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900"
      title="Copy a ready curl for this table"
      onclick={copyCurl}
    ><Copy class="size-3" /> curl</button>
    <button
      class="flex shrink-0 items-center gap-1 text-xs text-zinc-500 hover:text-zinc-900"
      onclick={() => ongoto?.("api")}
    >API reference <ArrowRight class="size-3" /></button>
  </div>
{/if}

{#if tables.current}
  <Grid
    {rows}
    {columns}
    {types}
    invalid={invalidCell}
    {focusRowId}
    canEdit={canEdit()}
    renameable={auth.role === "admin"}
    {selected}
    {sort}
    {order}
    {onsort}
    onsave={saveCell}
    onselect={toggleSelect}
    onselectall={toggleSelectAll}
    onduplicate={duplicateRow}
    ondelete={deleteRow}
    onrenamecol={askRenameColumn}
    emptyText={q ? "No rows match the search." : "No rows yet 🌱"}
  />
  <!-- Mobile: record cards + tap-to-edit sheet -->
  <RecordCards
    {rows}
    {columns}
    {types}
    invalid={invalidCell}
    {focusRowId}
    canEdit={canEdit()}
    renameable={auth.role === "admin"}
    {selected}
    onsave={saveCell}
    onselect={toggleSelect}
    onduplicate={duplicateRow}
    ondelete={deleteRow}
    onrenamecol={askRenameColumn}
    emptyText={q ? "No rows match the search." : "No rows yet 🌱"}
  />
  {#if canEdit()}
    {#if selected.size > 0}
      <div class="fixed inset-x-4 bottom-20 z-30 flex items-center gap-2 rounded-xl border border-zinc-200 bg-white p-2 shadow-lg md:hidden">
        <span class="flex-1 pl-2 text-sm font-medium">{selected.size} selected</span>
        <button
          class="rounded-lg px-2.5 py-1.5 text-sm text-zinc-500 hover:bg-zinc-100"
          onclick={() => selected.clear()}
        >Clear</button>
        <button
          class="flex items-center gap-1.5 rounded-lg bg-red-600 px-3 py-1.5 text-sm font-medium text-white"
          onclick={deleteSelected}
        ><Trash class="size-3.5" /> Delete</button>
      </div>
    {:else}
      <button
        class="bg-brand fixed right-4 bottom-20 z-30 flex size-12 items-center justify-center rounded-full text-brand-on shadow-lg md:hidden"
        title="Add row"
        onclick={addRow}
      ><Plus class="size-5" /></button>
    {/if}
  {/if}
  <Pagination.Root count={total} perPage={PAGE_SIZE} bind:page>
    {#snippet children({ currentPage })}
      <div class="mt-2 flex items-center gap-3">
        <Pagination.PrevButton class="rounded-lg border border-zinc-300 bg-white p-1.5 hover:bg-zinc-50 disabled:opacity-40" title="Previous page">
          <ChevronLeft class="size-4" />
        </Pagination.PrevButton>
        <span class="flex items-center gap-1.5 text-sm text-zinc-400">
          <Rows3 class="size-3.5" />
          {total} rows · page
          <input
            class="w-12 rounded-md border border-zinc-300 px-1.5 py-0.5 text-center text-sm text-zinc-900"
            type="number"
            min="1"
            max={Math.max(1, Math.ceil(total / PAGE_SIZE))}
            value={currentPage}
            onchange={(e) => {
              const v = parseInt(e.target.value, 10);
              const max = Math.max(1, Math.ceil(total / PAGE_SIZE));
              if (v >= 1 && v <= max) page = v;
              else e.target.value = currentPage;
            }}
          />
          / {Math.max(1, Math.ceil(total / PAGE_SIZE))}
        </span>
        <Pagination.NextButton class="rounded-lg border border-zinc-300 bg-white p-1.5 hover:bg-zinc-50 disabled:opacity-40" title="Next page">
          <ChevronRight class="size-4" />
        </Pagination.NextButton>
      </div>
    {/snippet}
  </Pagination.Root>
{:else}
  <div class="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm">
    <GenArt seed="nori-start" variant={1} w={640} h={120} class="mb-3 h-20 w-full rounded-md border border-zinc-200 object-cover" />
    <p class="flex items-center gap-2 text-zinc-500">
      <FileSpreadsheet class="size-4" />
      Import an Excel file (.xlsx) to create your first table.
    </p>
  </div>
{/if}

<PromptDialog
  bind:open={promptOpen}
  bind:value={promptValue}
  withType={promptWithType}
  bind:colType={promptColType}
  title={promptTitle}
  label={promptLabel}
  onsubmit={(v, t) => promptSubmit?.(v, t)}
/>

<AddColumnDialog bind:open={addColumnOpen} onadded={loadRows} />

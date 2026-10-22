<script>
  import { Pagination, DropdownMenu } from "bits-ui";
  import { SvelteSet } from "svelte/reactivity";
  import Upload from "@lucide/svelte/icons/upload";
  import Download from "@lucide/svelte/icons/download";
  import Plus from "@lucide/svelte/icons/plus";
  import Trash2 from "@lucide/svelte/icons/trash-2";
  import ChevronLeft from "@lucide/svelte/icons/chevron-left";
  import ChevronRight from "@lucide/svelte/icons/chevron-right";
  import Search from "@lucide/svelte/icons/search";
  import Rows3 from "@lucide/svelte/icons/rows-3";
  import FileSpreadsheet from "@lucide/svelte/icons/file-spreadsheet";
  import Ellipsis from "@lucide/svelte/icons/ellipsis";
  import ArrowUpDown from "@lucide/svelte/icons/arrow-up-down";
  import ArrowRight from "@lucide/svelte/icons/arrow-right";
  import CircleHelp from "@lucide/svelte/icons/circle-help";
  import X from "@lucide/svelte/icons/x";
  import Pencil from "@lucide/svelte/icons/pencil";
  import ChevronDown from "@lucide/svelte/icons/chevron-down";
  import { api } from "./api.js";
  import { auth, canEdit } from "./auth.svelte.js";
  import { tables, refreshTables, openTable, closeTable } from "./tables.svelte.js";
  import { showToast } from "./toast.svelte.js";
  import { askConfirm } from "./confirm.svelte.js";
  import Grid from "./Grid.svelte";
  import RecordCards from "./RecordCards.svelte";
  import AddColumnDialog from "./AddColumnDialog.svelte";
  import PromptDialog from "./PromptDialog.svelte";
  import CopyButton from "./CopyButton.svelte";
  import GenArt from "./GenArt.svelte";

  let { onimport, ongoto } = $props();

  const PAGE_SIZE = 50;
  // "select all matching" stops here: deletes run one request per row
  const MAX_SELECT_ALL = 10000;

  const menuItem =
    "flex cursor-pointer items-center gap-2 px-3 py-2 text-sm data-highlighted:bg-zinc-100";
  const menuItemDanger = menuItem + " text-red-600";
  const chipCls = "flex shrink-0 items-center gap-1 rounded-full bg-brand px-2.5 py-1 text-xs text-brand-on";
  const btnCls =
    "flex items-center gap-1.5 rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50 disabled:opacity-50";

  let rows = $state([]);
  let columns = $state([]);
  let types = $state([]);
  let total = $state(0);
  // searchText is what the box shows (both boxes share it); q is the
  // debounced value the query runs on - clearing a chip clears both
  let searchText = $state("");
  let q = $state("");
  let sort = $state("");
  let order = $state("asc");
  let page = $state(1);
  let addColumnOpen = $state(false);
  let focusRowId = $state(null);
  let loading = $state(true);
  let showHelp = $state(false);
  // $state doesn't wrap built-ins like Set - SvelteSet is the reactive one.
  const selected = new SvelteSet();
  let allMatching = $state(false);
  let selectingAll = $state(false);

  // a freshly added row stays a draft until something is typed into it;
  // left empty (Esc, click away, sheet closed) it is removed again
  let draftId = null;
  let gridEditing = $state(false);
  let sheetEditing = $state(false);

  // save indicator: cell edits apply instantly in the UI, this says when
  // the server has them
  let pendingSaves = $state(0);
  let saveState = $state(""); // "" | saving | saved | error
  let saveTimer;

  let exporting = $state(false);
  let deleting = $state(false);

  let seq = 0;
  let searchTimer;

  // every table is an API resource; the strip under the toolbar keeps that
  // visible where the data lives. The curl carries a placeholder, never the
  // admin's own session token: snippets get pasted into chats and tickets.
  const curlText = $derived(
    `curl "${location.origin}/api/tables/${encodeURIComponent(tables.current ?? "")}/rows?limit=10" -H "Authorization: Bearer $NORI_API_KEY"`
  );

  // Active "col=value" filter, shown as a chip next to the search box.
  const filterChip = $derived.by(() => {
    const m = q.match(/^(\S+)=(.*)$/);
    return m && columns.includes(m[1]) ? { col: m[1], val: m[2] } : null;
  });

  function queryParams() {
    const params = new URLSearchParams();
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
    return params;
  }

  // background: a quiet refresh (focus, timer) - keeps the selection and
  // the invalid-cell mark, and doesn't toast on failure
  async function loadRows({ background = false } = {}) {
    if (!background) {
      selected.clear();
      allMatching = false;
      invalidCell = null;
    }
    if (!tables.current) {
      rows = [];
      columns = [];
      types = [];
      total = 0;
      loading = false;
      return;
    }
    const my = ++seq;
    const params = queryParams();
    params.set("limit", PAGE_SIZE);
    params.set("offset", (page - 1) * PAGE_SIZE);
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
      if (my === seq && !background) showToast(err.message, "error");
    } finally {
      if (my === seq) loading = false;
    }
  }

  // Reload whenever the table, search, sort or page changes. A saved table
  // that no longer exists (stale localStorage, fresh data dir) is skipped
  // until refreshTables has validated it.
  let lastTable = null;
  $effect(() => {
    void q;
    void sort;
    void order;
    void page;
    const name = tables.current;
    if (name !== lastTable) {
      // a different table: show "Loading…", not the previous table's rows
      // or a false "No rows yet"
      lastTable = name;
      loading = true;
      rows = [];
    }
    if (!name) {
      rows = [];
      columns = [];
      total = 0;
      loading = false;
      return;
    }
    if (tables.list.length > 0 && !tables.list.some((t) => t.name === name)) {
      return;
    }
    loadRows();
  });

  // other people (and agents) edit too: refresh quietly when the tab comes
  // back into view and every 30s while visible - never mid-edit
  function backgroundRefresh() {
    if (document.visibilityState !== "visible") return;
    if (gridEditing || sheetEditing || pendingSaves > 0 || deleting || promptOpen) return;
    loadRows({ background: true });
  }
  $effect(() => {
    const timer = setInterval(backgroundRefresh, 30000);
    const onVis = () => backgroundRefresh();
    document.addEventListener("visibilitychange", onVis);
    window.addEventListener("focus", onVis);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", onVis);
      window.removeEventListener("focus", onVis);
    };
  });

  // header click cycles: ascending -> descending -> default order
  function onsort(col) {
    if (sort !== col) {
      sort = col;
      order = "asc";
    } else if (order === "asc") {
      order = "desc";
    } else {
      sort = "";
      order = "asc";
    }
    page = 1;
  }

  function setSort(col, ord) {
    sort = col;
    order = ord;
    page = 1;
  }

  $effect(() => {
    const v = searchText;
    clearTimeout(searchTimer);
    if (v === q) return;
    searchTimer = setTimeout(() => {
      q = v;
      page = 1;
    }, 200);
  });

  function clearSearch() {
    clearTimeout(searchTimer);
    searchText = "";
    q = "";
    page = 1;
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
      clearSearch();
      sort = "";
      page = Number.MAX_SAFE_INTEGER;
      focusRowId = created.id;
      draftId = created.id;
      await loadRows();
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  // a draft row the user left without typing anything is removed again
  async function checkDraft() {
    if (!draftId) return;
    const row = rows.find((r) => r.id === draftId);
    const id = draftId;
    draftId = null;
    if (!row || columns.some((c) => (row[c] ?? "") !== "")) return;
    try {
      await api(`/api/tables/${encodeURIComponent(tables.current)}/rows/${id}`, { method: "DELETE" });
      showToast("Empty row discarded");
      loadRows();
    } catch {}
  }

  function onGridEditState(on) {
    gridEditing = on;
    if (!on) checkDraft();
  }

  // ---- shared prompt dialog (rename table/column) ----
  let promptOpen = $state(false);
  let promptTitle = $state("");
  let promptLabel = $state("");
  let promptValue = $state("");
  let promptWarning = $state("");
  let promptSubmitLabel = $state("Save");
  let promptSubmit = null;

  let promptWithType = $state(false);
  let promptColType = $state("text");

  function askPrompt(title, label, value, fn, { withType = false, colType = "text", warning = "", submitLabel = "Save" } = {}) {
    promptTitle = title;
    promptLabel = label;
    promptValue = value;
    promptSubmit = fn;
    promptWithType = withType;
    promptColType = colType;
    promptWarning = warning;
    promptSubmitLabel = submitLabel;
    promptOpen = true;
  }

  // renames change the API contract: say how much traffic would break
  async function apiTrafficNote(table) {
    try {
      const usage = await api("/api/usage");
      let n = 0;
      for (const [path, count] of Object.entries(usage.byPath ?? {})) {
        const m = path.match(/\/api\/tables\/([^/]+)/);
        if (m && decodeURIComponent(m[1]) === table) n += count;
      }
      return n > 0 ? ` It had ${n.toLocaleString()} API request${n === 1 ? "" : "s"} in the last 24h.` : "";
    } catch {
      return "";
    }
  }

  // opens at once; the last-24h traffic count is filled in when it arrives
  async function askRenameTable() {
    const name = tables.current;
    const base = `Apps and agents calling /api/tables/${name} stop working until they switch to the new name.`;
    askPrompt("Rename table", "New table name", name, renameTable, { submitLabel: "Rename", warning: base });
    const traffic = await apiTrafficNote(name);
    if (traffic && promptOpen && promptTitle === "Rename table") promptWarning = base + traffic;
  }

  // throws on failure: the dialog stays open with the error inline
  async function renameTable(name) {
    await api(`/api/tables/${encodeURIComponent(tables.current)}`, {
      method: "PATCH",
      body: JSON.stringify({ name }),
    });
    openTable(name);
    await refreshTables();
    showToast(`Renamed to ${name}`);
  }

  async function renameColumn(oldCol, newCol, newType = "", convert = false) {
    const body = { name: newCol };
    if (newType) body.type = newType;
    if (convert) body.convert = true;
    await api(
      `/api/tables/${encodeURIComponent(tables.current)}/columns/${encodeURIComponent(oldCol)}`,
      { method: "PATCH", body: JSON.stringify(body) }
    );
    showToast(
      newCol !== oldCol
        ? `Renamed to ${newCol}`
        : `Column type set to ${newType}${convert ? " - values converted" : ""}`
    );
    await refreshTables();
    loadRows();
  }

  let invalidCell = $state(null);

  async function saveCell(row, col, value) {
    pendingSaves++;
    saveState = "saving";
    clearTimeout(saveTimer);
    try {
      const updated = await api(
        `/api/tables/${encodeURIComponent(tables.current)}/rows/${row.id}`,
        { method: "PATCH", body: JSON.stringify({ [col]: value }) }
      );
      Object.assign(row, updated);
      invalidCell = null;
      pendingSaves--;
      if (pendingSaves === 0 && saveState !== "error") {
        saveState = "saved";
        saveTimer = setTimeout(() => (saveState = ""), 2000);
      }
    } catch (err) {
      pendingSaves--;
      saveState = "error";
      // keep the typed value visible and mark the offending cell
      invalidCell = { id: row.id, col };
      showToast(`Not saved: ${err.message}`, "error");
    }
  }

  async function deleteRow(row) {
    if (!(await askConfirm("This row will be removed. You can undo right after.", "Delete row?"))) return;
    const values = Object.fromEntries(columns.map((c) => [c, row[c] ?? ""]));
    try {
      await api(`/api/tables/${encodeURIComponent(tables.current)}/rows/${row.id}`, {
        method: "DELETE",
      });
      const table = tables.current;
      showToast("Row deleted", "info", {
        action: { label: "Undo", run: () => restoreRow(table, values) },
      });
      loadRows();
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  // undo = the same values as a new row (it gets a new id)
  async function restoreRow(table, values) {
    try {
      await api(`/api/tables/${encodeURIComponent(table)}/rows`, {
        method: "POST",
        body: JSON.stringify(values),
      });
      showToast("Row restored");
      if (tables.current === table) loadRows();
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
      showToast("Row duplicated - added at the end");
      loadRows();
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  function toggleSelect(id, checked) {
    allMatching = false;
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
          // three answers: convert, change the type only, or back out
          const ans = await askConfirm(
            `Rewrite the existing values in "${col}" to the ${t} format? Values that don't fit stay as they are.`,
            "Convert values?",
            "Convert",
            { altLabel: "Keep values as they are", danger: false }
          );
          if (ans === false) return false;
          convert = ans === true;
        }
        await renameColumn(col, v, t, convert);
      },
      {
        withType: true,
        colType: origType,
        submitLabel: "Save",
        warning: `Renaming changes the field name every app and agent reads and writes ("${col}").`,
      }
    );
  }

  async function deleteColumn(col) {
    const ok = await askConfirm(
      `"${col}" and its values in all ${total.toLocaleString()} rows will be removed. Apps and agents reading this field will get nothing back.`,
      "Delete column?",
      "Delete column"
    );
    if (!ok) return;
    try {
      await api(`/api/tables/${encodeURIComponent(tables.current)}/columns/${encodeURIComponent(col)}`, { method: "DELETE" });
      showToast(`Deleted column ${col}`);
      if (sort === col) sort = "";
      await refreshTables();
      loadRows();
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  function toggleSelectAll(checked) {
    allMatching = false;
    for (const r of rows) {
      if (checked) selected.add(r.id);
      else selected.delete(r.id);
    }
  }

  const allOnPage = $derived(rows.length > 0 && rows.every((r) => selected.has(r.id)));

  // Gmail-style: after "select all on this page", offer every matching row
  async function selectAllMatching() {
    selectingAll = true;
    try {
      const base = queryParams();
      base.set("limit", "1000");
      for (let offset = 0; offset < Math.min(total, MAX_SELECT_ALL); offset += 1000) {
        base.set("offset", String(offset));
        const data = await api(`/api/tables/${encodeURIComponent(tables.current)}/rows?${base}`);
        for (const r of data.rows) {
          if (selected.size >= MAX_SELECT_ALL) break;
          selected.add(r.id);
        }
        if (data.rows.length < 1000) break;
      }
      allMatching = true;
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      selectingAll = false;
    }
  }

  function clearSelection() {
    selected.clear();
    allMatching = false;
  }

  // a few requests at a time, counting what actually went through, so a
  // partial failure says how many rows were removed
  async function deleteSelected() {
    const ids = [...selected];
    const n = ids.length;
    if (!(await askConfirm(`${n.toLocaleString()} row${n === 1 ? "" : "s"} will be permanently removed.`, `Delete ${n.toLocaleString()} row${n === 1 ? "" : "s"}?`))) return;
    deleting = true;
    let ok = 0;
    let firstErr = "";
    const table = tables.current;
    const queue = [...ids];
    async function worker() {
      while (queue.length) {
        const id = queue.shift();
        try {
          await api(`/api/tables/${encodeURIComponent(table)}/rows/${id}`, { method: "DELETE" });
          ok++;
        } catch (err) {
          firstErr ||= err.message;
        }
      }
    }
    await Promise.all(Array.from({ length: Math.min(5, n) }, worker));
    deleting = false;
    if (ok === n) showToast(`Deleted ${n.toLocaleString()} row${n === 1 ? "" : "s"}`);
    else showToast(`Deleted ${ok.toLocaleString()} of ${n.toLocaleString()} rows - ${n - ok} failed: ${firstErr}`, "error");
    loadRows();
  }

  // called by App after an import finishes; rows may have changed under a
  // table whose name stayed the same, so nothing else would trigger a reload
  export function reload() {
    loadRows();
  }

  async function exportXlsx() {
    exporting = true;
    const name = tables.current;
    showToast(`Preparing ${name}.xlsx…`);
    try {
      const res = await fetch(
        `/api/tables/${encodeURIComponent(name)}/export`,
        { headers: { Authorization: "Bearer " + auth.token, "X-Requested-With": "nori-dashboard" } }
      );
      if (!res.ok) throw new Error(`Export failed (${res.status})`);
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = name + ".xlsx";
      a.click();
      URL.revokeObjectURL(url);
      showToast(`Downloaded ${name}.xlsx`);
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      exporting = false;
    }
  }

  // the most destructive action in the app: type the name to confirm
  async function deleteTable() {
    const name = tables.current;
    const ok = await askConfirm(
      `"${name}" and all ${total.toLocaleString()} rows will be removed, and its API endpoint stops working. This can't be undone from the dashboard (restore from a backup).`,
      "Delete table?",
      "Delete table",
      { requireText: name }
    );
    if (!ok) return;
    try {
      await api(`/api/tables/${encodeURIComponent(name)}`, { method: "DELETE" });
      showToast(`Deleted ${name}`);
      closeTable();
      await refreshTables();
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  const emptyText = $derived(q ? "No rows match the search." : "No rows yet 🌱");
</script>

{#snippet searchBox(cls)}
  <label class="relative block {cls}">
    <span class="sr-only">Search rows</span>
    <Search class="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-zinc-500" />
    <input
      class="w-full rounded-lg border border-zinc-300 bg-white py-1.5 pr-3 pl-8 text-sm focus:border-zinc-500 focus:ring-2 focus:ring-zinc-200 focus:outline-none"
      type="search"
      placeholder="Search… or col=value"
      bind:value={searchText}
    />
  </label>
{/snippet}

{#snippet chips()}
  {#if filterChip}
    <button class={chipCls} aria-label="Clear filter {filterChip.col}={filterChip.val}" onclick={clearSearch}
    >{filterChip.col}={filterChip.val}<X class="size-3" /></button>
  {/if}
  {#if sort}
    <button class={chipCls} aria-label="Clear sort" onclick={() => setSort("", "asc")}
    >{sort} {order === "asc" ? "↑" : "↓"}<X class="size-3" /></button>
  {/if}
{/snippet}

{#snippet helpButton()}
  <button
    class="rounded-lg p-2 text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900"
    aria-label="Search help"
    aria-expanded={showHelp}
    onclick={() => (showHelp = !showHelp)}
  ><CircleHelp class="size-4" /></button>
{/snippet}

<div class="mb-3 hidden flex-wrap items-center gap-2 md:flex">
  {@render searchBox("w-56")}
  {@render helpButton()}
  {@render chips()}
  <span class="flex-1"></span>
  {#if saveState}
    <span class="text-xs {saveState === 'error' ? 'text-red-600' : 'text-zinc-500'}" aria-live="polite">
      {saveState === "saving" ? "Saving…" : saveState === "error" ? "Last change not saved" : "✓ All changes saved"}
    </span>
  {/if}
  {#if canEdit()}
    {#if selected.size > 0}
      <button class={btnCls} onclick={deleteSelected} disabled={deleting}
      ><Trash2 class="size-3.5" /> {deleting ? "Deleting…" : `Delete ${selected.size.toLocaleString()} row${selected.size === 1 ? "" : "s"}`}</button>
    {/if}
    {#if tables.current}
      <button class={btnCls} onclick={addRow}><Plus class="size-3.5" /> Add row</button>
    {/if}
  {/if}
  {@render tableMenuButton()}
</div>

{#snippet tableMenuButton()}
  <DropdownMenu.Root>
    <DropdownMenu.Trigger
      class="rounded-lg border border-zinc-300 bg-white p-2.5 text-zinc-900 hover:bg-zinc-50"
      title="Table actions"
      aria-label="Table actions"
    >
      <Ellipsis class="size-4" />
    </DropdownMenu.Trigger>
    <DropdownMenu.Portal>
      <DropdownMenu.Content class="z-50 min-w-48 rounded-md border border-zinc-200 bg-white py-1 shadow-md" align="end">
        {#if auth.role === "admin"}
          <DropdownMenu.Item class={menuItem} onSelect={() => onimport?.()}>
            <Upload class="size-3.5" /> Import xlsx…
          </DropdownMenu.Item>
        {/if}
        {#if tables.current}
          {#if auth.role === "admin"}
            <DropdownMenu.Item class={menuItem} onSelect={() => (addColumnOpen = true)}>
              <Plus class="size-3.5" /> Add column
            </DropdownMenu.Item>
            <DropdownMenu.Item class={menuItem} onSelect={askRenameTable}>
              <Pencil class="size-3.5" /> Rename table
            </DropdownMenu.Item>
          {/if}
          <DropdownMenu.Item class={menuItem} onSelect={exportXlsx} disabled={exporting}>
            <Download class="size-3.5" /> {exporting ? "Exporting…" : "Export xlsx"}
          </DropdownMenu.Item>
          {#if auth.role === "admin"}
            <DropdownMenu.Item class={menuItemDanger} onSelect={deleteTable}>
              <Trash2 class="size-3.5" /> Delete table…
            </DropdownMenu.Item>
          {/if}
        {/if}
      </DropdownMenu.Content>
    </DropdownMenu.Portal>
  </DropdownMenu.Root>
{/snippet}

<!-- Mobile: table picker (the bottom bar has one Tables tab, not one tab
     per table), then search + overflow menu -->
{#if tables.list.length > 0}
  <div class="mb-2 md:hidden">
    <DropdownMenu.Root>
      <DropdownMenu.Trigger class="flex w-full items-center justify-between gap-2 rounded-lg border border-zinc-300 bg-white px-3 py-2 text-sm font-medium" aria-label="Choose table">
        <span class="flex min-w-0 items-center gap-2">
          {#if tables.current}<GenArt seed={tables.current} variant={3} w={64} h={64} class="size-5 shrink-0 rounded-md border border-zinc-200" />{/if}
          <span class="truncate">{tables.current ?? "Choose a table"}</span>
        </span>
        <ChevronDown class="size-4 shrink-0 text-zinc-500" />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content class="z-50 max-h-80 w-[var(--bits-dropdown-menu-anchor-width)] overflow-y-auto rounded-md border border-zinc-200 bg-white py-1 shadow-md" align="start">
          {#each tables.list as t (t.name)}
            <DropdownMenu.Item class="{menuItem} {t.name === tables.current ? 'font-semibold' : ''}" onSelect={() => openTable(t.name)}>
              <GenArt seed={t.name} variant={3} w={64} h={64} class="size-5 shrink-0 rounded-md border border-zinc-200" />
              <span class="truncate">{t.name}</span>
            </DropdownMenu.Item>
          {/each}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  </div>
{/if}
<div class="mb-3 flex flex-wrap items-center gap-2 md:hidden">
  {@render searchBox("min-w-0 flex-1")}
  {@render helpButton()}
  <DropdownMenu.Root>
    <DropdownMenu.Trigger
      class="rounded-lg border border-zinc-300 bg-white p-2.5 text-zinc-900 hover:bg-zinc-50"
      title="Sort rows"
      aria-label="Sort rows"
    >
      <ArrowUpDown class="size-4" />
    </DropdownMenu.Trigger>
    <DropdownMenu.Portal>
      <DropdownMenu.Content
        class="z-50 max-h-72 min-w-44 overflow-y-auto rounded-md border border-zinc-200 bg-white py-1 shadow-md"
        align="end"
      >
        <DropdownMenu.Item class={menuItem} onSelect={() => setSort("", "asc")}>Default order</DropdownMenu.Item>
        {#each columns as col (col)}
          <DropdownMenu.Item class={menuItem} onSelect={() => setSort(col, "asc")}>{col} ↑</DropdownMenu.Item>
          <DropdownMenu.Item class={menuItem} onSelect={() => setSort(col, "desc")}>{col} ↓</DropdownMenu.Item>
        {/each}
      </DropdownMenu.Content>
    </DropdownMenu.Portal>
  </DropdownMenu.Root>
  {@render tableMenuButton()}
  {#if filterChip || sort}
    <div class="flex basis-full flex-wrap gap-2">{@render chips()}</div>
  {/if}
</div>

{#if showHelp}
  <div class="mb-3 rounded-lg border border-zinc-200 bg-white p-3 text-xs leading-5 text-zinc-600">
    <p><b>Search:</b> type any text to find rows containing it in any column.</p>
    <p><b>Filter one column:</b> type <code class="rounded bg-zinc-100 px-1">column=value</code>, e.g. <code class="rounded bg-zinc-100 px-1">{columns[0] ?? "name"}=amy</code> (contains-match, case-insensitive).</p>
    <p class="hidden md:block"><b>Edit:</b> double-click a cell, or select it and press Enter or just start typing. Arrows move, Tab/Enter save and move, Esc cancels. Row actions are behind the row number.</p>
  </div>
{/if}

{#if tables.current}
  <!-- the table's read path, kept next to the data -->
  <div class="mb-3 flex items-center gap-2 overflow-hidden rounded-lg border border-zinc-200 bg-zinc-50 px-3 py-1.5">
    <span class="shrink-0 rounded border border-zinc-200 bg-white px-1.5 py-0.5 text-[11px] font-semibold text-zinc-600">API</span>
    <code class="min-w-0 flex-1 truncate font-mono text-xs text-zinc-600" title="Read this table over the API">
      GET /api/tables/{tables.current}/rows
    </code>
    <CopyButton
      class="flex shrink-0 items-center gap-1 rounded px-1.5 py-1 text-xs text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900"
      iconClass="size-3"
      title="Copy a ready curl for this table (set $NORI_API_KEY to a key from the Access page)"
      label="curl"
      text={curlText}
    />
    <button
      class="flex shrink-0 items-center gap-1 text-xs text-zinc-600 hover:text-zinc-900"
      onclick={() => ongoto?.("api")}
    >API reference <ArrowRight class="size-3" /></button>
  </div>

  {#if canEdit() && allOnPage && total > rows.length}
    <div class="mb-2 flex flex-wrap items-center justify-center gap-x-2 rounded-lg bg-zinc-100 px-3 py-1.5 text-xs text-zinc-700">
      {#if allMatching}
        All {selected.size.toLocaleString()} matching rows are selected{selected.size >= MAX_SELECT_ALL ? ` (limit ${MAX_SELECT_ALL.toLocaleString()})` : ""}.
        <button class="font-semibold underline" onclick={clearSelection}>Clear selection</button>
      {:else}
        All {rows.length} rows on this page are selected.
        <button class="font-semibold underline disabled:opacity-50" disabled={selectingAll} onclick={selectAllMatching}
        >{selectingAll ? "Selecting…" : `Select all ${Math.min(total, MAX_SELECT_ALL).toLocaleString()} matching rows`}</button>
      {/if}
    </div>
  {/if}

  <Grid
    {rows}
    {columns}
    {types}
    {loading}
    invalid={invalidCell}
    {focusRowId}
    canEdit={canEdit()}
    renameable={auth.role === "admin"}
    {selected}
    {sort}
    {order}
    {onsort}
    onsetsort={setSort}
    onsave={saveCell}
    onselect={toggleSelect}
    onselectall={toggleSelectAll}
    onduplicate={duplicateRow}
    ondelete={deleteRow}
    onrenamecol={askRenameColumn}
    ondeletecol={deleteColumn}
    onfocused={() => (focusRowId = null)}
    oneditstate={onGridEditState}
    {emptyText}
  />
  <!-- Mobile: record cards + tap-to-edit sheet -->
  <RecordCards
    {rows}
    {columns}
    {types}
    {loading}
    {saveState}
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
    onfocused={() => (focusRowId = null)}
    oneditstate={(on) => (sheetEditing = on)}
    onsheetclosed={checkDraft}
    {emptyText}
  />
  {#if canEdit()}
    {#if selected.size > 0}
      <div class="fixed inset-x-4 bottom-20 z-30 flex items-center gap-2 rounded-xl border border-zinc-200 bg-white p-2 shadow-lg md:hidden">
        <span class="flex-1 pl-2 text-sm font-medium">{selected.size} selected</span>
        <button class="rounded-lg px-2.5 py-1.5 text-sm text-zinc-600 hover:bg-zinc-100" onclick={clearSelection}>Clear</button>
        <button
          class="flex items-center gap-1.5 rounded-lg bg-red-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-50"
          onclick={deleteSelected}
          disabled={deleting}
        ><Trash2 class="size-3.5" /> {deleting ? "Deleting…" : "Delete"}</button>
      </div>
    {:else}
      <button
        class="bg-brand fixed right-4 bottom-20 z-30 flex size-12 items-center justify-center rounded-full text-brand-on shadow-lg md:hidden"
        title="Add row"
        aria-label="Add row"
        onclick={addRow}
      ><Plus class="size-5" /></button>
    {/if}
  {/if}
  <Pagination.Root count={total} perPage={PAGE_SIZE} bind:page>
    {#snippet children({ currentPage })}
      <div class="mt-2 flex items-center gap-3">
        <Pagination.PrevButton class="rounded-lg border border-zinc-300 bg-white p-1.5 hover:bg-zinc-50 disabled:opacity-40" title="Previous page" aria-label="Previous page">
          <ChevronLeft class="size-4" />
        </Pagination.PrevButton>
        <span class="flex items-center gap-1.5 text-sm text-zinc-500">
          <Rows3 class="size-3.5" />
          {total.toLocaleString()} rows · page
          <input
            class="w-12 rounded-md border border-zinc-300 px-1.5 py-0.5 text-center text-sm text-zinc-900"
            type="number"
            aria-label="Page number"
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
        <Pagination.NextButton class="rounded-lg border border-zinc-300 bg-white p-1.5 hover:bg-zinc-50 disabled:opacity-40" title="Next page" aria-label="Next page">
          <ChevronRight class="size-4" />
        </Pagination.NextButton>
      </div>
    {/snippet}
  </Pagination.Root>
{:else}
  <div class="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm">
    <GenArt seed="nori-start" variant={1} w={640} h={120} class="mb-3 h-20 w-full rounded-md border border-zinc-200 object-cover" />
    <p class="flex items-center gap-2 text-zinc-600">
      <FileSpreadsheet class="size-4" />
      {#if tables.list.length > 0}
        Pick a table from the menu to see its rows.
      {:else}
        Import an Excel file (.xlsx) to create your first table.
      {/if}
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
  warning={promptWarning}
  submitLabel={promptSubmitLabel}
  onsubmit={(v, t) => promptSubmit?.(v, t)}
/>

<AddColumnDialog bind:open={addColumnOpen} onadded={() => { refreshTables(); loadRows(); }} />

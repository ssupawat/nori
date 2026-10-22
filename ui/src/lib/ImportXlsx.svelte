<script>
  import { Dialog } from "bits-ui";
  import { api } from "./api.js";
  import { dialogOverlay, dialogContent, btnPrimary, btnSecondary } from "./cls.js";
  import { askConfirm } from "./confirm.svelte.js";
  import { tables, refreshTables, openTable } from "./tables.svelte.js";
  import { showToast } from "./toast.svelte.js";

  // fired after a finished import so the host can navigate or reload rows
  let { onimported } = $props();

  let fileInput;
  let file = $state(null);
  // append is the safe default: replace drops tables and every edit made
  // since the last import
  let mode = $state("append");
  let open = $state(false);
  let busy = $state(false);

  export function pick() {
    fileInput.click();
  }

  // host already holds the bytes (the sample workbook): import right away
  // in replace mode - a sample seeds a demo, there is nothing to append to
  export async function loadFile(f) {
    file = f;
    mode = "replace";
    await run();
  }

  // a file from the picker or dropped onto the dashboard
  export function choose(f) {
    if (!/\.xlsx$/i.test(f.name)) {
      showToast(`${f.name} is not an .xlsx file`, "error");
      return;
    }
    file = f;
    mode = "append";
    open = true;
  }

  function chosen(e) {
    const f = e.target.files[0] ?? null;
    e.target.value = "";
    if (f) choose(f);
  }

  async function submit(e) {
    e.preventDefault();
    if (mode === "replace" && tables.list.length > 0) {
      const ok = await askConfirm(
        "Tables named like a sheet in the file are dropped and rebuilt from it. Rows added or edited in the dashboard since the last import are lost.",
        "Replace tables?",
        "Replace"
      );
      if (!ok) return;
    }
    await run();
  }

  async function run() {
    if (!file || busy) return;
    busy = true;
    const prev = tables.current;
    const form = new FormData();
    form.append("file", file);
    form.append("mode", mode);
    try {
      const res = await api("/api/import", { method: "POST", body: form });
      await refreshTables();
      // stay on the current table if it survived the import; otherwise land
      // on the first table the file brought in
      const kept = prev && tables.list.some((t) => t.name === prev);
      if (!kept && res.tables.length > 0) openTable(res.tables[0].name);
      showToast(
        `Imported: ${res.tables.map((t) => `${t.name} (${t.rows} rows)`).join(", ")}`
      );
      open = false;
      onimported?.(res);
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      busy = false;
      file = null;
    }
  }

  const optCls =
    "flex cursor-pointer items-start gap-3 rounded-lg border border-zinc-200 p-3 text-sm has-[:checked]:border-zinc-900 has-[:checked]:bg-zinc-50";
</script>

<input bind:this={fileInput} type="file" accept=".xlsx" class="hidden" onchange={chosen} />

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class={dialogOverlay} />
    <Dialog.Content
      class="{dialogContent} max-h-[85vh] w-96 max-w-[calc(100vw-2rem)] overflow-y-auto"
    >
      <Dialog.Title class="mb-1 truncate font-semibold">Import {file?.name}</Dialog.Title>
      <Dialog.Description class="mb-4 text-sm text-zinc-500">
        Every sheet in the file becomes a table.
      </Dialog.Description>
      <form onsubmit={submit} class="flex flex-col gap-2">
        <!-- replace vs append only differs when tables already exist -->
        {#if tables.list.length > 0}
        <label class={optCls}>
          <input type="radio" bind:group={mode} value="append" class="mt-1 accent-zinc-900" />
          <span>
            <span class="block font-medium">Append</span>
            <span class="block text-xs text-zinc-500">
              Rows are added to tables named like a sheet; new columns are merged in. Other sheets become new tables.
            </span>
          </span>
        </label>
        <label class={optCls}>
          <input type="radio" bind:group={mode} value="replace" class="mt-1 accent-zinc-900" />
          <span>
            <span class="block font-medium">Replace</span>
            <span class="block text-xs text-zinc-500">
              Tables named like a sheet are dropped and rebuilt from the file - edits made here are lost.
            </span>
          </span>
        </label>
        {/if}
        <div class="mt-1 flex justify-end gap-2">
          <Dialog.Close class={btnSecondary} disabled={busy}>Cancel</Dialog.Close>
          <button class={btnPrimary} disabled={busy}>{busy ? "Importing…" : "Import"}</button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

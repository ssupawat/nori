<script>
  import { Dialog } from "bits-ui";
  import { api } from "./api.js";
  import { dialogOverlay, dialogContent } from "./cls.js";
  import { tables, refreshTables, openTable } from "./tables.svelte.js";
  import { showToast } from "./toast.svelte.js";

  // fired after a finished import so the host can navigate or reload rows
  let { onimported } = $props();

  let fileInput;
  let file = $state(null);
  let mode = $state("replace");
  let open = $state(false);
  let busy = $state(false);

  export function pick() {
    fileInput.click();
  }

  // host already holds the bytes (e.g. the sample workbook): skip the picker
  export function loadFile(f) {
    file = f;
    mode = "replace";
    open = true;
  }

  function chosen(e) {
    file = e.target.files[0] ?? null;
    e.target.value = "";
    mode = "replace";
    if (file) open = true;
  }

  async function submit(e) {
    e.preventDefault();
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
      <Dialog.Description class="mb-4 text-sm text-zinc-400">
        Every sheet in the file becomes a table.
      </Dialog.Description>
      <form onsubmit={submit} class="flex flex-col gap-2">
        <label class={optCls}>
          <input type="radio" bind:group={mode} value="replace" class="mt-1 accent-zinc-900" />
          <span>
            <span class="block font-medium">Replace</span>
            <span class="block text-xs text-zinc-500">
              Tables named like a sheet are dropped and rebuilt from the file.
            </span>
          </span>
        </label>
        <label class={optCls}>
          <input type="radio" bind:group={mode} value="append" class="mt-1 accent-zinc-900" />
          <span>
            <span class="block font-medium">Append</span>
            <span class="block text-xs text-zinc-500">
              Rows are added to tables named like a sheet; new columns are merged in.
            </span>
          </span>
        </label>
        <div class="mt-1 flex justify-end gap-2">
          <Dialog.Close
            class="rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50"
          >Cancel</Dialog.Close>
          <button
            class="bg-brand rounded-lg px-3 py-1.5 text-sm font-medium text-brand-on disabled:opacity-50"
            disabled={busy}
          >Import</button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

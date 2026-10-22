<script>
  import { Dialog, Select } from "bits-ui";
  import Plus from "@lucide/svelte/icons/plus";
  import X from "@lucide/svelte/icons/x";
  import { api } from "./api.js";
  import { showToast } from "./toast.svelte.js";
  import { inputCls, dialogOverlay, dialogContent, selectTriggerSm, selectContent, selectItem } from "./cls.js";
  import ChevronDown from "@lucide/svelte/icons/chevron-down";

  let { open = $bindable(false), oncreated } = $props();

  let name = $state("");
  let busy = $state(false);
  let cols = $state([
    { name: "", type: "text" },
    { name: "", type: "text" },
  ]);

  const typeItems = [
    { value: "text", label: "text" },
    { value: "number", label: "number" },
    { value: "date", label: "date" },
    { value: "datetime", label: "datetime" },
  ];

  function addCol() {
    cols = [...cols, { name: "", type: "text" }];
  }

  function removeCol(i) {
    if (cols.length > 1) cols = cols.filter((_, j) => j !== i);
  }

  async function submit(e) {
    e.preventDefault();
    const cleaned = cols
      .map((c) => ({ name: c.name.trim(), type: c.type }))
      .filter((c) => c.name);
    if (!name.trim() || cleaned.length === 0) return;
    busy = true;
    try {
      await api("/api/tables", {
        method: "POST",
        body: JSON.stringify({ name: name.trim(), columns: cleaned }),
      });
      const created = name.trim();
      name = "";
      cols = [
        { name: "", type: "text" },
        { name: "", type: "text" },
      ];
      open = false;
      oncreated?.(created);
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      busy = false;
    }
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class={dialogOverlay} />
    <Dialog.Content
      class="{dialogContent} max-h-[85vh] w-96 max-w-[calc(100vw-2rem)] overflow-y-auto"
    >
      <Dialog.Title class="mb-1 font-semibold">New table</Dialog.Title>
      <Dialog.Description class="mb-4 text-sm text-zinc-500">
        Define the columns. You can add more later.
      </Dialog.Description>
      <form onsubmit={submit} class="flex flex-col gap-3">
        <input
          class={inputCls}
          type="text"
          placeholder="Table name"
          bind:value={name}
          autofocus
          required
        />
        <div class="flex flex-col gap-2">
          {#each cols as col, i}
            <div class="flex items-center gap-2">
              <input
                class="{inputCls} flex-1"
                type="text"
                placeholder="Column {i + 1}"
                aria-label="Column {i + 1} name"
                bind:value={col.name}
              />
              <Select.Root type="single" bind:value={col.type}>
                <Select.Trigger class="{selectTriggerSm}" aria-label="Column {i + 1} type">
                  <span class="truncate">{col.type}</span>
                  <ChevronDown class="size-3.5 shrink-0 text-zinc-500" />
                </Select.Trigger>
                <Select.Portal>
                  <Select.Content class={selectContent}>
                    {#each typeItems as t (t.value)}
                      <Select.Item
                        value={t.value}
                        class={selectItem}
                      >{t.label}</Select.Item>
                    {/each}
                  </Select.Content>
                </Select.Portal>
              </Select.Root>
              <button
                type="button"
                class="rounded p-1.5 text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900 disabled:opacity-30"
                title="Remove column"
                aria-label="Remove column {i + 1}"
                disabled={cols.length === 1}
                onclick={() => removeCol(i)}
              >
                <X class="size-4" />
              </button>
            </div>
          {/each}
          <button
            type="button"
            class="flex items-center gap-1 self-start text-sm text-zinc-500 hover:text-zinc-900"
            onclick={addCol}
          ><Plus class="size-3.5" /> Add column</button>
        </div>
        <div class="mt-1 flex justify-end gap-2">
          <Dialog.Close
            class="rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50"
          >Cancel</Dialog.Close>
          <button
            class="bg-brand rounded-lg px-3 py-1.5 text-sm font-medium text-brand-on disabled:opacity-50"
            disabled={busy}
          >{busy ? "Creating…" : "Create table"}</button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

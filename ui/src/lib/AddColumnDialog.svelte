<script>
  import { Dialog, Select } from "bits-ui";
  import Columns3 from "@lucide/svelte/icons/columns-3";
  import { api } from "./api.js";
  import { tables } from "./tables.svelte.js";
  import { showToast } from "./toast.svelte.js";
  import { inputCls, dialogOverlay, dialogContent, selectTrigger, selectContent, selectItem } from "./cls.js";
  import ChevronDown from "@lucide/svelte/icons/chevron-down";

  let { open = $bindable(false), onadded } = $props();
  let name = $state("");
  let colType = $state("text");
  let busy = $state(false);

  async function submit(e) {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return;
    busy = true;
    try {
      await api(`/api/tables/${encodeURIComponent(tables.current)}/columns`, {
        method: "POST",
        body: JSON.stringify({ name: trimmed, type: colType }),
      });
      name = "";
      colType = "text";
      open = false;
      showToast(`Column "${trimmed}" added`);
      onadded?.();
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
    <Dialog.Content class={dialogContent}>
      <Dialog.Title class="mb-1 flex items-center gap-2 font-semibold">
        <Columns3 class="size-4" /> Add column
      </Dialog.Title>
      <Dialog.Description class="mb-4 text-sm text-zinc-400">
        Existing rows get an empty value for the new column.
      </Dialog.Description>
      <form onsubmit={submit} class="flex flex-col gap-4">
        <input
          class={inputCls}
          type="text"
          placeholder="Column name"
          bind:value={name}
          required
        />
        <Select.Root type="single" bind:value={colType}>
          <Select.Trigger class="{selectTrigger} w-full">
            <span class="truncate">{colType}</span>
            <ChevronDown class="size-3.5 shrink-0 text-zinc-400" />
          </Select.Trigger>
          <Select.Portal>
            <Select.Content class={selectContent}>
              <Select.Item value="text" class={selectItem}>
                <div class="font-medium">text</div>
                <div class="text-xs text-zinc-500">any value</div>
              </Select.Item>
              <Select.Item value="number" class={selectItem}>
                <div class="font-medium">number</div>
                <div class="text-xs text-zinc-500">sorted numerically</div>
              </Select.Item>
              <Select.Item value="date" class={selectItem}>
                <div class="font-medium">date</div>
                <div class="text-xs text-zinc-500">YYYY-MM-DD</div>
              </Select.Item>
              <Select.Item value="datetime" class={selectItem}>
                <div class="font-medium">datetime</div>
                <div class="text-xs text-zinc-500">stored as UTC, shown local</div>
              </Select.Item>
            </Select.Content>
          </Select.Portal>
        </Select.Root>
        <div class="flex justify-end gap-2">
          <Dialog.Close
            class="rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50"
          >Cancel</Dialog.Close>
          <button
            class="bg-brand rounded-lg px-3 py-1.5 text-sm font-medium text-brand-on disabled:opacity-50"
            disabled={busy}
          >Add</button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

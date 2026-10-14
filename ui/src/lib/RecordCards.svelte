<script>
  import { Checkbox, Dialog } from "bits-ui";
  import Check from "@lucide/svelte/icons/check";
  import Minus from "@lucide/svelte/icons/minus";
  import Copy from "@lucide/svelte/icons/copy";
  import Trash2 from "@lucide/svelte/icons/trash-2";
  import Inbox from "@lucide/svelte/icons/inbox";
  import Pencil from "@lucide/svelte/icons/pencil";
  import ChevronRight from "@lucide/svelte/icons/chevron-right";
  import X from "@lucide/svelte/icons/x";
  import { utcToLocalInput, localInputToUtc } from "./fmt.js";
  import { dialogOverlay } from "./cls.js";

  let {
    rows,
    columns,
    types = [],
    invalid = null,
    focusRowId = null,
    canEdit,
    renameable = false,
    selected,
    onsave,
    onselect,
    onduplicate,
    ondelete,
    onrenamecol,
    emptyText = "No rows yet 🌱",
  } = $props();

  const checkboxCls =
    "mt-0.5 flex size-4 shrink-0 cursor-pointer items-center justify-center rounded border border-zinc-400 bg-white transition-colors data-[state=checked]:border-brand data-[state=checked]:bg-brand data-[state=indeterminate]:border-brand data-[state=indeterminate]:bg-brand";

  const inputCls = (bad) =>
    "w-full rounded-lg border bg-white px-3 py-2 text-sm text-zinc-900 focus:outline-none " +
    (bad ? "border-red-400 outline-1 outline-red-400" : "border-zinc-300 focus:border-zinc-500");

  // first column is the card title, the next two become preview lines
  const previewCols = $derived(columns.slice(1, 3));

  function titleOf(row) {
    const v = row[columns[0]];
    return v ? String(v) : "(untitled)";
  }

  // bottom sheet: tap a card to view/edit every field
  let sheetOpen = $state(false);
  let sheetRow = $state(null);

  function openSheet(row) {
    sheetRow = row;
    sheetOpen = true;
  }

  function save(row, col, e) {
    const value = e.currentTarget.value;
    if (value !== (row[col] ?? "")) onsave(row, col, value);
  }

  // mirror the grid: after add-row, scroll the new card into view and flash it
  let highlightId = $state(null);
  $effect(() => {
    if (!focusRowId || rows.length === 0) return;
    const row = rows.find((r) => r.id === focusRowId);
    if (!row) return;
    const el = document.querySelector(`[data-row-id="${focusRowId}"]`);
    el?.scrollIntoView({ block: "center" });
    highlightId = focusRowId;
    const id = focusRowId;
    setTimeout(() => {
      if (highlightId === id) highlightId = null;
    }, 1800);
  });
</script>

{#if rows.length === 0}
  <div class="rounded-lg border border-zinc-200 bg-white px-3 py-10 text-center text-zinc-500 shadow-sm md:hidden">
    <Inbox class="mx-auto mb-1 size-7 text-zinc-300" />
    {emptyText}
  </div>
{:else}  <div class="flex flex-col gap-2 md:hidden">
    {#each rows as row, i (row.id)}
      <div
        data-row-id={row.id}
        class="flex items-start gap-3 rounded-xl border border-zinc-200 bg-white p-3 shadow-sm transition-colors active:bg-zinc-50 {highlightId === row.id ? 'bg-amber-50/70' : ''}"
        role="button"
        tabindex="0"
        onclick={() => openSheet(row)}
        onkeydown={(e) => e.key === "Enter" && openSheet(row)}
      >
        {#if canEdit}
          <Checkbox.Root
            class={checkboxCls}
            checked={selected.has(row.id)}
            onclick={(e) => e.stopPropagation()}
            onCheckedChange={(c) => onselect(row.id, c === true)}
          >
            {#snippet children({ checked, indeterminate })}
              {#if indeterminate}
                <Minus class="size-3 text-white" />
              {:else if checked}
                <Check class="size-3 text-white" />
              {/if}
            {/snippet}
          </Checkbox.Root>
        {/if}
        <div class="min-w-0 flex-1">
          <div class="truncate text-sm font-medium text-zinc-900">{titleOf(row)}</div>
          {#each previewCols as col (col)}
            {@const v = row[col]}
            {@const tp = types[columns.indexOf(col)]}
            {#if v}
              <div class="mt-0.5 truncate text-xs text-zinc-500 {tp === 'number' ? 'tabular-nums' : ''}">
                <span class="text-zinc-400">{col}</span> {v}
              </div>
            {/if}
          {/each}
        </div>
        <div class="flex shrink-0 items-center gap-1 self-center text-zinc-300">
          <span class="font-mono text-xs text-zinc-400 tabular-nums">#{i + 1}</span>
          <ChevronRight class="size-4" />
        </div>
      </div>
    {/each}
  </div>
{/if}

<Dialog.Root bind:open={sheetOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="anim-fade {dialogOverlay}" />
    <Dialog.Content
      class="anim-slide-up fixed inset-x-0 bottom-0 z-50 max-h-[85vh] overflow-y-auto rounded-t-2xl bg-white p-4 pb-6 shadow-2xl outline-none"
    >
      {#if sheetRow}
        <div class="mb-3 flex items-start justify-between gap-2">
          <Dialog.Title class="min-w-0 truncate text-base font-bold text-zinc-900">
            {titleOf(sheetRow)}
          </Dialog.Title>
          <Dialog.Close
            class="rounded-lg p-1.5 text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900"
            title="Close"
          ><X class="size-5" /></Dialog.Close>
        </div>
        <div class="flex flex-col gap-3">
          {#each columns as col, j (col)}
            {@const bad = invalid && invalid.id === sheetRow.id && invalid.col === col}
            <label class="block">
              <span class="mb-1 flex items-center gap-1 text-xs font-medium text-zinc-500">
                {col}
                {#if renameable}
                  <button
                    class="text-zinc-400 hover:text-zinc-900"
                    title="Rename column"
                    onclick={() => {
                      sheetOpen = false;
                      onrenamecol?.(col);
                    }}
                  ><Pencil class="size-3" /></button>
                {/if}
              </span>
              {#if types[j] === "date"}
                <input
                  type="date"
                  class={inputCls(bad)}
                  value={sheetRow[col] ?? ""}
                  onchange={(e) => save(sheetRow, col, e)}
                  readonly={!canEdit}
                />
              {:else if types[j] === "datetime"}
                <input
                  type="datetime-local"
                  class={inputCls(bad)}
                  value={utcToLocalInput(sheetRow[col])}
                  onchange={(e) => save(sheetRow, col, localInputToUtc(e.currentTarget.value))}
                  readonly={!canEdit}
                />
              {:else}
                <input
                  class={inputCls(bad)}
                  value={sheetRow[col] ?? ""}
                  inputmode={types[j] === "number" ? "decimal" : undefined}
                  onchange={(e) => save(sheetRow, col, e)}
                  readonly={!canEdit}
                />
              {/if}
            </label>
          {/each}
        </div>
        {#if canEdit}
          <div class="mt-4 flex items-center gap-2 border-t border-zinc-200 pt-3">
            <button
              class="flex items-center gap-1.5 rounded-lg border border-zinc-300 px-3 py-2 text-sm text-zinc-700 hover:bg-zinc-50"
              onclick={() => {
                sheetOpen = false;
                onduplicate(sheetRow);
              }}
            ><Copy class="size-3.5" /> Duplicate</button>
            <button
              class="ml-auto flex items-center gap-1.5 rounded-lg bg-red-600 px-3 py-2 text-sm font-medium text-white hover:bg-red-700"
              onclick={() => {
                sheetOpen = false;
                ondelete(sheetRow);
              }}
            ><Trash2 class="size-3.5" /> Delete</button>
          </div>
        {/if}
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

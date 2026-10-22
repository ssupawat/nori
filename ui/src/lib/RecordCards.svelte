<script>
  import { Checkbox, Dialog } from "bits-ui";
  import Check from "@lucide/svelte/icons/check";
  import Minus from "@lucide/svelte/icons/minus";
  import CopyPlus from "@lucide/svelte/icons/copy-plus";
  import Trash2 from "@lucide/svelte/icons/trash-2";
  import Inbox from "@lucide/svelte/icons/inbox";
  import ChevronRight from "@lucide/svelte/icons/chevron-right";
  import X from "@lucide/svelte/icons/x";
  import { utcToLocalInput, localInputToUtc, sameInstant, dtDisplay } from "./fmt.js";
  import { dialogOverlay } from "./cls.js";

  let {
    rows,
    columns,
    types = [],
    invalid = null,
    focusRowId = null,
    loading = false,
    saveState = "",
    canEdit,
    selected,
    onsave,
    onselect,
    onduplicate,
    ondelete,
    onrenamecol,
    onfocused,
    oneditstate,
    onsheetclosed,
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

  // reports whether the sheet is open
  $effect(() => {
    oneditstate?.(sheetOpen);
  });

  // Closing blurs the focused field first; its change handler saves it.
  // Both close paths end here: onOpenChange (Esc, overlay, ×) and Done.
  function closeSheet() {
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
    sheetOpen = false;
    sheetClosed();
  }

  function onSheetOpenChange(o) {
    if (o) return;
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
    sheetClosed();
  }

  function sheetClosed() {
    const row = sheetRow;
    // runs after the blur's change handler
    setTimeout(() => onsheetclosed?.(row));
  }

  function save(row, col, value) {
    if (value === (row[col] ?? "")) return;
    row[col] = value;
    onsave(row, col, value);
  }

  function saveDatetime(row, col, local) {
    const utc = localInputToUtc(local);
    if (sameInstant(utc, row[col])) return;
    row[col] = utc;
    onsave(row, col, utc);
  }

  // mirror the grid: after add-row, scroll the new card into view and flash it
  let highlightId = $state(null);
  $effect(() => {
    if (!focusRowId || rows.length === 0) return;
    const row = rows.find((r) => r.id === focusRowId);
    if (!row) return;
    const el = document.querySelector(`[data-row-id="${focusRowId}"]`);
    if (!el || el.offsetParent === null) return; // cards hidden (desktop)
    el.scrollIntoView({ block: "center" });
    highlightId = focusRowId;
    const id = focusRowId;
    onfocused?.();
    if (canEdit) openSheet(row);
    setTimeout(() => {
      if (highlightId === id) highlightId = null;
    }, 1800);
  });
</script>

{#if rows.length === 0}
  <div class="rounded-lg border border-zinc-200 bg-white px-3 py-10 text-center text-zinc-500 shadow-sm md:hidden">
    {#if loading}
      Loading rows…
    {:else}
      <Inbox class="mx-auto mb-1 size-7 text-zinc-300" />
      {emptyText}
    {/if}
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
            aria-label="Select {titleOf(row)}"
            checked={selected.has(row.id)}
            onclick={(e) => e.stopPropagation()}
            onCheckedChange={(c) => onselect(row.id, c === true)}
          >
            {#snippet children({ checked, indeterminate })}
              {#if indeterminate}
                <Minus class="size-3 text-brand-on" />
              {:else if checked}
                <Check class="size-3 text-brand-on" />
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
                <span class="text-zinc-500">{col}</span> {tp === "datetime" ? dtDisplay(v) : v}
              </div>
            {/if}
          {/each}
        </div>
        <div class="flex shrink-0 items-center gap-1 self-center text-zinc-300">
          <span class="text-xs text-zinc-500 tabular-nums">#{i + 1}</span>
          <ChevronRight class="size-4" />
        </div>
      </div>
    {/each}
  </div>
{/if}

<Dialog.Root bind:open={sheetOpen} onOpenChange={onSheetOpenChange}>
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
          {#if saveState}
            <span class="ml-auto shrink-0 self-center text-xs text-zinc-500" aria-live="polite">
              {saveState === "saving" ? "Saving…" : saveState === "error" ? "Not saved" : "✓ Saved"}
            </span>
          {/if}
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

              </span>
              {#if types[j] === "date"}
                <input
                  type="date"
                  class={inputCls(bad)}
                  value={sheetRow[col] ?? ""}
                  onchange={(e) => save(sheetRow, col, e.currentTarget.value)}
                  readonly={!canEdit}
                />
              {:else if types[j] === "datetime"}
                <input
                  type="datetime-local"
                  step="1"
                  class={inputCls(bad)}
                  value={utcToLocalInput(sheetRow[col])}
                  onchange={(e) => saveDatetime(sheetRow, col, e.currentTarget.value)}
                  readonly={!canEdit}
                />
              {:else}
                <input
                  class={inputCls(bad)}
                  value={sheetRow[col] ?? ""}
                  inputmode={types[j] === "number" ? "decimal" : undefined}
                  onchange={(e) => save(sheetRow, col, e.currentTarget.value)}
                  readonly={!canEdit}
                />
              {/if}
            </label>
          {/each}
        </div>
        {#if canEdit}
          <div class="mt-4 flex flex-wrap items-center gap-2 border-t border-zinc-200 pt-3">
            <button
              class="flex items-center gap-1.5 rounded-lg border border-zinc-300 px-3 py-2 text-sm text-zinc-700 hover:bg-zinc-50"
              onclick={() => {
                sheetOpen = false;
                onduplicate(sheetRow);
              }}
            ><CopyPlus class="size-3.5" /> Duplicate</button>
            <button
              class="ml-auto flex items-center gap-1.5 rounded-lg bg-red-600 px-3 py-2 text-sm font-medium text-[#fff] hover:bg-red-700"
              onclick={() => {
                sheetOpen = false;
                ondelete(sheetRow);
              }}
            ><Trash2 class="size-3.5" /> Delete</button>
            <button
              class="bg-brand w-full rounded-lg px-3 py-2 text-sm font-medium text-brand-on"
              onclick={closeSheet}
            >Done</button>
          </div>
        {:else}
          <button class="mt-4 w-full rounded-lg border border-zinc-300 px-3 py-2 text-sm" onclick={closeSheet}>Close</button>
        {/if}
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

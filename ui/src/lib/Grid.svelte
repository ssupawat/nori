<script>
  import { Checkbox, ContextMenu } from "bits-ui";
  import Check from "@lucide/svelte/icons/check";
  import Minus from "@lucide/svelte/icons/minus";
  import Copy from "@lucide/svelte/icons/copy";
  import Trash2 from "@lucide/svelte/icons/trash-2";
  import Inbox from "@lucide/svelte/icons/inbox";
  import Pencil from "@lucide/svelte/icons/pencil";
  import Hash from "@lucide/svelte/icons/hash";
  import CalendarDays from "@lucide/svelte/icons/calendar-days";
  import Clock from "@lucide/svelte/icons/clock";
  import ChevronUp from "@lucide/svelte/icons/chevron-up";
  import ChevronDown from "@lucide/svelte/icons/chevron-down";
  import ChevronRight from "@lucide/svelte/icons/chevron-right";
  import { utcToLocalInput, localInputToUtc } from "./fmt.js";

  // Cell text is set imperatively through an action: contenteditable
  // elements can't take reactive children in Svelte.
  function setCellText(node, text) {
    node.textContent = text ?? "";
  }

  let {
    rows,
    columns,
    types = [],
    invalid = null,
    focusRowId = null,
    canEdit,
    renameable = false,
    selected,
    sort,
    order,
    onsort,
    onsave,
    onselect,
    onselectall,
    onduplicate,
    ondelete,
    onrenamecol,
    emptyText = "No rows yet 🌱",
  } = $props();

  const checkboxCls =
    "flex size-4 cursor-pointer items-center justify-center rounded border border-zinc-400 bg-white transition-colors data-[state=checked]:border-brand data-[state=checked]:bg-brand data-[state=indeterminate]:border-brand data-[state=indeterminate]:bg-brand";

  const itemCls =
    "flex cursor-pointer items-center gap-2 px-3 py-1.5 text-sm text-zinc-900 select-none data-highlighted:bg-zinc-100";

  // Sheets-style cell interaction: single click selects, double-click or
  // Enter/F2 enters edit mode, Esc reverts, Enter/Tab commit and move.
  let selectedCell = $state(null); // { id, col }
  let editing = $state(null); // { id, col }

  function focusCell(rowIdx, colIdx) {
    const el = document.querySelector(`td[data-row="${rowIdx}"][data-col="${colIdx}"]`);
    if (!el) return;
    el.focus();
    selectedCell = { id: rows[rowIdx]?.id, col: columns[colIdx] };
  }

  function startEdit(row, colIdx) {
    editing = { id: row.id, col: columns[colIdx] };
  }

  // focus the contenteditable cell and put the caret at the end
  $effect(() => {
    if (!editing) return;
    const rowIdx = rows.findIndex((r) => r.id === editing.id);
    const colIdx = columns.indexOf(editing.col);
    const td = document.querySelector(`td[data-row="${rowIdx}"][data-col="${colIdx}"]`);
    if (!td) return;
    td.focus();
    const range = document.createRange();
    range.selectNodeContents(td);
    range.collapse(false);
    window.getSelection()?.removeAllRanges();
    window.getSelection()?.addRange(range);
  });

  // shared td classes so the date-input branch and the contenteditable
  // branch render identically
  function cellCls(j, col, row) {
    return (
      (j === 0 ? "sticky z-10 bg-white group-hover:bg-zinc-50 " + (canEdit ? "left-[84px]" : "left-12") : "") +
      " " +
      (types[j] === "number" ? "text-right tabular-nums " : "") +
      "max-w-90 px-3 py-1.5 break-words transition-colors focus:outline-none " +
      (canEdit ? "cursor-cell rounded-lg hover:bg-zinc-50 " : "") +
      (selectedCell && selectedCell.id === row.id && selectedCell.col === col ? "ring-1 ring-zinc-400 " : "") +
      (invalid && invalid.id === row.id && invalid.col === col ? "bg-red-50 outline-1 outline-red-400" : "")
    );
  }

  function editComplete(row, col, e) {
    const value = e.currentTarget.textContent ?? "";
    if (value !== (row[col] ?? "")) {
      row[col] = value; // optimistic: avoid flashing the old value until PATCH lands
      onsave(row, col, value);
    }
  }

  function cellKeydown(e, row, rowIdx, colIdx) {
    if (e.key === "Enter" || e.key === "F2") {
      e.preventDefault();
      startEdit(row, colIdx);
    }
  }

  // Date cells show the stored YYYY-MM-DD as plain text; tapping opens a
  // native <input type="date"> (OS picker on mobile, dropdown on desktop).
  // Datetime cells are stored as RFC3339 UTC: display and input run in the
  // browser's local zone, commits convert back to UTC for the API.
  let dateEdit = $state(null);
  let dateInputEl = $state(null);
  let dateCancelled = false;

  function dtDisplay(value) {
    const d = new Date(value);
    if (isNaN(d)) return value;
    const p = (n) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
  }

  function openDatePicker(row, col, type = "date") {
    if (!canEdit) return;
    dateCancelled = false;
    dateEdit = { id: row.id, col, type };
  }

  $effect(() => {
    if (dateInputEl && dateEdit) {
      dateInputEl.focus();
      try {
        dateInputEl.showPicker();
      } catch {}
    }
  });

  function dateCommit(row, col, e) {
    if (dateCancelled) {
      dateCancelled = false;
      return;
    }
    const value = e.currentTarget.value;
    if (dateEdit?.type === "datetime") {
      const utc = localInputToUtc(value);
      if (utc !== (row[col] ?? "")) onsave(row, col, utc);
    } else if (value !== (row[col] ?? "")) {
      onsave(row, col, value);
    }
  }

  // Enter moves down, Tab moves right (shift-Tab left, wrapping rows);
  // blur first so the commit handler runs before the move.
  function keyMove(e, rowIdx, colIdx, lastCol) {
    if (e.key !== "Enter" && e.key !== "Tab") return;
    e.preventDefault();
    e.currentTarget.blur();
    if (e.key === "Enter") {
      focusCell(rowIdx + 1, colIdx);
    } else if (e.shiftKey) {
      if (colIdx > 0) focusCell(rowIdx, colIdx - 1);
      else if (rowIdx > 0) focusCell(rowIdx - 1, lastCol);
    } else if (colIdx < lastCol) {
      focusCell(rowIdx, colIdx + 1);
    } else {
      focusCell(rowIdx + 1, 0);
    }
  }

  // When a new row is created, scroll it into view and start editing its
  // first cell right away (explicit intent, so still safe from accidents).
  let highlightId = $state(null);
  $effect(() => {
    if (!focusRowId || rows.length === 0) return;
    const idx = rows.findIndex((r) => r.id === focusRowId);
    if (idx === -1) return;
    const td = document.querySelector(`td[data-row="${idx}"][data-col="0"]`);
    if (!td) return;
    td.scrollIntoView({ block: "center" });
    selectedCell = { id: focusRowId, col: columns[0] };
    if (canEdit) startEdit(rows[idx], 0);
    highlightId = focusRowId;
    const id = focusRowId;
    setTimeout(() => {
      if (highlightId === id) highlightId = null;
    }, 1800);
  });

  function editKeydown(e, row, rowIdx, colIdx, lastCol) {
    if (e.key === "Escape") {
      // revert: restore original text so the blur handler sees no change
      e.preventDefault();
      e.currentTarget.textContent = row[columns[colIdx]] ?? "";
      e.currentTarget.blur();
      return;
    }
    keyMove(e, rowIdx, colIdx, lastCol);
  }
</script>

<!-- flex-1 min-h-0: fill the remaining page height exactly (pagination sits
     right below), and scroll internally instead of leaving dead space below. -->
<div class="hidden min-h-0 flex-1 overflow-auto rounded-lg border border-zinc-200 bg-white shadow-sm md:block">
  <table class="w-full min-w-[900px] text-sm">
    <thead>
      <tr>
        {#if canEdit}
          <th class="sticky top-0 left-0 z-20 w-9 border-b border-zinc-200 bg-zinc-50 px-2 py-1.5">
            <Checkbox.Root
              class={checkboxCls}
              checked={rows.length > 0 && rows.every((r) => selected.has(r.id))}
              indeterminate={selected.size > 0 && !rows.every((r) => selected.has(r.id))}
              onCheckedChange={(c) => onselectall(c === true)}
            >
              {#snippet children({ checked, indeterminate })}
                {#if indeterminate}
                  <Minus class="size-3 text-white" />
                {:else if checked}
                  <Check class="size-3 text-white" />
                {/if}
              {/snippet}
            </Checkbox.Root>
          </th>
        {/if}
        <th
          class="sticky top-0 z-20 w-12 border-b border-zinc-200 bg-zinc-50 px-3 py-1.5 text-left text-xs font-medium text-zinc-500 {canEdit ? 'left-9' : 'left-0'}"
        >
          #
        </th>
        {#each columns as col, j (col)}
          <th
            class="group sticky top-0 z-20 cursor-pointer border-b border-zinc-200 bg-zinc-50 px-3 py-1.5 text-left text-xs font-medium whitespace-nowrap text-zinc-500 select-none hover:text-zinc-900 {j === 0 ? (canEdit ? 'left-[84px]' : 'left-12') : ''} {types[j] === 'number' ? 'text-right' : ''} {sort === col ? 'text-zinc-900' : ''}"
            onclick={() => onsort(col)}
            title="Sort by {col}"
          >
            <span class="inline-flex items-center gap-1">
              {col}
              {#if types[j] === "number"}
                <Hash class="size-3 text-zinc-300" title="number" />
              {:else if types[j] === "datetime"}
                <Clock class="size-3 text-zinc-300" title="datetime (UTC, shown local)" />
              {:else if types[j] === "date"}
                <CalendarDays class="size-3 text-zinc-300" title="date" />
              {/if}
              {#if sort === col}
                {#if order === "asc"}<ChevronUp class="size-3.5 text-zinc-900" />{:else}<ChevronDown class="size-3.5 text-zinc-900" />{/if}
              {:else}
                <ChevronRight class="size-3 text-zinc-300 opacity-0 transition-opacity group-hover:opacity-100" />
              {/if}
              {#if renameable}
                <Pencil
                  class="size-3 text-zinc-400 opacity-0 transition-opacity hover:text-zinc-900 group-hover:opacity-100"
                  title="Rename column"
                  onclick={(e) => {
                    e.stopPropagation();
                    onrenamecol?.(col);
                  }}
                />
              {/if}
            </span>
          </th>
        {/each}
      </tr>
    </thead>
    <tbody class="divide-y divide-zinc-200">
      {#each rows as row, i (row.id)}
        <tr class="group transition-colors hover:bg-zinc-50 {highlightId === row.id ? 'bg-amber-50/70' : ''}">
          {#if canEdit}
            <td class="sticky left-0 z-10 bg-white px-2 py-1.5 group-hover:bg-zinc-50">
              <Checkbox.Root
                class={checkboxCls}
                checked={selected.has(row.id)}
                onCheckedChange={(c) => onselect(row.id, c === true)}
              >
                {#snippet children({ checked })}
                  {#if checked}<Check class="size-3 text-white" />{/if}
                {/snippet}
              </Checkbox.Root>
            </td>
          {/if}
          {#if canEdit}
            <ContextMenu.Root>
              <ContextMenu.Trigger>
                {#snippet child({ props })}
                  <td
                    {...props}
                    class="sticky left-9 z-10 cursor-context-menu bg-white px-3 py-1.5 font-mono text-xs text-zinc-500 tabular-nums select-none group-hover:bg-zinc-50"
                    title={row.id}
                  >
                    {i + 1}
                  </td>
                {/snippet}
              </ContextMenu.Trigger>
              <ContextMenu.Portal>
                <ContextMenu.Content class="z-50 min-w-36 rounded-md border border-zinc-200 bg-white py-1 shadow-md">
                  <ContextMenu.Item class={itemCls} onSelect={() => onduplicate(row)}>
                    <Copy class="size-3.5" /> Duplicate row
                  </ContextMenu.Item>
                  <ContextMenu.Item
                    class={itemCls + " text-red-600"}
                    onSelect={() => ondelete(row)}
                  >
                    <Trash2 class="size-3.5" /> Delete row
                  </ContextMenu.Item>
                </ContextMenu.Content>
              </ContextMenu.Portal>
            </ContextMenu.Root>
          {:else}
            <td class="sticky left-0 z-10 bg-white px-3 py-1.5 font-mono text-xs text-zinc-500 tabular-nums group-hover:bg-zinc-50" title={row.id}>
              {i + 1}
            </td>
          {/if}
          {#each columns as col, j (col)}
          {#if canEdit && (types[j] === "date" || types[j] === "datetime")}
            <td
              data-row={i}
              data-col={j}
              class={cellCls(j, col, row)}
              tabindex="0"
              onclick={() => (selectedCell = { id: row.id, col })}
              ondblclick={() => openDatePicker(row, col, types[j])}
              onkeydown={(e) => {
                if (e.key === "Enter" || e.key === "F2") {
                  e.preventDefault();
                  openDatePicker(row, col, types[j]);
                }
              }}
            >
              {#if dateEdit && dateEdit.id === row.id && dateEdit.col === col}
                <input
                  bind:this={dateInputEl}
                  type={types[j] === "datetime" ? "datetime-local" : "date"}
                  class="w-full bg-transparent text-sm outline-none"
                  value={types[j] === "datetime" ? utcToLocalInput(row[col]) : row[col] ?? ""}
                  onchange={(e) => dateCommit(row, col, e)}
                  onblur={(e) => {
                    dateCommit(row, col, e);
                    dateEdit = null;
                  }}
                  onkeydown={(e) => {
                    if (e.key === "Escape") {
                      dateCancelled = true;
                      dateEdit = null;
                      return;
                    }
                    keyMove(e, i, j, columns.length - 1);
                  }}
                />
              {:else}
                <span class="block">{types[j] === "datetime" ? dtDisplay(row[col]) : row[col]}</span>
              {/if}
            </td>
          {:else if editing && editing.id === row.id && editing.col === col && types[j] !== "date" && types[j] !== "datetime"}
            <td
              data-row={i}
              data-col={j}
              class={cellCls(j, col, row)}
              contenteditable="true"
              use:setCellText={row[col]}
              onblur={(e) => {
                editComplete(row, col, e);
                editing = null;
              }}
              onkeydown={(e) => editKeydown(e, row, i, j, columns.length - 1)}
            ></td>
          {:else}
            <td
              data-row={i}
              data-col={j}
              class={cellCls(j, col, row)}
              tabindex={canEdit ? "0" : undefined}
              onclick={canEdit ? () => (selectedCell = { id: row.id, col }) : undefined}
              ondblclick={canEdit ? () => startEdit(row, j) : undefined}
              onkeydown={canEdit ? (e) => cellKeydown(e, row, i, j) : undefined}
            >{row[col] ?? ""}</td>
          {/if}
          {/each}
        </tr>
      {:else}
        <tr>
          <td colspan={columns.length + (canEdit ? 2 : 1)} class="px-3 py-10 text-center text-zinc-500">
            <Inbox class="mx-auto mb-1 size-7 text-zinc-300" />
            {emptyText}
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>

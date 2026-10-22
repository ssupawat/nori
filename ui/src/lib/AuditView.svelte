<script>
  import { Select } from "bits-ui";
  import { api } from "./api.js";
  import ChevronDown from "@lucide/svelte/icons/chevron-down";
  import { selectTriggerSm, selectContent, selectItem, actionCls } from "./cls.js";
  import { tables } from "./tables.svelte.js";
  import { showToast } from "./toast.svelte.js";
  import { ago } from "./fmt.js";
  import History from "@lucide/svelte/icons/history";
  import X from "@lucide/svelte/icons/x";

  let entries = $state([]);
  let tableSel = $state("__all__");
  let rowId = $state("");
  let limit = $state("100");
  const table = $derived(tableSel === "__all__" ? "" : tableSel);
  let loading = $state(false);

  async function load() {
    loading = true;
    try {
      const p = new URLSearchParams();
      if (table) p.set("table", table);
      if (rowId) p.set("rowId", rowId);
      p.set("limit", String(limit));
      entries = await api(`/api/audit?${p}`);
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      loading = false;
    }
  }

  // tracks table / rowId / limit (read in load) and reloads on change
  $effect(() => {
    load();
  });

  // recent events read as relative time, older ones as clock time
  function when(rfc) {
    const d = new Date(rfc);
    if ((Date.now() - d.getTime()) / 1000 < 3600) return ago(rfc);
    return d.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
  }

  const cut = (v) => (v.length > 40 ? v.slice(0, 40) + "…" : String(v));

  // events that carry no values still describe themselves, so every entry
  // renders the same two-part structure
  const staticNotes = {
    delete_table: "table dropped with all its rows",
    set_password: "password changed - other sessions of this user were revoked",
    create_user: "account created",
    set_role: "role changed - applies to live sessions at once",
    login: "signed in",
    login_failed: "sign-in failed (wrong credentials)",
  };

</script>

<div class="mx-auto w-full max-w-4xl">
  <div class="mb-3">
    <h2 class="text-sm font-semibold">Audit</h2>
    <p class="text-xs text-zinc-500">Who changed what, newest first</p>
  </div>

  <div class="mb-3 flex flex-wrap items-center gap-2">
    <Select.Root type="single" bind:value={tableSel}>
      <Select.Trigger class="{selectTriggerSm} w-44" aria-label="Filter by table">
        <span class="truncate">{tableSel === "__all__" ? "All tables" : tableSel}</span>
        <ChevronDown class="size-3.5 shrink-0 text-zinc-500" />
      </Select.Trigger>
      <Select.Portal>
        <Select.Content class={selectContent}>
          <Select.Item value="__all__" class={selectItem}>All tables</Select.Item>
          {#each tables.list as tb (tb.name)}
            <Select.Item value={tb.name} class={selectItem}>{tb.name}</Select.Item>
          {/each}
        </Select.Content>
      </Select.Portal>
    </Select.Root>
    {#if rowId}
      <button
        class="flex items-center gap-1 rounded-full bg-brand px-2.5 py-1 text-xs text-brand-on"
        title="Clear row filter"
        onclick={() => (rowId = "")}
      >row {rowId} <X class="size-3" /></button>
    {/if}
    <Select.Root type="single" bind:value={limit}>
      <Select.Trigger class="{selectTriggerSm} w-20" aria-label="Number of events">
        <span class="truncate">{limit}</span>
        <ChevronDown class="size-3.5 shrink-0 text-zinc-500" />
      </Select.Trigger>
      <Select.Portal>
        <Select.Content class={selectContent}>
          {#each ["100", "500", "1000"] as n (n)}
            <Select.Item value={n} class={selectItem}>{n}</Select.Item>
          {/each}
        </Select.Content>
      </Select.Portal>
    </Select.Root>
    <button
      class="flex items-center gap-1.5 rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50 disabled:opacity-50"
      onclick={load}
      disabled={loading}
    ><History class="size-3.5" /> {loading ? "Loading…" : "Refresh"}</button>
  </div>

  <!-- two layers: the static frame insets the scroll window, so rows sliced
       while scrolling never touch the card border directly -->
  <div class="rounded-lg border border-zinc-200 bg-white p-2">
    <div class="h-[544px] overflow-y-auto">
      <ul class="divide-y divide-zinc-200">
      {#each entries as e, i (i)}
        {@const vals = e.values ?? {}}
        {@const note = staticNotes[e.action] ?? ""}
        <li class="px-4 py-2.5">
          <div class="flex flex-wrap items-center gap-2">
            <span class="rounded-full px-2 py-0.5 text-xs font-medium {actionCls(e.action)}">{e.action}</span>
            <span class="text-sm font-medium text-zinc-900">{e.table}</span>
            {#if e.rowId}
              <button
                class="font-mono text-xs text-zinc-500 hover:text-zinc-900 hover:underline"
                title="Show only this row"
                onclick={() => (rowId = e.rowId)}
              >row {e.rowId}</button>
            {/if}
            <span class="ml-auto flex shrink-0 items-center gap-2 text-xs text-zinc-500">
              {e.user}
              <span title={new Date(e.at).toLocaleString()}>{when(e.at)}</span>
            </span>
          </div>
          {#if Object.keys(vals).length > 0 || note}
            <p class="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-xs">
              {#if note}
                <span class="text-zinc-500">{note}</span>
              {/if}
              {#if e.action === "delete_row"}
                <span class="text-zinc-500">removed - full copy kept in this trail:</span>
              {/if}
              {#each Object.entries(vals) as [k, v] (k)}
                <span class="text-zinc-500">
                  <span class="text-zinc-500">{k}</span>
                  {e.action === "update_row" ? "→" : ":"} {v === "" ? '""' : cut(v)}
                </span>
              {/each}
            </p>
          {/if}
        </li>
      {:else}
        <li class="px-4 py-8 text-center text-sm text-zinc-500">{loading ? "Loading audit trail…" : `No audit events${table ? ` for ${table}` : ""}.`}</li>
      {/each}
    </ul>
      <!-- spacer: scrolled content never ends flush against the card border -->
      <div class="h-3"></div>
    </div>
  </div>
</div>

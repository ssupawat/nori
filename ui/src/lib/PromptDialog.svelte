<script>
  import { Dialog, Select } from "bits-ui";
  import { inputCls, dialogOverlay, dialogContent, selectTrigger, selectContent, selectItem } from "./cls.js";
  import ChevronDown from "@lucide/svelte/icons/chevron-down";

  let {
    open = $bindable(false),
    title = "",
    label = "",
    value = $bindable(""),
    withType = false,
    colType = $bindable("text"),
    password = false,
    onsubmit,
  } = $props();

  const typeItems = [
    { value: "text", desc: "any value" },
    { value: "number", desc: "sorted numerically" },
    { value: "date", desc: "YYYY-MM-DD" },
    { value: "datetime", desc: "stored as UTC, shown local" },
  ];

  function submit(e) {
    e.preventDefault();
    if (!value.trim()) return;
    onsubmit?.(value.trim(), colType);
    open = false;
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class={dialogOverlay} />
    <Dialog.Content class={dialogContent}>
      <Dialog.Title class="mb-1 font-semibold">{title}</Dialog.Title>
      <Dialog.Description class="mb-4 text-sm text-zinc-400">{label}</Dialog.Description>
      <form onsubmit={submit} class="flex flex-col gap-3">
        <input
          class={inputCls}
          type={password ? "password" : "text"}
          bind:value
          autofocus
          onkeydown={(e) => e.key === "Escape" && (open = false)}
        />
        {#if withType}
          <Select.Root type="single" bind:value={colType}>
            <Select.Trigger class="{selectTrigger}">
              <span class="truncate">{colType}</span>
              <ChevronDown class="size-3.5 shrink-0 text-zinc-400" />
            </Select.Trigger>
            <Select.Portal>
              <Select.Content class={selectContent}>
                {#each typeItems as t (t.value)}
                  <Select.Item
                    value={t.value}
                    class={selectItem}
                  >
                    <div class="font-medium">{t.value}</div>
                    <div class="text-xs text-zinc-500">{t.desc}</div>
                  </Select.Item>
                {/each}
              </Select.Content>
            </Select.Portal>
          </Select.Root>
        {/if}
        <div class="flex justify-end gap-2">
          <Dialog.Close
            class="rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50"
          >Cancel</Dialog.Close>
          <button
            class="bg-brand rounded-lg px-3 py-1.5 text-sm font-medium text-brand-on"
          >OK</button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

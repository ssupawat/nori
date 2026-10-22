<script>
  import { Dialog, Select } from "bits-ui";
  import { inputCls, dialogOverlay, dialogContent, selectTrigger, selectContent, selectItem, btnPrimary, btnSecondary } from "./cls.js";
  import ChevronDown from "@lucide/svelte/icons/chevron-down";
  import Eye from "@lucide/svelte/icons/eye";
  import EyeOff from "@lucide/svelte/icons/eye-off";
  import TriangleAlert from "@lucide/svelte/icons/triangle-alert";

  // onsubmit(value, colType) may return a promise: the dialog stays open
  // (input kept, error shown inline) until it resolves, and closes only on
  // success. Throw to report a failure; return false to stay open quietly
  // (the user backed out of a follow-up question).
  let {
    open = $bindable(false),
    title = "",
    label = "",
    value = $bindable(""),
    withType = false,
    colType = $bindable("text"),
    password = false,
    minLength = 0,
    submitLabel = "Save",
    warning = "",
    onsubmit,
  } = $props();

  let error = $state("");
  let busy = $state(false);
  let reveal = $state(false);

  $effect(() => {
    if (open) {
      error = "";
      reveal = false;
    }
  });

  const typeItems = [
    { value: "text", desc: "any value" },
    { value: "number", desc: "sorted numerically" },
    { value: "date", desc: "YYYY-MM-DD" },
    { value: "datetime", desc: "stored as UTC, shown local" },
  ];

  async function submit(e) {
    e.preventDefault();
    const v = value.trim();
    if (!v || busy) return;
    if (minLength && v.length < minLength) {
      error = `At least ${minLength} characters`;
      return;
    }
    busy = true;
    error = "";
    try {
      if ((await onsubmit?.(v, colType)) !== false) open = false;
    } catch (err) {
      error = err.message;
    } finally {
      busy = false;
    }
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class={dialogOverlay} />
    <Dialog.Content class="{dialogContent} w-96 max-w-[calc(100vw-2rem)]">
      <Dialog.Title class="mb-1 font-semibold">{title}</Dialog.Title>
      <Dialog.Description class="mb-4 text-sm text-zinc-500">{label}</Dialog.Description>
      <form onsubmit={submit} class="flex flex-col gap-3">
        <div class="relative">
          <input
            class="{inputCls} {password ? 'pr-10' : ''} {error ? 'border-red-400' : ''}"
            type={password && !reveal ? "password" : "text"}
            bind:value
            autofocus
            aria-invalid={error ? "true" : undefined}
            autocomplete={password ? "new-password" : "off"}
          />
          {#if password}
            <button
              type="button"
              class="absolute top-1/2 right-1.5 -translate-y-1/2 rounded p-1.5 text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900"
              aria-label={reveal ? "Hide password" : "Show password"}
              onclick={() => (reveal = !reveal)}
            >{#if reveal}<EyeOff class="size-4" />{:else}<Eye class="size-4" />{/if}</button>
          {/if}
        </div>
        {#if withType}
          <Select.Root type="single" bind:value={colType}>
            <Select.Trigger class="{selectTrigger}" aria-label="Column type">
              <span class="truncate">{colType}</span>
              <ChevronDown class="size-3.5 shrink-0 text-zinc-500" />
            </Select.Trigger>
            <Select.Portal>
              <Select.Content class={selectContent}>
                {#each typeItems as t (t.value)}
                  <Select.Item value={t.value} class={selectItem}>
                    <div class="font-medium">{t.value}</div>
                    <div class="text-xs text-zinc-500">{t.desc}</div>
                  </Select.Item>
                {/each}
              </Select.Content>
            </Select.Portal>
          </Select.Root>
        {/if}
        {#if warning}
          <p class="flex items-start gap-2 rounded-lg bg-amber-50 p-2.5 text-xs text-amber-800">
            <TriangleAlert class="mt-0.5 size-3.5 shrink-0" />
            <span>{warning}</span>
          </p>
        {/if}
        {#if error}
          <p class="text-sm text-red-600" role="alert">{error}</p>
        {/if}
        <div class="flex justify-end gap-2">
          <Dialog.Close class={btnSecondary}>Cancel</Dialog.Close>
          <button class={btnPrimary} disabled={busy}>{busy ? "Saving…" : submitLabel}</button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

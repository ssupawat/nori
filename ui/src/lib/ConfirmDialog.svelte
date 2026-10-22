<script>
  import { AlertDialog } from "bits-ui";
  import TriangleAlert from "@lucide/svelte/icons/triangle-alert";
  import { confirmState, answerConfirm } from "./confirm.svelte.js";
  import { dialogOverlay, dialogContent, inputCls, btnPrimary, btnSecondary, btnDanger } from "./cls.js";

  let typed = $state("");
  $effect(() => {
    if (confirmState.open) typed = "";
  });
  const blocked = $derived(confirmState.requireText !== "" && typed !== confirmState.requireText);
</script>

<AlertDialog.Root
  bind:open={confirmState.open}
  onOpenChange={(o) => {
    if (!o && confirmState.resolve) answerConfirm(false);
  }}
>
  <AlertDialog.Portal>
    <!-- stacked above other dialogs -->
    <AlertDialog.Overlay class={dialogOverlay.replace("z-40", "z-[55]")} />
    <AlertDialog.Content class="{dialogContent.replace('z-50', 'z-[56]')} w-[26rem] max-w-[calc(100vw-2rem)]">
      <AlertDialog.Title class="flex items-center gap-2 font-semibold">
        <TriangleAlert class="size-4 {confirmState.danger ? 'text-red-600' : ''}" />
        {confirmState.title}
      </AlertDialog.Title>
      <AlertDialog.Description class="mt-1 mb-4 text-sm text-zinc-500">
        {confirmState.message}
      </AlertDialog.Description>
      {#if confirmState.requireText}
        <label class="mb-4 block text-xs text-zinc-600">
          Type <span class="font-mono font-semibold text-zinc-900">{confirmState.requireText}</span> to confirm
          <input class="{inputCls} mt-1" bind:value={typed} autocomplete="off" />
        </label>
      {/if}
      <div class="flex flex-wrap justify-end gap-2">
        <AlertDialog.Cancel class={btnSecondary}>Cancel</AlertDialog.Cancel>
        {#if confirmState.altLabel}
          <button class={btnSecondary} onclick={() => answerConfirm("alt")}>{confirmState.altLabel}</button>
        {/if}
        <AlertDialog.Action
          class={confirmState.danger ? btnDanger : btnPrimary}
          disabled={blocked}
          onclick={() => !blocked && answerConfirm(true)}
        >{confirmState.confirmLabel}</AlertDialog.Action>
      </div>
    </AlertDialog.Content>
  </AlertDialog.Portal>
</AlertDialog.Root>

<script>
  import { AlertDialog } from "bits-ui";
  import TriangleAlert from "@lucide/svelte/icons/triangle-alert";
  import { confirmState, answerConfirm } from "./confirm.svelte.js";
  import { dialogOverlay, dialogContent } from "./cls.js";
</script>

<AlertDialog.Root
  bind:open={confirmState.open}
  onOpenChange={(o) => {
    if (!o && confirmState.resolve) answerConfirm(false);
  }}
>
  <AlertDialog.Portal>
    <AlertDialog.Overlay class={dialogOverlay} />
    <AlertDialog.Content class={dialogContent}>
      <AlertDialog.Title class="flex items-center gap-2 font-semibold">
        <TriangleAlert class="size-4" />
        {confirmState.title}
      </AlertDialog.Title>
      <AlertDialog.Description class="mt-1 mb-4 text-sm text-zinc-400">
        {confirmState.message}
      </AlertDialog.Description>
      <div class="flex justify-end gap-2">
        <AlertDialog.Cancel
          class="rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50"
        >Cancel</AlertDialog.Cancel>
        <AlertDialog.Action
          class="bg-brand rounded-lg px-3 py-1.5 text-sm text-brand-on"
          onclick={() => answerConfirm(true)}
        >{confirmState.confirmLabel}</AlertDialog.Action>
      </div>
    </AlertDialog.Content>
  </AlertDialog.Portal>
</AlertDialog.Root>

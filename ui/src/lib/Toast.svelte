<script>
  import CircleCheck from "@lucide/svelte/icons/circle-check";
  import CircleAlert from "@lucide/svelte/icons/circle-alert";
  import { fly } from "svelte/transition";
  import { toast } from "./toast.svelte.js";
</script>

{#if toast.visible}
  <div
    transition:fly={{ y: 8, duration: 160 }}
    class="fixed inset-x-0 bottom-6 z-50 mx-auto flex w-max max-w-[90vw] items-center gap-2 rounded-lg bg-brand px-4 py-2.5 text-sm text-brand-on shadow-lg"
    role="status"
  >
    {#if toast.kind === 'error'}
      <CircleAlert class="size-4 shrink-0" />
    {:else}
      <CircleCheck class="size-4 shrink-0" />
    {/if}
    <span>{toast.message}</span>
  </div>
{/if}

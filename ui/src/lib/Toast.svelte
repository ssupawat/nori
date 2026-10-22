<script>
  import CircleCheck from "@lucide/svelte/icons/circle-check";
  import CircleAlert from "@lucide/svelte/icons/circle-alert";
  import X from "@lucide/svelte/icons/x";
  import { fly } from "svelte/transition";
  import { toasts, dismissToast } from "./toast.svelte.js";
</script>

<!-- the live regions stay mounted so screen readers announce new text;
     bottom-20 on phones keeps toasts clear of the bottom tab bar, and on
     wider screens they sit bottom-right, clear of the table's pager -->
<div class="pointer-events-none fixed inset-x-0 bottom-20 z-[60] flex flex-col items-center gap-2 px-4 md:right-6 md:bottom-6 md:left-auto md:items-end md:px-0">
  <div class="contents" role="status" aria-live="polite">
    {#each toasts.filter((t) => t.kind !== "error") as t (t.id)}
      {@render toast(t)}
    {/each}
  </div>
  <div class="contents" role="alert" aria-live="assertive">
    {#each toasts.filter((t) => t.kind === "error") as t (t.id)}
      {@render toast(t)}
    {/each}
  </div>
</div>

{#snippet toast(t)}
  <div
    transition:fly={{ y: 8, duration: 160 }}
    class="pointer-events-auto flex w-max max-w-[90vw] items-center gap-2 rounded-lg px-4 py-2.5 text-sm shadow-lg {t.kind === 'error' ? 'bg-red-600 text-[#fff]' : 'bg-brand text-brand-on'}"
  >
    {#if t.kind === "error"}
      <CircleAlert class="size-4 shrink-0" />
    {:else}
      <CircleCheck class="size-4 shrink-0" />
    {/if}
    <span>{t.message}</span>
    {#if t.action}
      <button
        class="ml-1 rounded px-1.5 py-0.5 font-semibold underline underline-offset-2 hover:bg-white/15"
        onclick={() => {
          dismissToast(t.id);
          t.action.run();
        }}
      >{t.action.label}</button>
    {/if}
    {#if t.kind === "error" || t.action}
      <button class="-mr-1 rounded p-0.5 hover:bg-white/15" aria-label="Dismiss" onclick={() => dismissToast(t.id)}><X class="size-3.5" /></button>
    {/if}
  </div>
{/snippet}

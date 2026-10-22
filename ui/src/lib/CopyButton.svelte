<script>
  import Copy from "@lucide/svelte/icons/copy";
  import Check from "@lucide/svelte/icons/check";
  import { copyText } from "./clipboard.js";
  import { showToast } from "./toast.svelte.js";

  // text: the string to copy, or an async function returning it (fetched
  // on click). The button itself confirms the copy - the user is looking at
  // it - and a failure says what to do instead of failing silently.
  let {
    text,
    label = "",
    title = "Copy",
    class: cls = "",
    iconClass = "size-3.5",
    disabled = false,
    oncopied,
  } = $props();

  let copied = $state(false);
  let busy = $state(false);
  let timer;

  async function onclick() {
    busy = true;
    let value;
    try {
      value = typeof text === "function" ? await text() : text;
    } catch (err) {
      busy = false;
      showToast(err.message, "error");
      return;
    }
    const ok = value ? await copyText(value) : false;
    busy = false;
    if (!ok) {
      showToast(value ? "Couldn't copy - select the text and press Ctrl/⌘+C" : "Nothing to copy yet", "error");
      return;
    }
    copied = true;
    oncopied?.();
    clearTimeout(timer);
    timer = setTimeout(() => (copied = false), 1500);
  }
</script>

<button
  type="button"
  class={cls}
  {title}
  aria-label={label ? undefined : title}
  disabled={disabled || busy}
  {onclick}
>
  {#if copied}
    <Check class="{iconClass} text-green-600" />
  {:else}
    <Copy class={iconClass} />
  {/if}
  {#if label}<span aria-live="polite">{copied ? "Copied" : label}</span>{/if}
  {#if !label}<span class="sr-only" aria-live="polite">{copied ? "Copied" : ""}</span>{/if}
</button>

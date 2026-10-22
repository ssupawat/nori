<script>
  // Letter tile for a table: first letter on a hue derived from the name.
  import { hash } from "./art.js";

  let { name = "", class: cls = "size-5 rounded-md text-[11px]" } = $props();
  const hue = $derived(hash(name) % 360);
  const letter = $derived((name.trim()[0] ?? "?").toUpperCase());
</script>

<span
  class="table-tile inline-flex shrink-0 items-center justify-center leading-none font-semibold {cls}"
  style="--tile-h: {hue}"
  aria-hidden="true"
>{letter}</span>

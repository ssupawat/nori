<script>
  // Deterministic generative art from a seed. Same seed → same artwork;
  // used for identicons, banners and the brand mark. The palette hue
  // follows the brand color unless overridden per instance.
  import { drawArt, hueFromHex } from "./art.js";
  import { branding } from "./branding.svelte.js";

  let { seed = "nori", w = 320, h = 200, variant = null, color = null, class: cls = "" } = $props();

  let canvas;
  const hue = $derived(hueFromHex(color ?? branding.color));

  $effect(() => {
    drawArt(canvas.getContext("2d"), canvas.width, canvas.height, seed, variant, hue);
  });
</script>

<canvas bind:this={canvas} width={w} height={h} class={cls}></canvas>

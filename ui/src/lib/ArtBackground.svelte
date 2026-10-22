<script>
  // Ambient generative background: fills the viewport, redraws on resize
  // and whenever the brand changes. Seeded from the brand name so it is
  // stable, hue from the brand color, washed to ~8% so content on top
  // stays readable.
  import { drawArt, hash, hueFromHex } from "./art.js";
  import { branding } from "./branding.svelte.js";

  let canvas;
  let timer;
  const dark = window.matchMedia?.("(prefers-color-scheme: dark)");

  function paint() {
    canvas.width = window.innerWidth;
    canvas.height = window.innerHeight;
    const ctx = canvas.getContext("2d");
    drawArt(ctx, canvas.width, canvas.height, branding.name, hash(branding.name) % 3, hueFromHex(branding.color));
    // wash toward the page color
    ctx.fillStyle = dark?.matches ? "rgba(12,12,14,0.93)" : "rgba(255,255,255,0.92)";
    ctx.fillRect(0, 0, canvas.width, canvas.height);
  }

  function onResize() {
    clearTimeout(timer);
    timer = setTimeout(paint, 150);
  }

  $effect(() => {
    paint();
    window.addEventListener("resize", onResize);
    dark?.addEventListener?.("change", paint);
    return () => {
      window.removeEventListener("resize", onResize);
      dark?.removeEventListener?.("change", paint);
      clearTimeout(timer);
    };
  });
</script>

<canvas bind:this={canvas} class="fixed inset-0 -z-10 h-full w-full"></canvas>

// Deterministic generative art engine shared by GenArt and ArtBackground.
// A seed is hashed into a PRNG, so the same seed always yields the same
// artwork - used for identicons, banners and ambient backgrounds.

export function hash(str) {
  let x = 2166136261;
  for (const c of str) {
    x ^= c.charCodeAt(0);
    x = Math.imul(x, 16777619);
  }
  return x >>> 0;
}

export function mulberry32(a) {
  return () => {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const pick = (rng, arr) => arr[Math.floor(rng() * arr.length)];

const STYLES = [
  function flow(ctx, W, H, rng, palette, k) {
    const a1 = rng() * Math.PI * 2;
    const a2 = rng() * Math.PI * 2;
    const freq = 2 + rng() * 3;
    ctx.lineCap = "round";
    for (let i = 0, n = Math.floor((60 + rng() * 80) * k); i < n; i++) {
      let x = rng() * W;
      let y = rng() * H;
      ctx.strokeStyle = pick(rng, palette);
      ctx.globalAlpha = 0.55 + rng() * 0.45;
      ctx.lineWidth = (0.5 + rng() * 1.8) * Math.min(k, 3);
      ctx.beginPath();
      ctx.moveTo(x, y);
      for (let s = 0, m = 20 + rng() * 40; s < m; s++) {
        const a =
          a1 +
          Math.sin((x / W) * freq * Math.PI + a2) * Math.PI +
          Math.cos((y / H) * freq * Math.PI) * 0.9;
        x += Math.cos(a) * 3;
        y += Math.sin(a) * 3;
        ctx.lineTo(x, y);
      }
      ctx.stroke();
    }
  },
  function waves(ctx, W, H, rng, palette, k) {
    ctx.lineCap = "round";
    const amp = H * (0.04 + rng() * 0.08);
    const base = H * (0.1 + rng() * 0.2);
    const step = H / (8 + rng() * 8) / Math.max(1, k * 0.5);
    for (let y = base; y < H + amp; y += step) {
      ctx.strokeStyle = pick(rng, palette);
      ctx.globalAlpha = 0.5 + rng() * 0.5;
      ctx.lineWidth = (1 + rng() * 3) * Math.min(k, 3);
      const f = 2 + rng() * 4;
      const ph = rng() * Math.PI * 2;
      ctx.beginPath();
      for (let x = -4; x <= W + 4; x += 3) {
        const yy = y + Math.sin((x / W) * f * Math.PI + ph) * amp;
        x === -4 ? ctx.moveTo(x, yy) : ctx.lineTo(x, yy);
      }
      ctx.stroke();
    }
  },
  function rings(ctx, W, H, rng, palette, k) {
    const cx = W * (0.3 + rng() * 0.4);
    const cy = H * (0.3 + rng() * 0.4);
    const max = Math.hypot(W, H);
    for (let r = max * (0.05 + rng() * 0.1); r < max; r += max / (8 + rng() * 8) / Math.max(1, k * 0.5)) {
      ctx.strokeStyle = pick(rng, palette);
      ctx.globalAlpha = 0.4 + rng() * 0.6;
      ctx.lineWidth = (1 + rng() * 4) * Math.min(k, 3);
      ctx.beginPath();
      ctx.arc(cx, cy, r, 0, Math.PI * 2);
      ctx.stroke();
    }
  },
  function mosaic(ctx, W, H, rng, palette) {
    const n = 6 + Math.floor(rng() * 4);
    const cw = W / n;
    const ch = H / Math.max(1, Math.round((n * H) / W));
    for (let gx = 0; gx < W; gx += cw) {
      for (let gy = 0; gy < H; gy += ch) {
        if (rng() < 0.3) continue;
        ctx.globalAlpha = 0.35 + rng() * 0.65;
        ctx.fillStyle = pick(rng, palette);
        const s = Math.min(cw, ch) * (0.5 + rng() * 0.5);
        if (rng() < 0.5) {
          ctx.fillRect(gx + (cw - s) / 2, gy + (ch - s) / 2, s, s);
        } else {
          ctx.beginPath();
          ctx.arc(gx + cw / 2, gy + ch / 2, s / 2, 0, Math.PI * 2);
          ctx.fill();
        }
      }
    }
  },
];

// Draw one artwork onto a 2d context (bg: the paper color, white unless a
// dark surface asks otherwise). `variant` forces one of the four
// styles (flow / waves / rings / mosaic); otherwise the seed picks one.
// `hue` pins the palette to a base hue; otherwise the seed randomizes it.
export function drawArt(ctx, W, H, seed, variant = null, hue = null, bg = "#fff") {
  const rng = mulberry32(hash(seed));
  const style = variant ?? Math.floor(rng() * STYLES.length);
  const k = Math.max(1, 240 / Math.max(W, H));

  ctx.globalAlpha = 1;
  ctx.fillStyle = bg;
  ctx.fillRect(0, 0, W, H);

  const h0 = hue ?? rng() * 360;
  const palette = [
    `hsl(${h0} 65% ${48 + rng() * 18}%)`,
    `hsl(${(h0 + 25 + rng() * 45) % 360} 60% ${55 + rng() * 15}%)`,
    `hsl(${(h0 + 175 + rng() * 20) % 360} 55% ${45 + rng() * 18}%)`,
  ];

  STYLES[style](ctx, W, H, rng, palette, k);
  ctx.globalAlpha = 1;
}

// Base hue of a hex color, or null when it is not a parseable color or is
// pure gray (no meaningful hue).
export function hueFromHex(hex) {
  const m = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i.exec((hex ?? "").trim());
  if (!m) return null;
  let s = m[1];
  if (s.length === 3) s = [...s].map((c) => c + c).join("");
  const n = parseInt(s, 16);
  const r = ((n >> 16) & 255) / 255;
  const g = ((n >> 8) & 255) / 255;
  const b = (n & 255) / 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const d = max - min;
  if (d === 0) return null;
  const h =
    max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
  return ((h * 60) + 360) % 360;
}

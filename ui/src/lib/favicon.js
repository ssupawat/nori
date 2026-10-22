// Favicon follows the brand: the logo image when one is set, otherwise
// the deterministic art mark in mosaic style (the grid family stays
// legible at tab size). Re-applied whenever branding changes.
//
// The tab icon is clipped to a rounded square, matching the rounded-md
// marks in the dashboard (CSS rounding can't reach a favicon). The
// apple-touch-icon stays square and full-bleed: iOS applies its own mask.
import { drawArt, hueFromHex } from "./art.js";

const SIZE = 64;
const RADIUS = 14; // ~22%, the same ratio as rounded-md on the size-6 marks
const TOUCH = 180;

function setLink(rel, href, type = "image/png") {
  let link = document.querySelector(`link[rel='${rel}']`);
  if (!link) {
    link = document.createElement("link");
    link.rel = rel;
    document.head.appendChild(link);
  }
  link.href = href;
  link.type = type;
}

function canvas(size) {
  const c = document.createElement("canvas");
  c.width = size;
  c.height = size;
  return c;
}

// draw(ctx, size) paints the square; rounded clips it first
function render(size, rounded, draw) {
  const c = canvas(size);
  const ctx = c.getContext("2d");
  if (rounded) {
    ctx.beginPath();
    ctx.roundRect(0, 0, size, size, (RADIUS / SIZE) * size);
    ctx.clip();
  }
  draw(ctx, size);
  return c.toDataURL("image/png");
}

function artMark(branding) {
  const hue = hueFromHex(branding.color);
  const draw = (ctx, s) => drawArt(ctx, s, s, branding.name, 3, hue);
  setLink("icon", render(SIZE, true, draw));
  setLink("apple-touch-icon", render(TOUCH, false, draw));
}

let pending = 0;

export function applyFavicon(branding) {
  const my = ++pending;
  if (!branding.logoUrl) {
    artMark(branding);
    return;
  }
  // round the logo too, when the image host allows reading it back
  // (CORS); otherwise use it as is. A logo that fails to load falls back
  // to the art mark rather than a broken tab icon.
  const img = new Image();
  img.crossOrigin = "anonymous";
  img.onload = () => {
    if (my !== pending) return;
    // contain inside a white square, like the login page shows it
    const draw = (ctx, s) => {
      ctx.fillStyle = "#ffffff";
      ctx.fillRect(0, 0, s, s);
      const k = Math.min(s / img.width, s / img.height);
      const w = img.width * k, h = img.height * k;
      ctx.drawImage(img, (s - w) / 2, (s - h) / 2, w, h);
    };
    try {
      setLink("icon", render(SIZE, true, draw));
      setLink("apple-touch-icon", render(TOUCH, false, draw));
    } catch {
      // tainted canvas: no CORS headers on the logo host
      setLink("icon", branding.logoUrl, "");
      setLink("apple-touch-icon", branding.logoUrl, "");
    }
  };
  // a CORS refusal also lands here: load it once more without CORS - if
  // that works, the logo is fine and is used unrounded
  img.onerror = () => {
    if (my !== pending) return;
    const plain = new Image();
    plain.onload = () => {
      if (my !== pending) return;
      setLink("icon", branding.logoUrl, "");
      setLink("apple-touch-icon", branding.logoUrl, "");
    };
    plain.onerror = () => {
      if (my === pending) artMark(branding);
    };
    plain.src = branding.logoUrl;
  };
  img.src = branding.logoUrl;
}

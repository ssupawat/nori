// Favicon follows the brand: the logo image when one is set, otherwise
// the deterministic art mark in mosaic style (the grid family stays
// legible at tab size). Re-applied whenever branding changes.
import { drawArt, hueFromHex } from "./art.js";

export function applyFavicon(branding) {
  let link = document.querySelector("link[rel='icon']");
  if (!link) {
    link = document.createElement("link");
    link.rel = "icon";
    document.head.appendChild(link);
  }
  if (branding.logoUrl) {
    link.href = branding.logoUrl;
    link.type = "";
    return;
  }
  const c = document.createElement("canvas");
  c.width = 64;
  c.height = 64;
  drawArt(c.getContext("2d"), 64, 64, branding.name, 3, hueFromHex(branding.color));
  link.href = c.toDataURL("image/png");
  link.type = "image/png";
}

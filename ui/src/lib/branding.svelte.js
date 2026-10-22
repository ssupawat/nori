// White-label options served by GET /api/branding (public). Falls back to
// nori defaults until (and if) the fetch resolves; --brand drives the
// accent color used by .bg-brand utilities.
export const branding = $state({
  name: "nori",
  tagline: "import a spreadsheet, get an API",
  color: "#18181b",
  logoUrl: "",
});

// WCAG relative luminance of #rrggbb, 0 (dark) to 1 (light).
function luminance(hex) {
  const m = hex.match(/^#?([0-9a-f]{6})$/i);
  if (!m) return 0;
  const v = parseInt(m[1], 16);
  const ch = (shift) => {
    const c = ((v >> shift) & 255) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * ch(16) + 0.7152 * ch(8) + 0.0722 * ch(0);
}

// Text color over the brand accent, picked for WCAG contrast (>= 4.5:1
// against either white or near-black), so light accents flip to dark text.
function textOver(hex) {
  return luminance(hex) > 0.35 ? "#18181b" : "#ffffff";
}

export async function loadBranding() {
  try {
    // X-Requested-With marks the dashboard's own calls, which usage does not count
    const res = await fetch("/api/branding", { headers: { "X-Requested-With": "nori-dashboard" } });
    if (!res.ok) return;
    const b = await res.json();
    Object.assign(branding, b);
    document.title = b.name;
    applyBrandColor();
  } catch {
    // offline / old binary - keep defaults
  }
}

// In dark mode a near-black accent is replaced by near-white.
const darkQuery = window.matchMedia?.("(prefers-color-scheme: dark)");

function applyBrandColor() {
  let color = branding.color;
  if (darkQuery?.matches && luminance(color) < 0.03) color = "#f4f4f5";
  const root = document.documentElement;
  root.style.setProperty("--brand", color);
  root.style.setProperty("--brand-on", textOver(color));
}

darkQuery?.addEventListener?.("change", applyBrandColor);
applyBrandColor();

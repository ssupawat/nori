// Copy text to the clipboard. navigator.clipboard only exists in a secure
// context (HTTPS or localhost); self-hosted nori is often opened over plain
// http://host:port, so fall back to the legacy execCommand path there.
// Resolves true when the text was copied.
export async function copyText(text) {
  if (window.isSecureContext && navigator.clipboard) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // permission denied or document not focused: try the fallback
    }
  }
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.style.position = "fixed";
  ta.style.opacity = "0";
  // inside a modal dialog the focus trap would steal focus from a textarea
  // on <body>, so attach it next to the active element instead
  (document.activeElement?.parentElement ?? document.body).appendChild(ta);
  ta.select();
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch {}
  ta.remove();
  return ok;
}

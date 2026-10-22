// Reactive OS color scheme.
const query = window.matchMedia?.("(prefers-color-scheme: dark)");

export const scheme = $state({ dark: !!query?.matches });

query?.addEventListener?.("change", (e) => (scheme.dark = e.matches));

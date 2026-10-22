// Reactive OS color scheme, for canvas art that can't use CSS variables.
const query = window.matchMedia?.("(prefers-color-scheme: dark)");

export const scheme = $state({ dark: !!query?.matches });

query?.addEventListener?.("change", (e) => (scheme.dark = e.matches));

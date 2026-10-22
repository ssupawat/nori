export const confirmState = $state({
  open: false,
  title: "",
  message: "",
  confirmLabel: "Delete",
  altLabel: "",
  danger: true,
  requireText: "",
  resolve: null,
});

// Usage: if (await askConfirm("message", "Title")) { ... }
// The third argument relabels the confirm button (e.g. "Reset"); options:
//   danger      - red confirm button (default when the label is "Delete")
//   altLabel    - adds a middle button that resolves "alt" (e.g. "Keep values")
//   requireText - the confirm button stays disabled until this is typed
// Resolves true (confirm), "alt", or false (cancel / dismissed).
export function askConfirm(message, title = "Are you sure?", confirmLabel = "Delete", opts = {}) {
  confirmState.title = title;
  confirmState.message = message;
  confirmState.confirmLabel = confirmLabel;
  confirmState.altLabel = opts.altLabel ?? "";
  confirmState.danger = opts.danger ?? /^(delete|revoke|replace)/i.test(confirmLabel);
  confirmState.requireText = opts.requireText ?? "";
  confirmState.open = true;
  return new Promise((resolve) => {
    confirmState.resolve = resolve;
  });
}

export function answerConfirm(answer) {
  confirmState.open = false;
  const resolve = confirmState.resolve;
  confirmState.resolve = null;
  resolve?.(answer);
}

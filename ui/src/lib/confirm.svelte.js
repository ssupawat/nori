export const confirmState = $state({
  open: false,
  title: "",
  message: "",
  confirmLabel: "Delete",
  resolve: null,
});

// Usage: if (await askConfirm("message", "Title")) { ... }
// Optional third argument relabels the confirm button (e.g. "Reset").
export function askConfirm(message, title = "Are you sure?", confirmLabel = "Delete") {
  confirmState.title = title;
  confirmState.message = message;
  confirmState.confirmLabel = confirmLabel;
  confirmState.open = true;
  return new Promise((resolve) => {
    confirmState.resolve = resolve;
  });
}

export function answerConfirm(yes) {
  confirmState.open = false;
  const resolve = confirmState.resolve;
  confirmState.resolve = null;
  resolve?.(yes);
}

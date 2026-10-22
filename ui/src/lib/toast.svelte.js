// Toast queue: info fades after 3s, errors stay 8s and can be dismissed,
// a toast with an action (e.g. Undo) stays 6s. At most three show at once.
export const toasts = $state([]);

let nextId = 1;

export function showToast(message, kind = "info", { action } = {}) {
  const id = nextId++;
  toasts.push({ id, message, kind, action });
  if (toasts.length > 3) toasts.splice(0, toasts.length - 3);
  const ms = kind === "error" ? 8000 : action ? 6000 : 3000;
  setTimeout(() => dismissToast(id), ms);
  return id;
}

export function dismissToast(id) {
  const i = toasts.findIndex((t) => t.id === id);
  if (i !== -1) toasts.splice(i, 1);
}

export const toast = $state({ message: "", kind: "info", visible: false });

let timer;

export function showToast(message, kind = "info") {
  toast.message = message;
  toast.kind = kind;
  toast.visible = true;
  clearTimeout(timer);
  timer = setTimeout(() => (toast.visible = false), 3000);
}

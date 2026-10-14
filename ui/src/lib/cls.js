// Shared class strings for form controls, so every select and input in the
// dashboard looks and behaves the same (border, padding, item hover).
export const selectTrigger =
  "flex w-full items-center justify-between gap-2 rounded-lg border border-zinc-300 bg-white px-3 py-2 text-sm text-zinc-900 focus:outline-none";

// compact variant for tight rows (e.g. column editor)
export const selectTriggerSm =
  "flex items-center justify-between gap-1.5 rounded-lg border border-zinc-300 bg-white px-2 py-2 text-sm text-zinc-900 focus:outline-none";

export const selectContent =
  "z-50 min-w-40 rounded-md border border-zinc-200 bg-white py-1 shadow-md";

export const selectItem =
  "cursor-pointer px-3 py-2 text-sm data-highlighted:bg-zinc-100";

export const inputCls =
  "w-full rounded-lg border border-zinc-300 bg-white px-3 py-2 text-sm text-zinc-900 focus:outline-none";

// modal backdrop and the centered dialog frame; callers add their own
// width and scroll classes on top
export const dialogOverlay = "fixed inset-0 z-40 bg-black/40";

export const dialogContent =
  "anim-pop fixed top-1/2 left-1/2 z-50 -translate-x-1/2 -translate-y-1/2 rounded-lg bg-white p-6 shadow-lg outline-none";

// pill for audit actions: red outline for destructive, gray otherwise
export const actionCls = (a) =>
  a.startsWith("delete")
    ? "border border-red-200 text-red-600"
    : "bg-zinc-100 text-zinc-600";


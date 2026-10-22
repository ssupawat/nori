// Shared class strings for form controls, so every select and input in the
// dashboard looks and behaves the same (border, padding, item hover).
// Focus: outlines are replaced by a visible ring, never removed outright.
const focusRing = "focus:outline-none focus-visible:ring-2 focus-visible:ring-zinc-400 focus:border-zinc-500";

// no width here: callers add w-full / w-44 (a built-in w-full would win
// over theirs, since it sorts later in the generated CSS)
export const selectTrigger =
  "flex items-center justify-between gap-2 rounded-lg border border-zinc-300 bg-white px-3 py-2 text-sm text-zinc-900 " + focusRing;

// compact variant for tight rows (e.g. column editor)
export const selectTriggerSm =
  "flex items-center justify-between gap-1.5 rounded-lg border border-zinc-300 bg-white px-2 py-2 text-sm text-zinc-900 " + focusRing;

export const selectContent =
  "z-50 min-w-40 rounded-md border border-zinc-200 bg-white py-1 shadow-md";

export const selectItem =
  "cursor-pointer px-3 py-2 text-sm data-highlighted:bg-zinc-100";

export const inputCls =
  "w-full rounded-lg border border-zinc-300 bg-white px-3 py-2 text-sm text-zinc-900 focus:outline-none focus:border-zinc-500 focus:ring-2 focus:ring-zinc-200";

// buttons: primary (brand) and secondary (outlined); disabled looks disabled
export const btnPrimary =
  "bg-brand rounded-lg px-3 py-1.5 text-sm font-medium text-brand-on disabled:cursor-not-allowed disabled:opacity-50";
export const btnSecondary =
  "rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50 disabled:cursor-not-allowed disabled:opacity-50";
export const btnDanger =
  "rounded-lg bg-red-600 px-3 py-1.5 text-sm font-medium text-[#fff] hover:bg-red-700 disabled:cursor-not-allowed disabled:opacity-50";

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

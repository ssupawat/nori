<script>
  // Same pill language as the role selects in AccessView.
  let { role } = $props();

  const cls = {
    admin: "bg-brand text-brand-on",
    editor: "bg-zinc-200 text-zinc-900",
    viewer: "border border-zinc-300 text-zinc-500",
    builder: "border border-zinc-300 text-zinc-500",
  };
</script>

<span
  class="inline-flex shrink-0 items-center rounded-full px-1.5 py-0.5 text-[10px] font-semibold tracking-wide uppercase {cls[role] ?? cls.viewer}"
>{role}</span>

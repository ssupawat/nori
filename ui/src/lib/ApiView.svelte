<script>
  import { api } from "./api.js";
  import { showToast } from "./toast.svelte.js";
  import BookOpen from "@lucide/svelte/icons/book-open";
  import CopyButton from "./CopyButton.svelte";
  import { renderMarkdown } from "./md.js";

  let llms = $state("");
  const origin = location.origin;

  async function specText() {
    const spec = await api("/openapi.json");
    return typeof spec === "string" ? spec : JSON.stringify(spec, null, 2);
  }

  $effect(() => {
    api("/llms.txt")
      .then((t) => (llms = typeof t === "string" ? t : ""))
      .catch((err) => showToast(err.message, "error"));
  });

  const sectionCls = "rounded-lg border border-zinc-200 bg-white p-4";
  const btnCls =
    "flex items-center gap-1.5 rounded-lg border border-zinc-300 bg-white px-2.5 py-1.5 text-xs hover:bg-zinc-50 disabled:opacity-50";
</script>

<div class="w-full">
  <div class="mb-3">
    <h2 class="text-xl font-semibold tracking-tight text-zinc-900">API</h2>
    <p class="mt-0.5 text-sm text-zinc-500">Reference for agents and scripts - keys live on the Access page, traffic and server health on the Dashboard</p>
  </div>

  <div class={sectionCls}>
    <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
      <span class="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm font-semibold">
        <BookOpen class="size-4" /> API reference
        <span class="basis-full text-xs font-normal text-zinc-500 sm:basis-auto">llms.txt and OpenAPI 3.1, written for agents and scripts</span>
      </span>
      <span class="flex items-center gap-1.5">
        <CopyButton
          class={btnCls}
          iconClass="size-3"
          title="Copy the llms.txt handbook"
          label="llms.txt"
          text={llms}
          disabled={!llms}
        />
        <CopyButton
          class={btnCls}
          iconClass="size-3"
          title="Copy the OpenAPI 3.1 spec"
          label="OpenAPI spec"
          text={specText}
        />
      </span>
    </div>
    {#snippet urlChip(url, title)}
      <span class="inline-flex max-w-full min-w-0 items-center gap-1 rounded-md bg-zinc-100 py-0.5 pr-0.5 pl-2 align-middle">
        <code class="min-w-0 truncate font-mono text-xs text-zinc-700">{url}</code>
        <CopyButton
          class="inline-flex shrink-0 items-center rounded p-1 text-zinc-500 hover:bg-zinc-200 hover:text-zinc-900"
          iconClass="size-3"
          {title}
          text={url}
        />
      </span>
    {/snippet}
    <ol class="mb-4 flex flex-col gap-2 text-sm text-zinc-600">
      <li>1. Create an API key on the Access page and paste it to the agent.</li>
      <li class="flex flex-wrap items-center gap-x-2 gap-y-1">2. Share the base URL: {@render urlChip(origin, "Copy base URL")}</li>
      <li class="flex flex-wrap items-center gap-x-2 gap-y-1">
        3. Point the agent at {@render urlChip(`${origin}/llms.txt`, "Copy llms.txt URL")} or {@render urlChip(`${origin}/openapi.json`, "Copy OpenAPI URL")}
      </li>
    </ol>
    <!-- full handbook, no inner scroller -->
    <div class="rounded-lg bg-zinc-50 p-4 text-sm text-zinc-700">
      <div class="max-w-4xl">{#if llms}{@html renderMarkdown(llms)}{:else}<p class="text-zinc-500">Loading…</p>{/if}</div>
    </div>
  </div>
</div>

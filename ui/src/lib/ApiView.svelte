<script>
  import { api } from "./api.js";
  import { showToast } from "./toast.svelte.js";
  import BookOpen from "@lucide/svelte/icons/book-open";
  import CopyButton from "./CopyButton.svelte";

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

<div class="mx-auto w-full max-w-4xl">
  <div class="mb-3">
    <h2 class="text-sm font-semibold">API</h2>
    <p class="text-xs text-zinc-500">Reference for agents and scripts - keys live on the Access page, traffic and server health on the Dashboard</p>
  </div>

  <div class={sectionCls}>
    <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
      <span class="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm font-semibold">
        <BookOpen class="size-4" /> API reference
        <span class="basis-full text-xs font-normal text-zinc-500 sm:basis-auto">llms.txt and OpenAPI 3.1, written for agents and scripts</span>
      </span>
      <span class="flex items-center gap-1.5">
        <!-- disabled until loaded: copying the empty placeholder would
             report success with nothing on the clipboard -->
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
    <ol class="mb-3 flex flex-col gap-1 text-xs text-zinc-500">
      <li>1. Create an API key on the Access page and paste it to the agent.</li>
      <li>2. Share the base URL: <code class="rounded bg-zinc-100 px-1">{origin}</code>
        <CopyButton
          class="inline-flex min-h-6 items-center gap-1 rounded px-1.5 py-0.5 align-middle text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900"
          iconClass="size-3"
          title="Copy base URL"
          label="Copy"
          text={origin}
        />
      </li>
      <li>3. Point the agent at <code class="rounded bg-zinc-100 px-1">{origin}/llms.txt</code> or <code class="rounded bg-zinc-100 px-1">{origin}/openapi.json</code> (OpenAPI 3.1).</li>
    </ol>
    <!-- full handbook, no inner scroller: this page exists for it -->
    <pre class="rounded-lg bg-zinc-50 p-3 font-mono text-xs whitespace-pre-wrap">{llms}</pre>
  </div>
</div>

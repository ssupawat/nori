<script>
  import { api } from "./api.js";
  import { auth, setSession } from "./auth.svelte.js";
  import { branding } from "./branding.svelte.js";
  import GenArt from "./GenArt.svelte";
  import LogIn from "@lucide/svelte/icons/log-in";

  let username = $state("");
  let password = $state("");
  let error = $state("");
  let busy = $state(false);

  async function submit(e) {
    e.preventDefault();
    error = "";
    busy = true;
    try {
      const res = await api("/api/login", {
        method: "POST",
        body: JSON.stringify({ username, password }),
      });
      // dashboard is admin-only: reject non-admin sessions politely
      if (res.role !== "admin") {
        try {
          await api("/api/logout", { method: "POST", headers: { Authorization: "Bearer " + res.token } });
        } catch {}
        error = "This account is for API use only. It has no access to this dashboard.";
        return;
      }
      setSession(res);
    } catch (err) {
      error = err.message;
    } finally {
      busy = false;
    }
  }
</script>

<div class="flex min-h-screen items-center justify-center bg-zinc-50 p-4">
  <div class="flex w-full max-w-2xl overflow-hidden rounded-lg border border-zinc-200 bg-white shadow-sm">
    <div class="hidden w-1/2 md:block">
      <GenArt seed={branding.name} variant={0} w={480} h={640} class="h-full w-full object-cover" />
    </div>
    <form onsubmit={submit} class="flex flex-1 flex-col justify-center gap-3 p-8">
      <h1 class="flex items-center justify-center gap-2 text-2xl font-bold text-zinc-900">
        {#if branding.logoUrl}
          <img src={branding.logoUrl} alt={branding.name} class="size-9 rounded-lg border border-zinc-200 object-contain" />
        {:else}
          <GenArt seed={branding.name} variant={3} w={96} h={96} class="size-9 rounded-lg border border-zinc-200" />
        {/if}
        {branding.name}
      </h1>
      <p class="mb-4 text-center text-sm text-zinc-400">{branding.tagline}</p>
      <input
        class="w-full rounded-lg border border-zinc-300 bg-white px-3 py-2 text-sm focus:outline-none"
        type="text"
        placeholder="Username"
        autocomplete="username"
        bind:value={username}
        required
      />
      <input
        class="w-full rounded-lg border border-zinc-300 bg-white px-3 py-2 text-sm focus:outline-none"
        type="password"
        placeholder="Password"
        autocomplete="current-password"
        bind:value={password}
        required
      />
      <button
        class="bg-brand mt-1 flex w-full items-center justify-center gap-2 rounded-lg py-2 text-sm font-medium text-brand-on"
        disabled={busy}
      >
        <LogIn class="size-4" />
        {busy ? "Signing in…" : "Sign in"}
      </button>
      {#if error}
        <p class="text-center text-sm font-medium text-zinc-900">{error}</p>
      {/if}
    </form>
  </div>
</div>

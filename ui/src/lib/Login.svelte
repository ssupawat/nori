<script>
  import { api } from "./api.js";
  import { auth, setSession } from "./auth.svelte.js";
  import { branding } from "./branding.svelte.js";
  import { inputCls } from "./cls.js";
  import GenArt from "./GenArt.svelte";
  import LogIn from "@lucide/svelte/icons/log-in";
  import CircleAlert from "@lucide/svelte/icons/circle-alert";
  import Eye from "@lucide/svelte/icons/eye";
  import EyeOff from "@lucide/svelte/icons/eye-off";

  // after an expired session the username is kept, so only the password
  // needs typing and the dashboard reopens where it was
  let username = $state(auth.username);
  let reveal = $state(false);
  let logoBroken = $state(false);
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
        {#if branding.logoUrl && !logoBroken}
          <img src={branding.logoUrl} alt={branding.name} class="size-9 rounded-lg border border-zinc-200 object-contain" onerror={() => (logoBroken = true)} />
        {:else}
          <GenArt seed={branding.name} variant={3} w={96} h={96} class="size-9 rounded-lg border border-zinc-200" />
        {/if}
        {branding.name}
      </h1>
      <p class="mb-4 text-center text-sm text-zinc-500">{branding.tagline}</p>
      {#if auth.ended === "expired"}
        <p class="rounded-lg bg-amber-50 p-2.5 text-center text-sm text-amber-800" role="status">
          Your session expired. Sign in again to continue where you left off.
        </p>
      {/if}
      <input
        class={inputCls}
        type="text"
        aria-label="Username"
        placeholder="Username"
        autocomplete="username"
        bind:value={username}
        required
      />
      <div class="relative">
        <input
          class="{inputCls} pr-10"
          type={reveal ? "text" : "password"}
          aria-label="Password"
          placeholder="Password"
          autocomplete="current-password"
          bind:value={password}
          aria-invalid={error ? "true" : undefined}
          required
        />
        <button
          type="button"
          class="absolute top-1/2 right-1.5 -translate-y-1/2 rounded p-1.5 text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900"
          aria-label={reveal ? "Hide password" : "Show password"}
          onclick={() => (reveal = !reveal)}
        >{#if reveal}<EyeOff class="size-4" />{:else}<Eye class="size-4" />{/if}</button>
      </div>
      <button
        class="bg-brand mt-1 flex w-full items-center justify-center gap-2 rounded-lg py-2 text-sm font-medium text-brand-on disabled:cursor-wait disabled:opacity-60"
        disabled={busy}
      >
        <LogIn class="size-4" />
        {busy ? "Signing in…" : "Sign in"}
      </button>
      {#if error}
        <p class="flex items-center justify-center gap-1.5 text-center text-sm font-medium text-red-600" role="alert">
          <CircleAlert class="size-4 shrink-0" /> {error}
        </p>
      {/if}
    </form>
  </div>
</div>

<script>
  // Account page for non-admin accounts: the dashboard is admin-only, but
  // every account can change its own password here.
  import { api } from "./api.js";
  import { auth } from "./auth.svelte.js";
  import { branding } from "./branding.svelte.js";
  import { inputCls, btnPrimary, btnSecondary } from "./cls.js";
  import GenArt from "./GenArt.svelte";
  import RoleBadge from "./RoleBadge.svelte";
  import LogOut from "@lucide/svelte/icons/log-out";
  import CircleCheck from "@lucide/svelte/icons/circle-check";

  let { onsignout } = $props();

  const roleText = {
    editor: "can create, edit and delete rows in every table",
    viewer: "can read every table",
    builder: "can read table names, columns and the API docs, never rows",
  };

  let currentPw = $state("");
  let newPw = $state("");
  let confirmPw = $state("");
  let reveal = $state(false);
  let busy = $state(false);
  let error = $state("");
  let done = $state(false);
  const mismatch = $derived(confirmPw !== "" && confirmPw !== newPw);

  async function changePassword(e) {
    e.preventDefault();
    if (newPw !== confirmPw) return;
    busy = true;
    error = "";
    done = false;
    try {
      await api("/api/me/password", {
        method: "POST",
        body: JSON.stringify({ currentPassword: currentPw, password: newPw }),
      });
      currentPw = newPw = confirmPw = "";
      done = true;
    } catch (err) {
      error = err.message;
    } finally {
      busy = false;
    }
  }
</script>

<div class="flex min-h-screen items-center justify-center bg-zinc-50 p-4">
  <div class="w-full max-w-md rounded-lg border border-zinc-200 bg-white p-6 shadow-sm sm:p-8">
    <div class="mb-5 flex items-center gap-2">
      <GenArt seed={branding.name} variant={3} w={64} h={64} class="size-7 rounded-md border border-zinc-200" />
      <span class="font-bold text-zinc-900">{branding.name}</span>
    </div>

    <h1 class="text-xl font-semibold tracking-tight text-zinc-900">Your account</h1>
    <div class="mt-2 flex flex-wrap items-center gap-2 text-sm text-zinc-600">
      Signed in as <span class="font-medium text-zinc-900">{auth.username}</span>
      <RoleBadge role={auth.role} />
    </div>
    <p class="mt-3 text-sm leading-6 text-zinc-600">
      This account {roleText[auth.role] ?? "uses the API"} through the apps built on {branding.name}.
      The {branding.name} dashboard itself is for admins.
    </p>

    <form onsubmit={changePassword} class="mt-6 flex flex-col gap-2 border-t border-zinc-200 pt-5">
      <h2 class="mb-1 text-sm font-semibold text-zinc-900">Change password</h2>
      <input class={inputCls} type={reveal ? "text" : "password"} aria-label="Current password" placeholder="Current password" autocomplete="current-password" bind:value={currentPw} required />
      <input class={inputCls} type={reveal ? "text" : "password"} aria-label="New password" placeholder="New password (min 8 characters)" autocomplete="new-password" minlength="8" bind:value={newPw} required />
      <input class="{inputCls} {mismatch ? 'border-red-400' : ''}" type={reveal ? "text" : "password"} aria-label="Repeat new password" placeholder="Repeat new password" autocomplete="new-password" aria-invalid={mismatch ? "true" : undefined} bind:value={confirmPw} required />
      <label class="mt-1 flex items-center gap-2 text-xs text-zinc-600">
        <input type="checkbox" class="size-4 accent-zinc-900" bind:checked={reveal} /> Show passwords
      </label>
      {#if mismatch}
        <p class="text-sm text-red-600" role="alert">The new passwords don't match</p>
      {:else if error}
        <p class="text-sm text-red-600" role="alert">{error}</p>
      {:else if done}
        <p class="flex items-center gap-1.5 text-sm text-green-700" role="status">
          <CircleCheck class="size-4" /> Password changed. Your other sessions were signed out.
        </p>
      {/if}
      <div class="mt-2 flex items-center justify-between gap-2">
        <button type="button" class="{btnSecondary} flex items-center gap-1.5" onclick={onsignout}>
          <LogOut class="size-3.5" /> Sign out
        </button>
        <button class={btnPrimary} disabled={busy || mismatch}>{busy ? "Changing…" : "Change password"}</button>
      </div>
    </form>
  </div>
</div>

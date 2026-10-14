<script>
  import { api } from "./api.js";
  import { branding, loadBranding } from "./branding.svelte.js";
  import { showToast } from "./toast.svelte.js";
  import { askConfirm } from "./confirm.svelte.js";
  import KeyRound from "@lucide/svelte/icons/key-round";
  import CalendarDays from "@lucide/svelte/icons/calendar-days";
  import SlidersHorizontal from "@lucide/svelte/icons/sliders-horizontal";
  import ChevronDown from "@lucide/svelte/icons/chevron-down";
  import { Select } from "bits-ui";
  import { inputCls, selectTrigger, selectContent, selectItem } from "./cls.js";
  import GenArt from "./GenArt.svelte";

  let name = $state("");
  let tagline = $state("");
  let color = $state("");
  let logoUrl = $state("");
  let busy = $state(false);

  let dateOrder = $state("dmy");
  let beYear = $state(true);
  let busyDates = $state(false);

  $effect(() => {
    name = branding.name;
    tagline = branding.tagline;
    color = branding.color;
    logoUrl = branding.logoUrl ?? "";
  });

  let currentPw = $state("");
  let newPw = $state("");
  let busyPw = $state(false);

  async function changePassword(e) {
    e.preventDefault();
    busyPw = true;
    try {
      await api("/api/me/password", {
        method: "POST",
        body: JSON.stringify({ currentPassword: currentPw, password: newPw }),
      });
      currentPw = "";
      newPw = "";
      showToast("Password changed - other sessions were signed out");
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      busyPw = false;
    }
  }

  async function save(e) {
    e.preventDefault();
    busy = true;
    try {
      await api("/api/branding", {
        method: "PATCH",
        body: JSON.stringify({ name, tagline, color, logoUrl }),
      });
      await loadBranding();
      showToast("Branding updated");
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      busy = false;
    }
  }

  async function reset() {
    if (!(await askConfirm("Branding goes back to the -brand-* flags the server was started with.", "Reset branding?", "Reset"))) return;
    busy = true;
    try {
      await api("/api/branding", { method: "DELETE" });
      await loadBranding();
      name = branding.name;
      tagline = branding.tagline;
      color = branding.color;
      logoUrl = branding.logoUrl ?? "";
      showToast("Branding reset");
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      busy = false;
    }
  }

  $effect(() => {
    api("/api/settings/dates")
      .then((d) => {
        dateOrder = d.dateOrder;
        beYear = d.buddhistYears;
      })
      .catch((err) => showToast(err.message, "error"));
  });

  async function saveDates(e) {
    e.preventDefault();
    busyDates = true;
    try {
      const d = await api("/api/settings/dates", {
        method: "PUT",
        body: JSON.stringify({ order: dateOrder, buddhistYears: beYear }),
      });
      dateOrder = d.dateOrder;
      beYear = d.buddhistYears;
      showToast("Date settings saved");
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      busyDates = false;
    }
  }
</script>

<div class="mx-auto w-full max-w-4xl">
  <div class="mb-3">
    <h2 class="text-sm font-semibold">Settings</h2>
    <p class="text-xs text-zinc-500">Branding for this instance, and your account</p>
  </div>

  <div class="flex flex-col gap-4 lg:grid lg:grid-cols-2">
  <form onsubmit={save} class="flex flex-col gap-4 rounded-lg border border-zinc-200 bg-white p-4">
    <div class="mb-2 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm font-semibold">
      <SlidersHorizontal class="size-4" /> Branding
      <span class="basis-full text-xs font-normal text-zinc-400 sm:basis-auto">white-label the dashboard under your own brand</span>
    </div>

    <label class="flex flex-col gap-1">
      <span class="text-xs text-zinc-500">Name (title, header, login)</span>
      <input class={inputCls} type="text" bind:value={name} required />
    </label>

    <label class="flex flex-col gap-1">
      <span class="text-xs text-zinc-500">Tagline (login page)</span>
      <input class={inputCls} type="text" bind:value={tagline} />
    </label>

    <div class="flex flex-col gap-1">
      <span class="text-xs text-zinc-500">Accent color (primary buttons)</span>
      <div class="flex items-center gap-2">
        <input
          type="color"
          class="size-9 cursor-pointer rounded-lg border border-zinc-300 bg-white p-1"
          bind:value={color}
        />
        <input class="{inputCls} max-w-32 font-mono" type="text" bind:value={color} />
      </div>
    </div>

    <label class="flex flex-col gap-1">
      <span class="text-xs text-zinc-500">Logo URL (empty = generated art mark)</span>
      <input class={inputCls} type="url" placeholder="https://example.com/logo.png" bind:value={logoUrl} />
    </label>

    <div class="mt-1 flex items-center justify-between gap-3">
      <button
        type="button"
        class="rounded-lg px-2.5 py-1.5 text-sm text-zinc-500 hover:bg-zinc-100"
        onclick={reset}
      >Reset to server flags</button>
      <button
        class="bg-brand rounded-lg px-3 py-1.5 text-sm font-medium text-brand-on disabled:opacity-50"
        disabled={busy}
      >Save</button>
    </div>
    <p class="text-xs text-zinc-400">Stored overrides take precedence over the -brand-* startup flags and survive restarts.</p>
  </form>

  <!-- live preview of the login page, bound to the unsaved form state -->
  <div class="flex flex-col rounded-lg border border-zinc-200 bg-white p-4">
    <div class="mb-3 text-sm font-semibold">Login preview</div>
    <div class="flex flex-1 items-center justify-center rounded-xl border border-zinc-200 bg-zinc-50/60 p-6">
      <div class="flex flex-col items-center gap-2 text-center">
        {#if logoUrl}
          <img src={logoUrl} alt="logo" class="size-10 rounded-lg border border-zinc-200 object-cover" />
        {:else}
          <GenArt seed={name || "nori"} color={color} variant={3} w={64} h={64} class="size-10 rounded-lg border border-zinc-200" />
        {/if}
        <div class="text-lg font-bold text-zinc-900">{name || "your app"}</div>
        {#if tagline}
          <div class="text-xs text-zinc-500">{tagline}</div>
        {/if}
        <div class="mt-3 flex w-full max-w-60 flex-col gap-2">
          <div class="h-8 rounded-lg border border-zinc-300 bg-white"></div>
          <div class="h-8 rounded-lg border border-zinc-300 bg-white"></div>
          <div class="bg-brand flex h-8 items-center justify-center rounded-lg text-xs font-medium text-brand-on">Sign in</div>
        </div>
      </div>
    </div>
    <p class="mt-2 text-xs text-zinc-400">Updates as you type - Save applies it for everyone.</p>
  </div>
  </div>

  <div class="mt-4 flex flex-col gap-3 rounded-lg border border-zinc-200 bg-white p-4">
    <div class="flex items-center gap-2 text-sm font-semibold"><KeyRound class="size-4" /> Account</div>
    <form onsubmit={changePassword} class="flex flex-col gap-2">
      <div class="flex flex-wrap gap-2">
        <input class="{inputCls} min-w-52 flex-1" type="password" placeholder="Current password" bind:value={currentPw} autocomplete="current-password" required />
        <input class="{inputCls} min-w-52 flex-1" type="password" placeholder="New password (min 8 characters)" bind:value={newPw} autocomplete="new-password" minlength="8" required />
      </div>
      <div class="flex justify-end">
        <button
          class="bg-brand rounded-lg px-3 py-1.5 text-sm font-medium text-brand-on disabled:opacity-50"
          disabled={busyPw}
        >Change password</button>
      </div>
    </form>
    <p class="text-xs text-zinc-400">Other sessions of yours are signed out; this one stays signed in.</p>
  </div>

  <div class="mt-4 flex flex-col gap-3 rounded-lg border border-zinc-200 bg-white p-4">
    <div class="flex items-center gap-2 text-sm font-semibold"><CalendarDays class="size-4" /> Dates</div>
    <form onsubmit={saveDates} class="flex flex-wrap items-end gap-3">
      <div class="flex flex-col gap-1">
        <span class="text-xs text-zinc-500">Date order for ambiguous dates like 3/4/2026</span>
        <Select.Root type="single" bind:value={dateOrder}>
          <Select.Trigger class="{selectTrigger} w-64">
            <span class="truncate">{dateOrder === "mdy" ? "M/D/YYYY (3/4/2026 = Mar 4, 2026)" : "D/M/YYYY (3/4/2026 = 3 Apr 2026)"}</span>
            <ChevronDown class="size-3.5 shrink-0 text-zinc-400" />
          </Select.Trigger>
          <Select.Portal>
            <Select.Content class={selectContent}>
              <Select.Item value="dmy" class={selectItem}>D/M/YYYY (3/4/2026 = 3 Apr 2026)</Select.Item>
              <Select.Item value="mdy" class={selectItem}>M/D/YYYY (3/4/2026 = Mar 4, 2026)</Select.Item>
            </Select.Content>
          </Select.Portal>
        </Select.Root>
      </div>
      <button
        class="bg-brand rounded-lg px-3 py-1.5 text-sm font-medium text-brand-on disabled:opacity-50"
        disabled={busyDates}
      >Save</button>
    </form>
    <label class="flex items-center gap-2 text-sm">
      <input type="checkbox" class="size-4 accent-zinc-900" bind:checked={beYear} />
      <span>Buddhist Era years (<span class="font-mono text-xs">พ.ศ.</span>) - read years ≥ 2400 as พ.ศ. and convert to CE (2567 → 2024)</span>
    </label>
    <p class="text-xs text-zinc-400">Applies to imports and row writes. ISO dates (2026-04-03) are never affected. datetime values are stored as UTC and shown in your browser's timezone.</p>
  </div>
</div>

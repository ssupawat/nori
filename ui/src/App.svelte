<script>
  import { api } from "./lib/api.js";
  import { auth, clearSession } from "./lib/auth.svelte.js";
  import { tables, refreshTables, openTable } from "./lib/tables.svelte.js";
  import { showToast } from "./lib/toast.svelte.js";
  import LogOut from "@lucide/svelte/icons/log-out";
  import LayoutDashboard from "@lucide/svelte/icons/layout-dashboard";
  import KeyRound from "@lucide/svelte/icons/key-round";
  import Braces from "@lucide/svelte/icons/braces";
  import History from "@lucide/svelte/icons/history";
  import SlidersHorizontal from "@lucide/svelte/icons/sliders-horizontal";
  import Menu from "@lucide/svelte/icons/menu";
  import X from "@lucide/svelte/icons/x";
  import GenArt from "./lib/GenArt.svelte";
  import ArtBackground from "./lib/ArtBackground.svelte";
  import NewTableDialog from "./lib/NewTableDialog.svelte";
  import ImportXlsx from "./lib/ImportXlsx.svelte";
  import Plus from "@lucide/svelte/icons/plus";
  import Upload from "@lucide/svelte/icons/upload";
  import { branding } from "./lib/branding.svelte.js";
  import { Dialog } from "bits-ui";
  import Toast from "./lib/Toast.svelte";
  import ConfirmDialog from "./lib/ConfirmDialog.svelte";
  import Login from "./lib/Login.svelte";
  import TablesView from "./lib/TablesView.svelte";
  import DashboardView from "./lib/DashboardView.svelte";
  import AccessView from "./lib/AccessView.svelte";
  import ApiView from "./lib/ApiView.svelte";
  import AuditView from "./lib/AuditView.svelte";
  import SettingsView from "./lib/SettingsView.svelte";
  import { applyFavicon } from "./lib/favicon.js";

  // last active view survives a refresh (current table already persists
  // separately in tables.svelte.js)
  const VIEWS = ["dashboard", "tables", "api", "access", "audit", "settings"];
  let view = $state(VIEWS.includes(localStorage.getItem("nori-view")) ? localStorage.getItem("nori-view") : "dashboard");
  let drawerOpen = $state(false);

  $effect(() => {
    localStorage.setItem("nori-view", view);
  });
  // the dashboard is admin-only; verify the stored session server-side on
  // boot so a spoofed localStorage role cannot render the admin shell
  let verified = $state(false);

  // tab icon follows the brand (art mark or custom logo)
  $effect(() => {
    applyFavicon(branding);
  });

  // Reset the active view only when the session actually changes
  // (login/logout), so a restricted account never lands on the previous
  // admin's view - and a plain refresh keeps the view it was on.
  let prevToken = auth.token;
  $effect(() => {
    if (auth.token === prevToken) return;
    prevToken = auth.token;
    view = "dashboard";
    drawerOpen = false;
  });

  $effect(() => {
    if (!auth.token) return;
    refreshTables().catch((err) => showToast(err.message, "error"));
  });

  // verify the claimed role against the server before rendering anything
  $effect(() => {
    if (!auth.token) return;
    api("/api/users")
      .then(() => (verified = true))
      .catch((err) => {
        if (err.status === 403) clearSession();
        verified = true;
      });
  });

  let newTableOpen = $state(false);
  let importRef;
  let tablesViewRef;

  async function onTableCreated(name) {
    await refreshTables();
    openTable(name);
    view = "tables";
    showToast(`Created ${name}`);
  }

  function onImported() {
    // same view: rows must be reloaded by hand (nothing it watches changed);
    // otherwise land on the imported table
    if (view === "tables") tablesViewRef?.reload();
    else goto("tables");
  }

  async function loadSample() {
    try {
      const res = await fetch("/api/sample.xlsx", {
        headers: { Authorization: "Bearer " + auth.token },
      });
      if (!res.ok) throw new Error(`sample download failed (${res.status})`);
      const blob = await res.blob();
      importRef.loadFile(
        new File([blob], "sample.xlsx", {
          type: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
        })
      );
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  async function signOut() {
    try {
      await api("/api/logout", { method: "POST" });
    } catch {}
    clearSession();
  }

  function goto(v) {
    view = v;
    drawerOpen = false;
  }

  function pickTable(name) {
    openTable(name);
    view = "tables";
    drawerOpen = false;
  }

  const systemItems = [
    { view: "api", label: "API", icon: Braces },
    { view: "access", label: "Access", icon: KeyRound },
    { view: "audit", label: "Audit", icon: History },
    { view: "settings", label: "Settings", icon: SlidersHorizontal },
  ];

  const sideItemCls = (active) =>
    "flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm transition-colors " +
    (active ? "bg-brand text-brand-on" : "text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900");
  const sideLabelCls = "px-3 pb-1 pt-4 text-[11px] font-semibold tracking-wider text-zinc-400 uppercase";
  const mobileTabCls = (active) =>
    "flex shrink-0 items-center gap-2 rounded-lg px-3 py-2 text-sm whitespace-nowrap " +
    (active ? "bg-zinc-100 font-medium text-zinc-900" : "text-zinc-500");
</script>

{#snippet sideNav()}
  <!-- single scroll region: groups flow top-down so the empty space lives at
       the bottom (above the footer) instead of a hole in the middle -->
  <nav class="flex min-h-0 flex-1 flex-col overflow-y-auto px-3 pb-3">
    <div class="flex flex-col gap-0.5">
      <button class={sideItemCls(view === "dashboard")} onclick={() => goto("dashboard")}>
        <LayoutDashboard class="size-4 shrink-0" /> Dashboard
      </button>
    </div>
    <div class={sideLabelCls}>Tables</div>
    <div class="flex flex-col gap-0.5">
      {#each tables.list as t (t.name)}
        {@const active = view === "tables" && tables.current === t.name}
        <a
          href="#"
          class={sideItemCls(active)}
          onclick={(e) => {
            e.preventDefault();
            pickTable(t.name);
          }}
        >
          <GenArt seed={t.name} variant={3} w={64} h={64} class="size-5 shrink-0 rounded-md border border-zinc-200" />
          <span class="truncate">{t.name}</span>
        </a>
      {/each}
      <button
        class="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900"
        onclick={() => {
          drawerOpen = false;
          newTableOpen = true;
        }}
      ><Plus class="size-4 shrink-0" /> New table</button>
      <button
        class="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900"
        onclick={() => {
          drawerOpen = false;
          importRef.pick();
        }}
      ><Upload class="size-4 shrink-0" /> Import xlsx</button>
    </div>
    <!-- mt-auto pins System to the sidebar's bottom when content is short;
         with many tables it just flows after the list -->
    <div class="{sideLabelCls} mt-auto">System</div>
    <div class="flex flex-col gap-0.5">
      {#each systemItems as m (m.view)}
        <button class={sideItemCls(view === m.view)} onclick={() => goto(m.view)}>
          <m.icon class="size-4 shrink-0" /> {m.label}
        </button>
      {/each}
    </div>
  </nav>
{/snippet}

{#if !auth.token}
  <Login />
{:else if !verified}
  <div class="grid min-h-screen place-items-center bg-zinc-50">
    <p class="text-sm text-zinc-400">Loading…</p>
  </div>
{:else if auth.role !== "admin"}
  <!-- dashboard is admin-only; editor/viewer accounts are for API use -->
  <div class="flex min-h-screen flex-col items-center justify-center gap-3 bg-zinc-50 p-4 text-center">
    <h1 class="text-lg font-bold text-zinc-900">{branding.name}</h1>
    <p class="max-w-sm text-sm text-zinc-500">
      This account is for API use only. It has no access to this dashboard.
      Frontends built on the {branding.name} API keep working as usual.
    </p>
    <button
      class="mt-2 rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm text-zinc-700 hover:bg-zinc-50"
      onclick={signOut}
    >Sign out</button>
  </div>
{:else}
  <div class="flex h-screen overflow-hidden">

    <!-- Desktop sidebar -->
    <aside class="hidden w-56 shrink-0 flex-col border-r border-zinc-200 bg-white/70 backdrop-blur-sm md:flex">
      <div class="flex items-center gap-2 px-4 py-3.5">
        <GenArt seed={branding.name} variant={3} w={64} h={64} class="size-6 rounded-md border border-zinc-200" />
        <span class="truncate font-bold text-zinc-900">{branding.name}</span>
      </div>
      {@render sideNav()}
      <div class="border-t border-zinc-200 px-3 py-2.5">
        <!-- every viewer of this dashboard is an admin, so no role badge here -->
        <div class="flex min-w-0 items-center justify-between gap-2">
          <span class="min-w-0 truncate text-xs font-medium text-zinc-600">{auth.username}</span>
          <button
            class="flex shrink-0 items-center gap-1.5 rounded-lg px-1 py-0.5 text-xs text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900"
            title="Sign out"
            onclick={signOut}
          ><LogOut class="size-3.5" /> Sign out</button>
        </div>
      </div>
    </aside>

    <!-- Main column -->
    <div class="flex min-w-0 flex-1 flex-col">
      <!-- Mobile header -->
      <header class="flex items-center justify-between gap-2 border-b border-zinc-200 bg-white/70 px-4 py-2 backdrop-blur-sm md:hidden">
        <div class="flex min-w-0 items-center gap-2">
          <button
            class="rounded-lg p-1.5 text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900"
            title="Menu"
            onclick={() => (drawerOpen = true)}
          ><Menu class="size-5" /></button>
          <span class="flex min-w-0 items-center gap-2 font-bold text-zinc-900">
            <GenArt seed={branding.name} variant={3} w={64} h={64} class="size-6 shrink-0 rounded-md border border-zinc-200" />
            <span class="truncate">{branding.name}</span>
          </span>
        </div>
        <div class="flex items-center gap-2">
          <span class="max-w-24 truncate text-xs font-medium text-zinc-600">{auth.username}</span>
          <button
            class="flex shrink-0 items-center gap-1 text-sm text-zinc-500 hover:text-zinc-900"
            onclick={signOut}
          ><LogOut class="size-3.5" /> Sign out</button>
        </div>
      </header>

      <!-- static frame + inner scroll window: cards sliced while scrolling
           stay off the screen edges -->
      <main class="flex min-h-0 flex-1 flex-col overflow-hidden p-4 pb-3">
        <div class="flex min-h-0 flex-1 flex-col overflow-y-auto">
        {#key view}
          <!-- fills the viewport on desktop (grid uses flex-1); sizes to content
               on mobile so pb-24 keeps the last card clear of the tab bar -->
          <div class="anim-fade flex flex-col pb-24 md:min-h-0 md:flex-1 md:pb-2">
            {#if view === "dashboard"}
              <DashboardView ongoto={goto} onimport={() => importRef.pick()} onnewtable={() => (newTableOpen = true)} onsample={loadSample} />
            {:else if view === "tables"}
              <TablesView bind:this={tablesViewRef} onimport={() => importRef.pick()} ongoto={goto} />
            {:else if view === "api"}
              <ApiView />
            {:else if view === "audit"}
              <AuditView />
            {:else if view === "settings"}
              <SettingsView />
            {:else}
              <AccessView />
            {/if}
          </div>
        {/key}
        </div>
      </main>

      <!-- Mobile bottom nav: dashboard + tables -->
      <nav class="fixed inset-x-0 bottom-0 z-30 flex items-center gap-1 overflow-x-auto border-t border-zinc-200 bg-white/90 px-2 py-1.5 backdrop-blur-sm md:hidden">
        <a
          href="#"
          class={mobileTabCls(view === "dashboard")}
          onclick={(e) => {
            e.preventDefault();
            goto("dashboard");
          }}
        >
          <LayoutDashboard class="size-4" /> Dashboard
        </a>
        {#each tables.list as t (t.name)}
          {@const active = view === "tables" && tables.current === t.name}
          <a
            href="#"
            class={mobileTabCls(active)}
            onclick={(e) => {
              e.preventDefault();
              pickTable(t.name);
            }}
          >
            <GenArt seed={t.name} variant={3} w={64} h={64} class="size-5 shrink-0 rounded-md border border-zinc-200" />
            {t.name}
          </a>
        {/each}
        <button
          class="ml-auto mr-1.5 flex shrink-0 items-center gap-1 rounded-lg px-2.5 py-2 text-sm text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900"
          title="New table"
          onclick={() => (newTableOpen = true)}
        ><Plus class="size-4" /></button>
      </nav>
    </div>
  </div>

  <Dialog.Root bind:open={drawerOpen}>
    <Dialog.Portal>
      <Dialog.Overlay class="anim-fade fixed inset-0 z-40 bg-black/40 md:hidden" />
      <Dialog.Content
        class="anim-slide-left fixed inset-y-0 left-0 z-50 flex w-64 flex-col bg-white shadow-xl outline-none md:hidden"
      >
        <div class="flex items-center justify-between px-4 py-3">
          <span class="flex items-center gap-2 font-bold text-zinc-900">
            <GenArt seed={branding.name} variant={3} w={64} h={64} class="size-6 rounded-md border border-zinc-200" />
            {branding.name}
          </span>
          <Dialog.Close
            class="rounded-lg p-1.5 text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900"
            title="Close"
          ><X class="size-5" /></Dialog.Close>
        </div>
        {@render sideNav()}
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>

  <ArtBackground />
{/if}

  <ConfirmDialog />
  <NewTableDialog bind:open={newTableOpen} oncreated={onTableCreated} />
  <ImportXlsx bind:this={importRef} onimported={onImported} />
  <Toast />

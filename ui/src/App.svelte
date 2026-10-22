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
  import Table2 from "@lucide/svelte/icons/table-2";
  import Ellipsis from "@lucide/svelte/icons/ellipsis";
  import X from "@lucide/svelte/icons/x";
  import GenArt from "./lib/GenArt.svelte";
  import TableIcon from "./lib/TableIcon.svelte";
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

  // The view and table are kept in the URL hash (#/tables/orders, #/audit);
  // localStorage restores the last view when the hash is empty.
  const VIEWS = ["dashboard", "tables", "api", "access", "audit", "settings"];

  function parseHash() {
    const [, v, ...rest] = location.hash.replace(/^#/, "").split("/");
    if (!VIEWS.includes(v)) return null;
    return { view: v, table: v === "tables" && rest.length ? decodeURIComponent(rest.join("/")) : null };
  }

  const initial = parseHash();
  if (initial?.table) openTable(initial.table);
  let view = $state(initial?.view ?? (VIEWS.includes(localStorage.getItem("nori-view")) ? localStorage.getItem("nori-view") : "dashboard"));
  let drawerOpen = $state(false);

  function hashFor(v, table) {
    return v === "tables" && table ? `#/tables/${encodeURIComponent(table)}` : `#/${v}`;
  }

  // state -> URL: a new history entry per navigation (first one replaces)
  let firstSync = true;
  $effect(() => {
    localStorage.setItem("nori-view", view);
    const want = hashFor(view, tables.current);
    if (location.hash === want) return;
    if (firstSync) history.replaceState(null, "", want);
    else history.pushState(null, "", want);
    firstSync = false;
  });

  // URL -> state: Back/Forward and edited links
  $effect(() => {
    const onHash = () => {
      const h = parseHash();
      if (!h) return;
      if (h.table && h.table !== tables.current) openTable(h.table);
      view = h.view;
      drawerOpen = false;
    };
    window.addEventListener("popstate", onHash);
    window.addEventListener("hashchange", onHash);
    return () => {
      window.removeEventListener("popstate", onHash);
      window.removeEventListener("hashchange", onHash);
    };
  });
  // the dashboard is admin-only; verify the stored session server-side on
  // boot so a spoofed localStorage role cannot render the admin shell
  let verified = $state(false);

  // tab icon follows the brand (art mark or custom logo)
  $effect(() => {
    applyFavicon(branding);
  });

  // Reset the view to the dashboard when a different user signs in.
  let lastUser = localStorage.getItem("nori-last-user") ?? "";
  let prevToken = auth.token;
  $effect(() => {
    if (auth.token === prevToken) return;
    prevToken = auth.token;
    drawerOpen = false;
    if (!auth.token) return;
    // a first sign-in on this browser keeps the current view
    if (lastUser && auth.username !== lastUser) view = "dashboard";
    lastUser = auth.username;
    localStorage.setItem("nori-last-user", lastUser);
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

  let sampleBusy = $state(false);

  async function loadSample() {
    sampleBusy = true;
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
    } finally {
      sampleBusy = false;
    }
  }

  // drag an .xlsx anywhere onto the dashboard to import it
  let dragging = $state(false);
  let dragDepth = 0;
  const hasFiles = (e) => [...(e.dataTransfer?.types ?? [])].includes("Files");

  function onDragEnter(e) {
    if (!hasFiles(e)) return;
    e.preventDefault();
    dragDepth++;
    dragging = true;
  }
  function onDragLeave(e) {
    if (!hasFiles(e)) return;
    dragDepth = Math.max(0, dragDepth - 1);
    if (dragDepth === 0) dragging = false;
  }
  function onDrop(e) {
    if (!hasFiles(e)) return;
    e.preventDefault();
    dragDepth = 0;
    dragging = false;
    const f = e.dataTransfer.files[0];
    if (f) importRef.choose(f);
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
  const sideLabelCls = "px-3 pb-1 pt-4 text-[11px] font-semibold tracking-wider text-zinc-500 uppercase";
  const mobileTabCls = (active) =>
    "flex flex-col items-center gap-0.5 rounded-lg px-1 py-1.5 text-[11px] " +
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
          <TableIcon name={t.name} />
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
    <p class="text-sm text-zinc-500">Loading…</p>
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
  <div
    class="flex h-screen overflow-hidden"
    role="presentation"
    ondragenter={onDragEnter}
    ondragover={(e) => hasFiles(e) && e.preventDefault()}
    ondragleave={onDragLeave}
    ondrop={onDrop}
  >
    {#if dragging}
      <div class="pointer-events-none fixed inset-0 z-[70] grid place-items-center bg-white/80 p-6 backdrop-blur-sm">
        <div class="flex flex-col items-center gap-2 rounded-2xl border-2 border-dashed border-zinc-400 px-10 py-12 text-center">
          <Upload class="size-8 text-zinc-500" />
          <p class="font-semibold text-zinc-900">Drop an .xlsx file to import it</p>
          <p class="text-sm text-zinc-500">Every sheet becomes a table</p>
        </div>
      </div>
    {/if}

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
            aria-label="Open menu"
            onclick={() => (drawerOpen = true)}
          ><Menu class="size-5" /></button>
          <span class="flex min-w-0 items-center gap-2 font-bold text-zinc-900">
            <GenArt seed={branding.name} variant={3} w={64} h={64} class="size-6 shrink-0 rounded-md border border-zinc-200" />
            <span class="truncate">{branding.name}</span>
          </span>
        </div>
        <span class="max-w-28 truncate text-xs font-medium text-zinc-600">{auth.username}</span>
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
              <DashboardView ongoto={goto} onimport={() => importRef.pick()} onnewtable={() => (newTableOpen = true)} onsample={loadSample} {sampleBusy} />
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

      <!-- Mobile bottom nav: Dashboard, Tables, API, More -->
      <nav class="fixed inset-x-0 bottom-0 z-30 grid grid-cols-4 border-t border-zinc-200 bg-white/90 px-1 pt-1 pb-[max(0.25rem,env(safe-area-inset-bottom))] backdrop-blur-sm md:hidden" aria-label="Main">
        {#each [{ v: "dashboard", label: "Dashboard", icon: LayoutDashboard }, { v: "tables", label: "Tables", icon: Table2 }, { v: "api", label: "API", icon: Braces }] as t (t.v)}
          <button class={mobileTabCls(view === t.v)} aria-current={view === t.v ? "page" : undefined} onclick={() => goto(t.v)}>
            <t.icon class="size-5" />
            <span>{t.label}</span>
          </button>
        {/each}
        <button class={mobileTabCls(["access", "audit", "settings"].includes(view))} onclick={() => (drawerOpen = true)}>
          <Ellipsis class="size-5" />
          <span>More</span>
        </button>
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
        <div class="flex items-center justify-between gap-2 border-t border-zinc-200 px-4 py-3">
          <span class="min-w-0 truncate text-sm font-medium text-zinc-700">{auth.username}</span>
          <button
            class="flex shrink-0 items-center gap-1.5 rounded-lg border border-zinc-300 px-2.5 py-1.5 text-sm text-zinc-700 hover:bg-zinc-50"
            onclick={signOut}
          ><LogOut class="size-3.5" /> Sign out</button>
        </div>
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>

  <ArtBackground />
{/if}

  <ConfirmDialog />
  <NewTableDialog bind:open={newTableOpen} oncreated={onTableCreated} />
  <ImportXlsx bind:this={importRef} onimported={onImported} />
  <Toast />

<script>
  import { Dialog, Select, DropdownMenu, Popover } from "bits-ui";
  import Trash2 from "@lucide/svelte/icons/trash-2";
  import UserRoundPlus from "@lucide/svelte/icons/user-round-plus";
  import KeyRound from "@lucide/svelte/icons/key-round";
  import Plus from "@lucide/svelte/icons/plus";
  import Ellipsis from "@lucide/svelte/icons/ellipsis";
  import Download from "@lucide/svelte/icons/download";
  import UserCog from "@lucide/svelte/icons/user-cog";
  import LockKeyhole from "@lucide/svelte/icons/lock-keyhole";
  import History from "@lucide/svelte/icons/history";
  import Search from "@lucide/svelte/icons/search";
  import CircleHelp from "@lucide/svelte/icons/circle-help";
  import TriangleAlert from "@lucide/svelte/icons/triangle-alert";
  import ChevronDown from "@lucide/svelte/icons/chevron-down";
  import CopyButton from "./CopyButton.svelte";
  import RoleBadge from "./RoleBadge.svelte";
  import TableIcon from "./TableIcon.svelte";
  import PromptDialog from "./PromptDialog.svelte";
  import { api } from "./api.js";
  import { inputCls, dialogOverlay, dialogContent, btnPrimary, btnSecondary, selectTrigger, selectContent, selectItem } from "./cls.js";
  import { auth } from "./auth.svelte.js";
  import { branding } from "./branding.svelte.js";
  import { showToast } from "./toast.svelte.js";
  import { askConfirm } from "./confirm.svelte.js";
  import { ago } from "./fmt.js";

  // sub: "" = People tab, "keys" = API keys tab
  let { sub = "", onsub } = $props();
  const tab = $derived(sub === "keys" ? "keys" : "people");

  const ROLES = ["admin", "editor", "viewer", "builder"];
  const roleDesc = {
    admin: "Full access: tables, columns, rows, import/export, users, API keys, branding. The only role that can use this dashboard.",
    editor: "Create, edit and delete rows in every table.",
    viewer: "Read-only: browse tables and rows, export.",
    builder: "Builds against the API without seeing data: table names, columns and docs only, never rows.",
  };
  const roleShort = {
    admin: "full access, including this dashboard",
    editor: "can edit data",
    viewer: "read only",
    builder: "schema and docs only, never rows",
  };

  let users = $state([]);
  let keys = $state([]);
  let usersLoaded = $state(false);
  let keysLoaded = $state(false);

  async function loadUsers() {
    try {
      users = await api("/api/users/");
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      usersLoaded = true;
    }
  }

  async function loadKeys() {
    try {
      keys = await api("/api/keys/");
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      keysLoaded = true;
    }
  }

  $effect(() => {
    loadUsers();
    loadKeys();
  });

  // ---- search + role filter, per tab ----
  let query = $state("");
  let roleFilter = $state("");
  $effect(() => {
    void tab;
    query = "";
    roleFilter = "";
  });

  const items = $derived(tab === "keys" ? keys : users);
  const nameOf = (it) => it.username ?? it.name;
  const shown = $derived(
    items.filter(
      (it) =>
        (!roleFilter || it.role === roleFilter) &&
        (!query || nameOf(it).toLowerCase().includes(query.trim().toLowerCase()))
    )
  );
  const counts = $derived(Object.fromEntries(ROLES.map((r) => [r, items.filter((it) => it.role === r).length])));

  const DAY = 86400000;
  // no sign-in / use for 30 days (counted from creation when never used)
  function stale(lastAt, createdAt) {
    const ref = lastAt || createdAt;
    return !!ref && Date.now() - Date.parse(ref) > 30 * DAY;
  }

  function viewActivity(actor) {
    location.hash = `#/audit/user/${encodeURIComponent(actor)}`;
  }

  // ---- one-time secrets (new password / new key) ----
  // closing before a copy or download asks once
  let secretSaved = $state(false);
  let secretWarn = $state(false);

  function closeSecret(close) {
    if (!secretSaved && !secretWarn) {
      secretWarn = true;
      return;
    }
    close();
  }

  // ---- people ----
  let addOpen = $state(false);
  let username = $state("");
  let role = $state("viewer");
  let busy = $state(false);
  let creds = $state(null);
  let credsOpen = $state(false);

  async function addUser(e) {
    e.preventDefault();
    busy = true;
    try {
      creds = await api("/api/users/", {
        method: "POST",
        body: JSON.stringify({ username, role }),
      });
      username = "";
      role = "viewer";
      addOpen = false;
      secretSaved = false;
      secretWarn = false;
      credsOpen = true;
      await loadUsers();
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      busy = false;
    }
  }

  function downloadCreds() {
    const text = [
      `${branding.name} - your login`,
      "",
      `Username: ${creds.username}`,
      `Password: ${creds.password}`,
      "",
      "Use these to sign in to the app you were given.",
      `To change the password, sign in at ${location.origin}`,
    ].join("\n");
    const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `${creds.username}-login.txt`;
    a.click();
    URL.revokeObjectURL(url);
    secretSaved = true;
    showToast(`Saved ${creds.username}-login.txt`);
  }

  let passOpen = $state(false);
  let passTarget = $state("");
  let passValue = $state("");

  function askSetPassword(name) {
    passTarget = name;
    passValue = "";
    passOpen = true;
  }

  // throws on failure
  async function setPassword(v) {
    await api(`/api/users/${encodeURIComponent(passTarget)}`, {
      method: "PATCH",
      body: JSON.stringify({ password: v }),
    });
    showToast(`Password changed for ${passTarget} - their sessions were signed out`);
  }

  let roleOpen = $state(false);
  let roleTarget = $state(null);
  let roleValue = $state("viewer");
  let roleBusy = $state(false);

  function askRole(u) {
    roleTarget = u;
    roleValue = u.role;
    roleOpen = true;
  }

  async function saveRole(e) {
    e.preventDefault();
    if (roleValue === roleTarget.role) {
      roleOpen = false;
      return;
    }
    roleBusy = true;
    try {
      await api(`/api/users/${encodeURIComponent(roleTarget.username)}`, {
        method: "PATCH",
        body: JSON.stringify({ role: roleValue }),
      });
      showToast(`${roleTarget.username} is now ${roleValue}`);
      roleOpen = false;
      await loadUsers();
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      roleBusy = false;
    }
  }

  async function deleteUser(name) {
    if (!(await askConfirm(`User "${name}" will be removed and signed out everywhere.`, "Delete user?"))) return;
    try {
      await api(`/api/users/${encodeURIComponent(name)}`, { method: "DELETE" });
      showToast(`Deleted user ${name}`);
      await loadUsers();
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  // ---- API keys ----
  let keyOpen = $state(false);
  let keyName = $state("");
  let keyRole = $state("builder");
  let keysBusy = $state(false);
  let newKey = $state(null);
  let newKeyOpen = $state(false);

  async function createKey(e) {
    e.preventDefault();
    if (!keyName.trim()) return;
    keysBusy = true;
    try {
      newKey = await api("/api/keys/", {
        method: "POST",
        body: JSON.stringify({ name: keyName.trim(), role: keyRole }),
      });
      keyName = "";
      keyRole = "builder";
      keyOpen = false;
      secretSaved = false;
      secretWarn = false;
      newKeyOpen = true;
      await loadKeys();
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      keysBusy = false;
    }
  }

  async function revokeKey(k) {
    if (!(await askConfirm(`"${k.name}" stops working immediately.`, "Revoke API key?", "Revoke"))) return;
    try {
      await api(`/api/keys/${encodeURIComponent(k.id)}`, { method: "DELETE" });
      showToast(`Revoked ${k.name}`);
      await loadKeys();
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  const menuItem = "flex cursor-pointer items-center gap-2 px-3 py-2 text-sm data-highlighted:bg-zinc-100";
  const chipCls = (on) =>
    "rounded-full px-2.5 py-1 text-xs tabular-nums " +
    (on ? "bg-brand text-brand-on" : "bg-zinc-100 text-zinc-600 hover:bg-zinc-200");
  const tabCls = (on) =>
    "flex items-center gap-2 border-b-2 px-1 pb-2 text-sm font-medium " +
    (on ? "border-brand text-zinc-900" : "border-transparent text-zinc-500 hover:text-zinc-900");
  // name | role | last activity | created | menu; on phones: name+role, then activity+menu
  const rowGrid =
    "grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 md:grid-cols-[minmax(0,1fr)_7rem_12rem_7rem_2rem]";
</script>

<div class="w-full">
  <div class="mb-4 flex items-start gap-3">
    <div class="min-w-0 flex-1">
      <h2 class="text-xl font-semibold tracking-tight text-zinc-900">Access</h2>
      <p class="mt-0.5 text-sm text-zinc-500">People who sign in to apps built on {branding.name}, and keys for scripts and agents</p>
    </div>
    {#if tab === "people"}
      <button class="{btnSecondary} flex shrink-0 items-center gap-1.5" onclick={() => (addOpen = true)}>
        <UserRoundPlus class="size-3.5" /> Add person
      </button>
    {:else}
      <button class="{btnSecondary} flex shrink-0 items-center gap-1.5" onclick={() => (keyOpen = true)}>
        <Plus class="size-3.5" /> Create key
      </button>
    {/if}
  </div>

  <div class="mb-3 flex gap-5 border-b border-zinc-200" role="tablist">
    <button role="tab" aria-selected={tab === "people"} class={tabCls(tab === "people")} onclick={() => onsub?.("")}>
      People <span class="rounded-full bg-zinc-100 px-1.5 text-xs text-zinc-600 tabular-nums">{users.length}</span>
    </button>
    <button role="tab" aria-selected={tab === "keys"} class={tabCls(tab === "keys")} onclick={() => onsub?.("keys")}>
      API keys <span class="rounded-full bg-zinc-100 px-1.5 text-xs text-zinc-600 tabular-nums">{keys.length}</span>
    </button>
  </div>

  <div class="mb-3 flex flex-wrap items-center gap-2">
    <label class="relative block w-full sm:w-64">
      <span class="sr-only">Search {tab === "keys" ? "keys" : "people"}</span>
      <Search class="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-zinc-500" />
      <input class="{inputCls} py-1.5 pl-8" type="search" placeholder={tab === "keys" ? "Search keys…" : "Search people…"} bind:value={query} />
    </label>
    <div class="flex flex-wrap items-center gap-1.5">
      <button class={chipCls(!roleFilter)} onclick={() => (roleFilter = "")}>All {items.length}</button>
      {#each ROLES as r (r)}
        {#if counts[r] > 0}
          <button class={chipCls(roleFilter === r)} aria-pressed={roleFilter === r} onclick={() => (roleFilter = roleFilter === r ? "" : r)}>{r} {counts[r]}</button>
        {/if}
      {/each}
    </div>
    <Popover.Root>
      <Popover.Trigger class="ml-auto flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900">
        <CircleHelp class="size-3.5" /> Roles
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content class="z-50 w-80 max-w-[calc(100vw-2rem)] rounded-lg border border-zinc-200 bg-white p-3 shadow-lg" align="end" sideOffset={6}>
          <ul class="flex flex-col gap-2.5">
            {#each ROLES as r (r)}
              <li>
                <RoleBadge role={r} />
                <p class="mt-1 text-xs leading-5 text-zinc-600">{roleDesc[r]}</p>
              </li>
            {/each}
          </ul>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  </div>

  <div class="overflow-hidden rounded-lg border border-zinc-200 bg-white">
    <div class="{rowGrid} hidden border-b border-zinc-200 bg-zinc-50 px-4 py-2 text-xs font-medium text-zinc-500 md:grid">
      <span>Name</span>
      <span>Role</span>
      <span>{tab === "keys" ? "Last used" : "Last sign-in"}</span>
      <span>Created</span>
      <span></span>
    </div>
    <ul class="divide-y divide-zinc-200">
      {#if tab === "people"}
        {#each shown as u (u.username)}
          {@const self = u.username === auth.username}
          <li class="{rowGrid} px-4 py-2.5 hover:bg-zinc-50">
            <div class="flex min-w-0 items-center gap-3">
              <TableIcon name={u.username} class="size-8 rounded-full text-xs" />
              <div class="min-w-0">
                <div class="truncate text-sm font-medium text-zinc-900">
                  {u.username}{#if self}<span class="ml-1 text-xs font-normal text-zinc-500">(you)</span>{/if}
                </div>
                <div class="truncate text-xs text-zinc-500">{roleShort[u.role] ?? ""}</div>
              </div>
            </div>
            <div class="justify-self-end md:justify-self-start">
              {#if self}
                <RoleBadge role={u.role} />
              {:else}
                <button class="flex items-center gap-0.5 rounded-full hover:opacity-80" title="Change role" onclick={() => askRole(u)}>
                  <RoleBadge role={u.role} /><ChevronDown class="size-3 text-zinc-500" />
                </button>
              {/if}
            </div>
            <div class="min-w-0 text-xs">
              <span class={stale(u.lastLoginAt, u.createdAt) ? "text-amber-600" : "text-zinc-600"}>
                <span class="md:hidden">Last sign-in&nbsp;</span>{u.lastLoginAt ? ago(u.lastLoginAt) : "never"}{stale(u.lastLoginAt, u.createdAt) ? " · inactive 30d+" : ""}
              </span>
              {#if u.failedLogins > 0}
                <span class="flex items-center gap-1 text-red-600">
                  <TriangleAlert class="size-3" /> {u.failedLogins} failed sign-in{u.failedLogins === 1 ? "" : "s"} · {ago(u.lastFailedAt)}
                </span>
              {/if}
            </div>
            <div class="hidden text-xs text-zinc-500 md:block">{u.createdAt ? ago(u.createdAt) : "—"}</div>
            <div class="justify-self-end">
              <DropdownMenu.Root>
                <DropdownMenu.Trigger class="rounded p-1.5 text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900" aria-label="Actions for {u.username}">
                  <Ellipsis class="size-4" />
                </DropdownMenu.Trigger>
                <DropdownMenu.Portal>
                  <DropdownMenu.Content class="z-50 min-w-44 rounded-md border border-zinc-200 bg-white py-1 shadow-md" align="end">
                    <DropdownMenu.Item class={menuItem} onSelect={() => viewActivity(u.username)}>
                      <History class="size-3.5" /> View activity
                    </DropdownMenu.Item>
                    {#if !self}
                      <DropdownMenu.Item class={menuItem} onSelect={() => askRole(u)}>
                        <UserCog class="size-3.5" /> Change role
                      </DropdownMenu.Item>
                      <DropdownMenu.Item class={menuItem} onSelect={() => askSetPassword(u.username)}>
                        <LockKeyhole class="size-3.5" /> Set password
                      </DropdownMenu.Item>
                      <DropdownMenu.Item class={menuItem + " text-red-600"} onSelect={() => deleteUser(u.username)}>
                        <Trash2 class="size-3.5" /> Delete user
                      </DropdownMenu.Item>
                    {/if}
                  </DropdownMenu.Content>
                </DropdownMenu.Portal>
              </DropdownMenu.Root>
            </div>
          </li>
        {:else}
          <li class="px-4 py-8 text-center text-sm text-zinc-500">
            {!usersLoaded ? "Loading people…" : query || roleFilter ? "No one matches." : "No people yet."}
          </li>
        {/each}
      {:else}
        {#each shown as k (k.id)}
          <li class="{rowGrid} px-4 py-2.5 hover:bg-zinc-50">
            <div class="flex min-w-0 items-center gap-3">
              <span class="flex size-8 shrink-0 items-center justify-center rounded-full bg-zinc-100 text-zinc-600"><KeyRound class="size-4" /></span>
              <div class="min-w-0">
                <div class="truncate text-sm font-medium text-zinc-900">{k.name}</div>
                <div class="truncate font-mono text-xs text-zinc-500">{k.prefix}…</div>
              </div>
            </div>
            <div class="justify-self-end md:justify-self-start"><RoleBadge role={k.role} /></div>
            <div class="text-xs {stale(k.lastUsedAt, k.createdAt) ? 'text-amber-600' : 'text-zinc-600'}">
              <span class="md:hidden">Last used&nbsp;</span>{k.lastUsedAt ? ago(k.lastUsedAt) : "never"}{stale(k.lastUsedAt, k.createdAt) ? " · unused 30d+" : ""}
            </div>
            <div class="hidden text-xs text-zinc-500 md:block">{ago(k.createdAt)}</div>
            <div class="justify-self-end">
              <DropdownMenu.Root>
                <DropdownMenu.Trigger class="rounded p-1.5 text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900" aria-label="Actions for key {k.name}">
                  <Ellipsis class="size-4" />
                </DropdownMenu.Trigger>
                <DropdownMenu.Portal>
                  <DropdownMenu.Content class="z-50 min-w-44 rounded-md border border-zinc-200 bg-white py-1 shadow-md" align="end">
                    <DropdownMenu.Item class={menuItem} onSelect={() => viewActivity(`key:${k.name}`)}>
                      <History class="size-3.5" /> View activity
                    </DropdownMenu.Item>
                    <DropdownMenu.Item class={menuItem + " text-red-600"} onSelect={() => revokeKey(k)}>
                      <Trash2 class="size-3.5" /> Revoke key
                    </DropdownMenu.Item>
                  </DropdownMenu.Content>
                </DropdownMenu.Portal>
              </DropdownMenu.Root>
            </div>
          </li>
        {:else}
          <li class="px-4 py-8 text-center text-sm text-zinc-500">
            {#if !keysLoaded}
              Loading keys…
            {:else if query || roleFilter}
              No keys match.
            {:else}
              No API keys yet. Create one per agent or script, so each can be revoked on its own.
            {/if}
          </li>
        {/each}
      {/if}
    </ul>
  </div>
  {#if tab === "keys"}
    <p class="mt-2 text-xs text-zinc-500">Keys are shown in full only once and stored hashed - revoke immediately if one leaks.</p>
  {/if}
</div>

{#snippet roleSelect(value, setValue, label)}
  <Select.Root type="single" {value} onValueChange={setValue}>
    <Select.Trigger class="{selectTrigger} w-full" aria-label={label}>
      <span class="truncate">{value}</span>
      <ChevronDown class="size-3.5 shrink-0 text-zinc-500" />
    </Select.Trigger>
    <Select.Portal>
      <Select.Content class={selectContent}>
        {#each ROLES as r (r)}
          <Select.Item value={r} class={selectItem}>
            <div class="font-medium">{r}</div>
            <div class="text-xs text-zinc-500">{roleShort[r]}</div>
          </Select.Item>
        {/each}
      </Select.Content>
    </Select.Portal>
  </Select.Root>
{/snippet}

<Dialog.Root bind:open={addOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class={dialogOverlay} />
    <Dialog.Content class="{dialogContent} w-96 max-w-[calc(100vw-2rem)]">
      <Dialog.Title class="mb-1 flex items-center gap-2 font-semibold">
        <UserRoundPlus class="size-4" /> Add person
      </Dialog.Title>
      <Dialog.Description class="mb-4 text-sm text-zinc-500">
        For someone who signs in to an app built on {branding.name}, or an admin of this dashboard. Scripts and agents get an API key instead.
      </Dialog.Description>
      <form onsubmit={addUser} class="flex flex-col gap-3">
        <input class={inputCls} type="text" placeholder="Username" aria-label="Username" bind:value={username} required />
        {@render roleSelect(role, (v) => (role = v), "Role")}
        <p class="text-xs text-zinc-500">A random password is generated and shown once. They can change it by signing in at this site.</p>
        <div class="mt-1 flex justify-end gap-2">
          <Dialog.Close class={btnSecondary}>Cancel</Dialog.Close>
          <button class={btnPrimary} disabled={busy}>{busy ? "Adding…" : "Add person"}</button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<Dialog.Root bind:open={keyOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class={dialogOverlay} />
    <Dialog.Content class="{dialogContent} w-96 max-w-[calc(100vw-2rem)]">
      <Dialog.Title class="mb-1 flex items-center gap-2 font-semibold">
        <KeyRound class="size-4" /> Create API key
      </Dialog.Title>
      <Dialog.Description class="mb-4 text-sm text-zinc-500">
        For a script, agent or server. Never put a key in browser code - browser apps sign people in instead.
      </Dialog.Description>
      <form onsubmit={createKey} class="flex flex-col gap-3">
        <input class={inputCls} type="text" placeholder="Key name (e.g. my-ai-agent)" aria-label="Key name" bind:value={keyName} required />
        {@render roleSelect(keyRole, (v) => (keyRole = v), "Key role")}
        <p class="text-xs text-zinc-500">builder is the default: an agent can build against the API without reading a single row.</p>
        <div class="mt-1 flex justify-end gap-2">
          <Dialog.Close class={btnSecondary}>Cancel</Dialog.Close>
          <button class={btnPrimary} disabled={keysBusy}>{keysBusy ? "Creating…" : "Create key"}</button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

{#snippet secretBlock(secret)}
  <pre class="mt-3 rounded-lg bg-zinc-50 p-3 font-mono text-xs break-all whitespace-pre-wrap select-all">{secret}</pre>
  {#if secretWarn && !secretSaved}
    <p class="mt-3 rounded-lg bg-amber-50 p-2.5 text-xs text-amber-800" role="alert">
      You haven't copied or saved it yet. It can't be shown again once this closes.
    </p>
  {/if}
{/snippet}

<!-- one-time secrets: Esc and outside clicks don't close these dialogs -->
<Dialog.Root bind:open={credsOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class={dialogOverlay} />
    <Dialog.Content
      class="{dialogContent} w-96 max-w-[calc(100vw-2rem)]"
      escapeKeydownBehavior="ignore"
      interactOutsideBehavior="ignore"
    >
      <Dialog.Title class="flex items-center gap-2 font-semibold">
        <LockKeyhole class="size-4" /> {creds?.username}
      </Dialog.Title>
      <Dialog.Description class="mt-1 text-sm text-zinc-600">
        Copy this password now - it is never shown again. The login file has the username too.
      </Dialog.Description>
      {@render secretBlock(creds?.password)}
      <div class="mt-3 flex flex-wrap justify-end gap-2">
        <button class="{btnSecondary} flex items-center gap-1.5" onclick={downloadCreds}
        ><Download class="size-3.5" /> Login file</button>
        <CopyButton
          class="{btnSecondary} flex items-center gap-1.5"
          title="Copy password"
          label="Copy"
          text={creds?.password ?? ""}
          oncopied={() => (secretSaved = true)}
        />
        <button class={btnPrimary} onclick={() => closeSecret(() => (credsOpen = false))}
        >{secretWarn && !secretSaved ? "Close anyway" : "Done"}</button>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<Dialog.Root bind:open={newKeyOpen} onOpenChange={(o) => { if (!o) newKey = null; }}>
  <Dialog.Portal>
    <Dialog.Overlay class={dialogOverlay} />
    <Dialog.Content
      class="{dialogContent} w-96 max-w-[calc(100vw-2rem)]"
      escapeKeydownBehavior="ignore"
      interactOutsideBehavior="ignore"
    >
      <Dialog.Title class="flex items-center gap-2 font-semibold">
        <KeyRound class="size-4" /> {newKey?.name}
      </Dialog.Title>
      <Dialog.Description class="mt-1 text-sm text-zinc-600">
        Copy this key now - it is never shown again.
      </Dialog.Description>
      {@render secretBlock(newKey?.key)}
      <div class="mt-3 flex justify-end gap-2">
        <CopyButton
          class="{btnSecondary} flex items-center gap-1.5"
          title="Copy key"
          label="Copy"
          text={newKey?.key ?? ""}
          oncopied={() => (secretSaved = true)}
        />
        <button class={btnPrimary} onclick={() => closeSecret(() => { newKeyOpen = false; newKey = null; })}
        >{secretWarn && !secretSaved ? "Close anyway" : "Done"}</button>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<PromptDialog
  bind:open={passOpen}
  bind:value={passValue}
  title={`Set password for ${passTarget}`}
  label="Min 8 characters - changing it signs that user out everywhere."
  password
  minLength={8}
  submitLabel="Set password"
  onsubmit={(v) => setPassword(v)}
/>

<Dialog.Root bind:open={roleOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class={dialogOverlay} />
    <Dialog.Content class="{dialogContent} w-96 max-w-[calc(100vw-2rem)]">
      <Dialog.Title class="mb-1 flex items-center gap-2 font-semibold">
        <UserCog class="size-4" /> Role for {roleTarget?.username}
      </Dialog.Title>
      <Dialog.Description class="mb-4 text-sm text-zinc-500">
        Applies right away, including to sessions already signed in.
      </Dialog.Description>
      <form onsubmit={saveRole} class="flex flex-col gap-3">
        {@render roleSelect(roleValue, (v) => (roleValue = v), "Role")}
        <p class="text-xs leading-5 text-zinc-500">{roleDesc[roleValue]}</p>
        <div class="mt-1 flex justify-end gap-2">
          <Dialog.Close class={btnSecondary}>Cancel</Dialog.Close>
          <button class={btnPrimary} disabled={roleBusy}>{roleBusy ? "Saving…" : "Change role"}</button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

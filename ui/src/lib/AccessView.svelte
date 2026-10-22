<script>
  import { Dialog, Select, DropdownMenu } from "bits-ui";
  import Trash2 from "@lucide/svelte/icons/trash-2";
  import LockKeyhole from "@lucide/svelte/icons/lock-keyhole";
  import UserRoundPlus from "@lucide/svelte/icons/user-round-plus";
  import UserRoundX from "@lucide/svelte/icons/user-round-x";
  import KeyRound from "@lucide/svelte/icons/key-round";
  import Plus from "@lucide/svelte/icons/plus";
  import Ellipsis from "@lucide/svelte/icons/ellipsis";
  import UserCog from "@lucide/svelte/icons/user-cog";
  import CopyButton from "./CopyButton.svelte";
  import Download from "@lucide/svelte/icons/download";
  import GenArt from "./GenArt.svelte";
  import RoleBadge from "./RoleBadge.svelte";
  import PromptDialog from "./PromptDialog.svelte";
  import { api } from "./api.js";
  import { inputCls, dialogOverlay, dialogContent, btnPrimary, btnSecondary } from "./cls.js";
  import { auth } from "./auth.svelte.js";
  import { branding } from "./branding.svelte.js";
  import { showToast } from "./toast.svelte.js";
  import { askConfirm } from "./confirm.svelte.js";
  import { ago } from "./fmt.js";
  import { selectTrigger, selectContent, selectItem } from "./cls.js";
  import ChevronDown from "@lucide/svelte/icons/chevron-down";

  let users = $state([]);
  let addOpen = $state(false);
  let username = $state("");
  let role = $state("viewer");
  let busy = $state(false);
  // generated password from the last create, shown once
  let creds = $state(null);
  let credsOpen = $state(false);
  let usersLoaded = $state(false);
  let keysLoaded = $state(false);

  // one-time secrets (new password / new key): closing before a copy or
  // download asks once
  let secretSaved = $state(false);
  let secretWarn = $state(false);

  function closeSecret(close) {
    if (!secretSaved && !secretWarn) {
      secretWarn = true;
      return;
    }
    close();
  }

  async function load() {
    try {
      users = await api("/api/users/");
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      usersLoaded = true;
    }
  }

  $effect(() => {
    load();
    loadKeys();
  });

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
      await load();
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
      `URL:      ${location.origin}`,
      `Username: ${creds.username}`,
      `Password: ${creds.password}`,
      "",
      "Please sign in and change this password right away.",
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
      await load();
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
      await load();
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  // API keys for machines: agents, scripts, cron jobs
  let keys = $state([]);
  let keyName = $state("");
  let keyRole = $state("builder");
  let newKey = $state(null); // full key, shown once
  let newKeyOpen = $state(false);
  let keysBusy = $state(false);

  async function loadKeys() {
    try {
      keys = await api("/api/keys/");
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      keysLoaded = true;
    }
  }

  async function createKey(e) {
    e.preventDefault();
    if (!keyName.trim()) return;
    keysBusy = true;
    try {
      const res = await api("/api/keys/", {
        method: "POST",
        body: JSON.stringify({ name: keyName.trim(), role: keyRole }),
      });
      newKey = { ...res };
      secretSaved = false;
      secretWarn = false;
      newKeyOpen = true;
      keyName = "";
      keyRole = "builder";
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

  const roleDesc = {
    admin: "full access, including this dashboard",
    editor: "can edit data (API only)",
    viewer: "read only (API only)",
    builder: "schema and docs only, never rows (API only)",
  };
  const roleItems = [
    { value: "builder" },
    { value: "viewer" },
    { value: "editor" },
    { value: "admin" },
  ];
  const menuItem =
    "flex cursor-pointer items-center gap-2 px-3 py-2 text-sm data-highlighted:bg-zinc-100";
</script>

<div class="w-full max-w-5xl">
  <div class="mb-3 flex items-start gap-3">
    <div class="min-w-0 flex-1">
      <h2 class="text-xl font-semibold tracking-tight text-zinc-900">Access</h2>
      <p class="mt-0.5 text-sm text-zinc-500">Human accounts and API keys - who can sign in, and what machines can do</p>
      <div class="mt-1.5 flex flex-wrap gap-1.5">
        <span class="rounded-full bg-zinc-100 px-2 py-0.5 text-[11px] text-zinc-600">{users.length} user{users.length === 1 ? "" : "s"}</span>
        <span class="rounded-full bg-zinc-100 px-2 py-0.5 text-[11px] text-zinc-600">{keys.length} key{keys.length === 1 ? "" : "s"}</span>
        {#each ["admin", "editor", "viewer", "builder"] as r (r)}
          {@const n = users.filter((u) => u.role === r).length}
          {#if n > 0}
            <span class="rounded-full bg-zinc-100 px-2 py-0.5 text-[11px] text-zinc-600">{n} {r}{n === 1 ? "" : "s"}</span>
          {/if}
        {/each}
      </div>
    </div>
    {#if auth.role === "admin"}
      <button
        class="rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50"
        onclick={() => (addOpen = true)}
      ><span class="flex items-center gap-1.5"><UserRoundPlus class="size-3.5" /> Add user</span></button>
    {/if}
  </div>

<div class="flex flex-col gap-4 lg:grid lg:grid-cols-3 lg:items-start">
  <div class="overflow-hidden rounded-lg border border-zinc-200 bg-white lg:col-span-2">
    <ul class="divide-y divide-zinc-200">
      {#each users as u (u.username)}
        <li class="group flex items-center gap-3 px-4 py-2.5 hover:bg-zinc-50">
          <GenArt seed={u.username} variant={3} w={64} h={64} class="size-8 shrink-0 rounded-full border border-zinc-200" />
          <div class="min-w-0 flex-1">
            <div class="truncate text-sm font-medium">
              {u.username}
              {#if u.username === auth.username}
                <span class="ml-1 text-xs font-normal text-zinc-500">(you)</span>
              {/if}
            </div>
            <div class="text-xs text-zinc-500">{roleDesc[u.role] ?? roleDesc.viewer}</div>
          </div>
          <RoleBadge role={u.role} />
          {#if auth.role === "admin" && u.username !== auth.username}
            <DropdownMenu.Root>
              <DropdownMenu.Trigger
                class="rounded p-1 text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900"
                title="User actions"
                aria-label="Actions for {u.username}"
              >
                <Ellipsis class="size-4" />
              </DropdownMenu.Trigger>
              <DropdownMenu.Portal>
                <DropdownMenu.Content
                  class="z-50 min-w-40 rounded-md border border-zinc-200 bg-white py-1 shadow-md"
                  align="end"
                >
                  <DropdownMenu.Item class={menuItem} onSelect={() => askRole(u)}>
                    <UserCog class="size-3.5" /> Change role
                  </DropdownMenu.Item>
                  <DropdownMenu.Item class={menuItem} onSelect={() => askSetPassword(u.username)}>
                    <LockKeyhole class="size-3.5" /> Set password
                  </DropdownMenu.Item>
                  <DropdownMenu.Item class={menuItem + " text-red-600"} onSelect={() => deleteUser(u.username)}>
                    <Trash2 class="size-3.5" /> Delete user
                  </DropdownMenu.Item>
                </DropdownMenu.Content>
              </DropdownMenu.Portal>
            </DropdownMenu.Root>
          {/if}
        </li>
      {:else}
        <li class="px-4 py-8 text-center text-sm text-zinc-500">
          {#if usersLoaded}
            <UserRoundX class="mx-auto mb-1 size-6 text-zinc-300" />
            No users found
          {:else}
            Loading users…
          {/if}
        </li>
      {/each}
    </ul>
  </div>

  <!-- roles reference: a card on wide screens, a disclosure on phones -->
  {#snippet roleList()}
    {#each [["admin", "Full access: tables, columns, rows, import/export, users, API keys, branding."], ["editor", "Create, edit and delete rows in every table."], ["viewer", "Read-only: browse tables and rows, export."], ["builder", "Builds against the API without seeing data: table names, columns and docs only, never rows."]] as [r, text] (r)}
      <div class="rounded-lg border border-zinc-100 bg-zinc-50/60 p-3">
        <RoleBadge role={r} />
        <p class="mt-1.5 text-xs leading-5 text-zinc-500">{text}</p>
      </div>
    {/each}
  {/snippet}
  <div class="hidden flex-col gap-3 rounded-lg border border-zinc-200 bg-white p-4 lg:flex">
    <div class="text-sm font-semibold">Roles</div>
    {@render roleList()}
  </div>
  <details class="group rounded-lg border border-zinc-200 bg-white lg:hidden">
    <summary class="flex cursor-pointer list-none items-center justify-between px-4 py-3 text-sm font-semibold">
      What do the roles mean?
      <ChevronDown class="size-4 text-zinc-500 transition-transform group-open:rotate-180" />
    </summary>
    <div class="flex flex-col gap-3 px-4 pb-4">{@render roleList()}</div>
  </details>

  <!-- API keys for machines: agents, scripts, cron jobs -->
  <div class="rounded-lg border border-zinc-200 bg-white p-4 lg:col-span-3">
    <div class="mb-2 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm font-semibold">
      <KeyRound class="size-4" /> API keys
      <span class="basis-full text-xs font-normal text-zinc-500 sm:basis-auto">for agents &amp; scripts - browser apps use the human login instead</span>
    </div>
    <ul class="flex flex-col divide-y divide-zinc-100">
      {#each keys as k (k.id)}
        <li class="flex items-center gap-3 py-2">
          <span class="min-w-0 flex-1">
            <span class="block truncate text-sm font-medium">{k.name}</span>
            <span class="block font-mono text-xs text-zinc-500">{k.prefix}… · created {ago(k.createdAt)} · used {ago(k.lastUsedAt)}</span>
          </span>
          <RoleBadge role={k.role} />
          <button
            class="rounded p-1.5 text-zinc-500 hover:bg-zinc-100 hover:text-red-600"
            title="Revoke key"
            aria-label="Revoke key {k.name}"
            onclick={() => revokeKey(k)}
          ><Trash2 class="size-3.5" /></button>
        </li>
      {:else}
        <li class="py-2 text-xs text-zinc-500">{keysLoaded ? "No keys yet - create one for each AI agent or script." : "Loading keys…"}</li>
      {/each}
    </ul>

    <form onsubmit={createKey} class="mt-2 flex flex-wrap items-center gap-2">
      <input
        class="{inputCls} min-w-40 flex-1"
        type="text"
        placeholder="Key name (e.g. my-ai-agent)"
        aria-label="Key name"
        bind:value={keyName}
        required
      />
      <Select.Root type="single" bind:value={keyRole}>
        <Select.Trigger class="{selectTrigger} w-44" aria-label="Key role">
          <span class="truncate">{keyRole}</span>
          <ChevronDown class="size-3.5 shrink-0 text-zinc-500" />
        </Select.Trigger>
        <Select.Portal>
          <Select.Content class={selectContent}>
            {#each ["builder", "viewer", "editor", "admin"] as r (r)}
              <Select.Item
                value={r}
                class={selectItem}
              >
                <div class="font-medium">{r}</div>
                <div class="text-xs text-zinc-500">{r === "builder" ? "schema and docs only, never rows" : r === "viewer" ? "read only" : r === "editor" ? "can edit data" : "full access"}</div>
              </Select.Item>
            {/each}
          </Select.Content>
        </Select.Portal>
      </Select.Root>
      <button
        class="flex items-center gap-1.5 rounded-lg border border-zinc-300 bg-white px-3 py-2 text-sm hover:bg-zinc-50 disabled:opacity-50"
        disabled={keysBusy}
      ><Plus class="size-3.5" /> {keysBusy ? "Creating…" : "Create key"}</button>
    </form>

    <p class="mt-2 text-xs text-zinc-500">
      Keys are shown in full only once and stored hashed - revoke immediately if one leaks.
    </p>
  </div>
</div>
</div>

<Dialog.Root bind:open={addOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class={dialogOverlay} />
    <Dialog.Content class={dialogContent}>
      <Dialog.Title class="mb-1 flex items-center gap-2 font-semibold">
        <UserRoundPlus class="size-4" /> Add user
      </Dialog.Title>
      <Dialog.Description class="mb-4 text-sm text-zinc-500">
        A random password is generated - hand it over once, they change it on first sign-in.
      </Dialog.Description>
      <form onsubmit={addUser} class="flex flex-col gap-3">
        <input class={inputCls} type="text" placeholder="Username" aria-label="Username" bind:value={username} required />
        <Select.Root type="single" bind:value={role}>
          <Select.Trigger class="{selectTrigger} w-full" aria-label="Role">
            <span class="truncate">{role}</span>
            <ChevronDown class="size-3.5 shrink-0 text-zinc-500" />
          </Select.Trigger>
          <Select.Portal>
            <Select.Content class={selectContent}>
              {#each roleItems as r (r.value)}
                <Select.Item
                  value={r.value}
                  class={selectItem}
                >
                  <div class="font-medium">{r.value}</div>
                  <div class="text-xs text-zinc-500">{roleDesc[r.value]}</div>
                </Select.Item>
              {/each}
            </Select.Content>
          </Select.Portal>
        </Select.Root>
        <div class="mt-1 flex justify-end gap-2">
          <Dialog.Close
            class="rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50"
          >Cancel</Dialog.Close>
          <button
            class="bg-brand rounded-lg px-3 py-1.5 text-sm font-medium text-brand-on disabled:opacity-50"
            disabled={busy}
          >{busy ? "Creating…" : "Create user"}</button>
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
        Copy this password now - it is never shown again. The login file carries the URL too.
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
        <Select.Root type="single" bind:value={roleValue}>
          <Select.Trigger class="{selectTrigger} w-full" aria-label="Role">
            <span class="truncate">{roleValue}</span>
            <ChevronDown class="size-3.5 shrink-0 text-zinc-500" />
          </Select.Trigger>
          <Select.Portal>
            <Select.Content class={selectContent}>
              {#each roleItems as r (r.value)}
                <Select.Item value={r.value} class={selectItem}>
                  <div class="font-medium">{r.value}</div>
                  <div class="text-xs text-zinc-500">{roleDesc[r.value]}</div>
                </Select.Item>
              {/each}
            </Select.Content>
          </Select.Portal>
        </Select.Root>
        <div class="mt-1 flex justify-end gap-2">
          <Dialog.Close class={btnSecondary}>Cancel</Dialog.Close>
          <button class={btnPrimary} disabled={roleBusy}>{roleBusy ? "Saving…" : "Change role"}</button>
        </div>
      </form>
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

<script>
  import { Dialog, Select, DropdownMenu } from "bits-ui";
  import Trash from "@lucide/svelte/icons/trash";
  import UserRoundPlus from "@lucide/svelte/icons/user-round-plus";
  import UserRoundX from "@lucide/svelte/icons/user-round-x";
  import KeyRound from "@lucide/svelte/icons/key-round";
  import Plus from "@lucide/svelte/icons/plus";
  import Ellipsis from "@lucide/svelte/icons/ellipsis";
  import Copy from "@lucide/svelte/icons/copy";
  import Download from "@lucide/svelte/icons/download";
  import GenArt from "./GenArt.svelte";
  import RoleBadge from "./RoleBadge.svelte";
  import PromptDialog from "./PromptDialog.svelte";
  import { api } from "./api.js";
  import { inputCls, dialogOverlay, dialogContent } from "./cls.js";
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

  async function load() {
    try {
      users = await api("/api/users/");
    } catch (err) {
      showToast(err.message, "error");
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
  }

  async function copyPassword() {
    try {
      await navigator.clipboard.writeText(creds.password);
      showToast("Copied");
    } catch {}
  }

  let passOpen = $state(false);
  let passTarget = $state("");
  let passValue = $state("");

  function askSetPassword(name) {
    passTarget = name;
    passValue = "";
    passOpen = true;
  }

  async function setPassword(v) {
    try {
      await api(`/api/users/${encodeURIComponent(passTarget)}`, {
        method: "PATCH",
        body: JSON.stringify({ password: v }),
      });
      showToast(`Password changed for ${passTarget}`);
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  async function deleteUser(name) {
    if (!(await askConfirm(`User "${name}" will be removed.`, "Delete user?"))) return;
    try {
      await api(`/api/users/${encodeURIComponent(name)}`, { method: "DELETE" });
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
      await loadKeys();
    } catch (err) {
      showToast(err.message, "error");
    }
  }

  async function copyKey() {
    try {
      await navigator.clipboard.writeText(newKey.key);
      showToast("Copied");
    } catch {}
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

<div class="mx-auto w-full max-w-3xl">
  <div class="mb-3 flex items-start gap-3">
    <div class="min-w-0 flex-1">
      <h2 class="text-sm font-semibold">Access</h2>
      <p class="text-xs text-zinc-500">Human accounts and API keys - who can sign in, and what machines can do</p>
      <div class="mt-1.5 flex flex-wrap gap-1.5">
        <span class="rounded-full bg-zinc-100 px-2 py-0.5 text-[11px] text-zinc-600">{users.length} user{users.length === 1 ? "" : "s"}</span>
        <span class="rounded-full bg-zinc-100 px-2 py-0.5 text-[11px] text-zinc-600">{keys.length} key{keys.length === 1 ? "" : "s"}</span>
        {#each ["admin", "editor", "viewer", "builder"] as r (r)}
          {@const n = users.filter((u) => u.role === r).length}
          {#if n > 0}
            <span class="rounded-full border border-zinc-200 px-2 py-0.5 text-[11px] text-zinc-500">{n} {r}</span>
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

<div class="flex flex-col gap-4 lg:grid lg:grid-cols-3 lg:items-stretch">
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
              >
                <Ellipsis class="size-4" />
              </DropdownMenu.Trigger>
              <DropdownMenu.Portal>
                <DropdownMenu.Content
                  class="z-50 min-w-40 rounded-md border border-zinc-200 bg-white py-1 shadow-md"
                  align="end"
                >
                  <DropdownMenu.Item class={menuItem} onSelect={() => askSetPassword(u.username)}>
                    <KeyRound class="size-3.5" /> Set password
                  </DropdownMenu.Item>
                  <DropdownMenu.Item class={menuItem + " text-red-600"} onSelect={() => deleteUser(u.username)}>
                    <Trash class="size-3.5" /> Delete user
                  </DropdownMenu.Item>
                </DropdownMenu.Content>
              </DropdownMenu.Portal>
            </DropdownMenu.Root>
          {/if}
        </li>
      {:else}
        <li class="px-4 py-8 text-center text-sm text-zinc-500">
          <UserRoundX class="mx-auto mb-1 size-6 text-zinc-300" />
          No users yet 🌱
        </li>
      {/each}
    </ul>
  </div>

  <!-- roles reference: helps pick the right role when adding someone -->
  <div class="flex flex-col gap-3 rounded-lg border border-zinc-200 bg-white p-4">
    <div class="text-sm font-semibold">Roles</div>
    <div class="rounded-lg border border-zinc-100 bg-zinc-50/60 p-3">
      <RoleBadge role="admin" />
      <p class="mt-1.5 text-xs leading-5 text-zinc-500">Full access: tables, columns, rows, import/export, users, API keys, branding.</p>
    </div>
    <div class="rounded-lg border border-zinc-100 bg-zinc-50/60 p-3">
      <RoleBadge role="editor" />
      <p class="mt-1.5 text-xs leading-5 text-zinc-500">Create, edit and delete rows in every table.</p>
    </div>
    <div class="rounded-lg border border-zinc-100 bg-zinc-50/60 p-3">
      <RoleBadge role="viewer" />
      <p class="mt-1.5 text-xs leading-5 text-zinc-500">Read-only: browse tables and rows, export.</p>
    </div>
    <div class="rounded-lg border border-zinc-100 bg-zinc-50/60 p-3">
      <RoleBadge role="builder" />
      <p class="mt-1.5 text-xs leading-5 text-zinc-500">Builds against the API without seeing data: table names, columns and docs only, never rows.</p>
    </div>
  </div>

  <!-- API keys for machines: agents, scripts, cron jobs -->
  <div class="rounded-lg border border-zinc-200 bg-white p-4 lg:col-span-3">
    <div class="mb-2 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm font-semibold">
      <KeyRound class="size-4" /> API keys
      <span class="basis-full text-xs font-normal text-zinc-400 sm:basis-auto">for agents &amp; scripts - browser apps use the human login instead</span>
    </div>
    <ul class="flex flex-col divide-y divide-zinc-100">
      {#each keys as k (k.id)}
        <li class="flex items-center gap-3 py-2">
          <span class="min-w-0 flex-1">
            <span class="block truncate text-sm font-medium">{k.name}</span>
            <span class="block font-mono text-xs text-zinc-400">{k.prefix}… · created {ago(k.createdAt)} · used {ago(k.lastUsedAt)}</span>
          </span>
          <RoleBadge role={k.role} />
          <button
            class="rounded p-1.5 text-zinc-500 hover:bg-zinc-100 hover:text-red-600"
            title="Revoke key"
            onclick={() => revokeKey(k)}
          ><Trash class="size-3.5" /></button>
        </li>
      {:else}
        <li class="py-2 text-xs text-zinc-400">No keys yet - create one for each AI agent or script.</li>
      {/each}
    </ul>

    <form onsubmit={createKey} class="mt-2 flex flex-wrap items-center gap-2">
      <input
        class="min-w-40 flex-1 rounded-lg border border-zinc-300 bg-white px-3 py-2 text-sm focus:outline-none"
        type="text"
        placeholder="Key name (e.g. my-ai-agent)"
        bind:value={keyName}
        required
      />
      <Select.Root type="single" bind:value={keyRole}>
        <Select.Trigger class="{selectTrigger} w-44">
          <span class="truncate">{keyRole}</span>
          <ChevronDown class="size-3.5 shrink-0 text-zinc-400" />
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
      ><Plus class="size-3.5" /> Create key</button>
    </form>

    <p class="mt-2 text-xs text-zinc-400">
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
      <Dialog.Description class="mb-4 text-sm text-zinc-400">
        A random password is generated - hand it over once, they change it on first sign-in.
      </Dialog.Description>
      <form onsubmit={addUser} class="flex flex-col gap-3">
        <input class={inputCls} type="text" placeholder="Username" bind:value={username} required />
        <Select.Root type="single" bind:value={role}>
          <Select.Trigger class="{selectTrigger} w-full">
            <span class="truncate">{role}</span>
            <ChevronDown class="size-3.5 shrink-0 text-zinc-400" />
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
          >Create</button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<Dialog.Root bind:open={credsOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class={dialogOverlay} />
    <Dialog.Content class="{dialogContent} w-96">
      <Dialog.Title class="flex items-center gap-2 font-semibold">
        <KeyRound class="size-4" /> {creds?.username}
      </Dialog.Title>
      <Dialog.Description class="mt-1 text-sm text-zinc-400">
        Copy this password now - it is never shown again. The login file carries the URL too.
      </Dialog.Description>
      <pre class="mt-3 rounded-lg bg-zinc-50 p-3 font-mono text-xs break-all whitespace-pre-wrap">{creds?.password}</pre>
      <div class="mt-3 flex justify-end gap-2">
        <button
          class="flex items-center gap-1.5 rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50"
          onclick={downloadCreds}
        ><Download class="size-3.5" /> Login file</button>
        <button
          class="flex items-center gap-1.5 rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50"
          onclick={copyPassword}
        ><Copy class="size-3.5" /> Copy</button>
        <Dialog.Close
          class="bg-brand rounded-lg px-3 py-1.5 text-sm font-medium text-brand-on"
        >Done</Dialog.Close>
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
  onsubmit={(v) => setPassword(v)}
/>

<Dialog.Root bind:open={newKeyOpen} onOpenChange={(o) => { if (!o) newKey = null; }}>
  <Dialog.Portal>
    <Dialog.Overlay class={dialogOverlay} />
    <Dialog.Content class="{dialogContent} w-96 max-w-[calc(100vw-2rem)]">
      <Dialog.Title class="flex items-center gap-2 font-semibold">
        <KeyRound class="size-4" /> {newKey?.name}
      </Dialog.Title>
      <Dialog.Description class="mt-1 text-sm text-zinc-400">
        Copy this key now - it is never shown again.
      </Dialog.Description>
      <pre class="mt-3 rounded-lg bg-zinc-50 p-3 font-mono text-xs break-all whitespace-pre-wrap">{newKey?.key}</pre>
      <div class="mt-3 flex justify-end gap-2">
        <button
          class="flex items-center gap-1.5 rounded-lg border border-zinc-300 bg-white px-3 py-1.5 text-sm hover:bg-zinc-50"
          onclick={copyKey}
        ><Copy class="size-3.5" /> Copy</button>
        <Dialog.Close
          class="bg-brand rounded-lg px-3 py-1.5 text-sm font-medium text-brand-on"
        >Done</Dialog.Close>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

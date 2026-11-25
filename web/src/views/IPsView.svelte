<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { IPAddress, IPScope, IPVisibility, PaginatedResponse, User } from '../lib/types';

  let ips = $state<IPAddress[]>([]);
  let users = $state<User[]>([]);
  let loading = $state(true);
  let syncing = $state(false);

  let isAdmin = $derived(auth.user?.role === 'admin');

  // Add IP modal
  let formOpen = $state(false);
  let fIp = $state('');
  let fScope = $state<IPScope>('shared');
  let fVisibility = $state<IPVisibility>('public');
  let fInterface = $state('');
  let fOwnerId = $state('');
  let formError = $state('');
  let saving = $state(false);

  // Delete modal
  let deleteTarget = $state<IPAddress | null>(null);
  let deleting = $state(false);

  async function loadIPs() {
    loading = true;
    try {
      const res = await api.get<PaginatedResponse<IPAddress>>('/ips');
      ips = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
    }
  }

  async function loadUsers() {
    if (!isAdmin) return;
    try {
      const res = await api.get<PaginatedResponse<User>>('/users?limit=200&offset=0');
      users = res.items;
    } catch {
      // ignore
    }
  }

  onMount(() => {
    loadIPs();
    loadUsers();
  });

  async function syncFromServer() {
    syncing = true;
    try {
      const res = await api.post<{ added: number; items: IPAddress[] }>('/ips/sync');
      toast.success(fmt(i18n.t.ips.syncSuccess, { added: res.added }));
      await loadIPs();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      syncing = false;
    }
  }

  function openCreate() {
    fIp = '';
    fScope = 'shared';
    fVisibility = 'public';
    fInterface = '';
    fOwnerId = '';
    formError = '';
    formOpen = true;
  }

  async function submitForm(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    saving = true;
    try {
      const body: Record<string, unknown> = {
        ip: fIp.trim(),
        scope: fScope,
        visibility: fVisibility,
      };
      if (fInterface.trim()) body.interface_name = fInterface.trim();
      if (fOwnerId) body.owner_id = fOwnerId;
      await api.post<IPAddress>('/ips', body);
      toast.success(i18n.t.ips.createdSuccess);
      formOpen = false;
      await loadIPs();
    } catch (err) {
      if (err instanceof ApiError) {
        const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
        formError = fieldMsg || err.detail || err.title;
      } else {
        formError = i18n.t.common.errorGeneric;
      }
    } finally {
      saving = false;
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    try {
      await api.delete(`/ips/${deleteTarget.id}`);
      toast.success(i18n.t.ips.deletedSuccess);
      deleteTarget = null;
      await loadIPs();
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        toast.error(i18n.t.ips.inUseError);
      } else {
        toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      }
      deleteTarget = null;
    } finally {
      deleting = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.ips.title}</h1>
    <p class="page-subtitle">{i18n.t.ips.subtitle}</p>
  </div>
  {#if isAdmin}
    <div class="table-actions">
      <button type="button" class="btn" onclick={syncFromServer} disabled={syncing}>
        {syncing ? i18n.t.common.loading : i18n.t.ips.syncFromServer}
      </button>
      <button type="button" class="btn btn-primary" onclick={openCreate}>
        + {i18n.t.ips.addIp}
      </button>
    </div>
  {/if}
</div>

<div class="toolbar">
  <div></div>
  <button type="button" class="btn btn-sm" onclick={loadIPs} disabled={loading}>
    {i18n.t.common.refresh}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.ips.ip}</th>
        <th>{i18n.t.ips.family}</th>
        <th>{i18n.t.ips.scope}</th>
        <th>{i18n.t.ips.visibility}</th>
        <th>{i18n.t.ips.interfaceName}</th>
        {#if isAdmin}
          <th>{i18n.t.common.actions}</th>
        {/if}
      </tr>
    </thead>
    <tbody>
      {#if loading && ips.length === 0}
        <tr><td colspan={isAdmin ? 6 : 5} class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if ips.length === 0}
        <tr><td colspan={isAdmin ? 6 : 5} class="empty-state">{i18n.t.common.noData}</td></tr>
      {:else}
        {#each ips as item (item.id)}
          <tr>
            <td class="mono"><strong>{item.ip}</strong></td>
            <td><span class="badge badge-primary">{item.family}</span></td>
            <td>{item.scope === 'shared' ? i18n.t.ips.shared : i18n.t.ips.dedicated}</td>
            <td>{item.visibility === 'public' ? i18n.t.ips.public : i18n.t.ips.internal}</td>
            <td class="mono">{item.interface_name || '—'}</td>
            {#if isAdmin}
              <td>
                <div class="table-actions">
                  <button
                    type="button"
                    class="btn btn-sm btn-danger"
                    onclick={() => (deleteTarget = item)}
                  >
                    {i18n.t.common.delete}
                  </button>
                </div>
              </td>
            {/if}
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>

<Modal open={formOpen} title={i18n.t.ips.addIp} onclose={() => (formOpen = false)}>
  <form id="ip-form" onsubmit={submitForm}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="ip-addr">{i18n.t.ips.ip}</label>
      <input
        id="ip-addr"
        type="text"
        class="form-control"
        bind:value={fIp}
        required
        placeholder="203.0.113.10"
      />
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="ip-scope">{i18n.t.ips.scope}</label>
        <select id="ip-scope" class="form-control" bind:value={fScope}>
          <option value="shared">{i18n.t.ips.shared}</option>
          <option value="dedicated">{i18n.t.ips.dedicated}</option>
        </select>
      </div>
      <div class="form-group">
        <label class="form-label" for="ip-vis">{i18n.t.ips.visibility}</label>
        <select id="ip-vis" class="form-control" bind:value={fVisibility}>
          <option value="public">{i18n.t.ips.public}</option>
          <option value="internal">{i18n.t.ips.internal}</option>
        </select>
      </div>
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="ip-iface">{i18n.t.ips.interfaceName}</label>
        <input
          id="ip-iface"
          type="text"
          class="form-control"
          bind:value={fInterface}
          placeholder="eth0"
        />
      </div>
      {#if users.length > 0}
        <div class="form-group">
          <label class="form-label" for="ip-owner">{i18n.t.ips.owner}</label>
          <select id="ip-owner" class="form-control" bind:value={fOwnerId}>
            <option value="">{i18n.t.common.none}</option>
            {#each users as u (u.id)}
              <option value={u.id}>{u.username}</option>
            {/each}
          </select>
        </div>
      {/if}
    </div>
  </form>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (formOpen = false)} disabled={saving}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="ip-form" class="btn btn-primary" disabled={saving}>
      {saving ? i18n.t.common.loading : i18n.t.common.save}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.ips.deleteConfirmTitle}
  onclose={() => (deleteTarget = null)}
>
  {#if deleteTarget}
    <p>{fmt(i18n.t.ips.deleteConfirmBody, { ip: deleteTarget.ip })}</p>
  {/if}
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (deleteTarget = null)} disabled={deleting}>
      {i18n.t.common.cancel}
    </button>
    <button type="button" class="btn btn-danger" onclick={confirmDelete} disabled={deleting}>
      {deleting ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  {/snippet}
</Modal>

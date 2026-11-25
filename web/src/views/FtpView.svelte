<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatQuota } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { FtpAccount, FtpProtocol, PaginatedResponse, User } from '../lib/types';

  let accounts = $state<FtpAccount[]>([]);
  let users = $state<User[]>([]);
  let loading = $state(true);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let canSelectOwner = $derived(auth.user?.role === 'admin' || auth.user?.role === 'reseller');

  // Create modal
  let formOpen = $state(false);
  let fUsername = $state('');
  let fPassword = $state('');
  let fProtocol = $state<FtpProtocol>('sftp');
  let fHomeDir = $state('');
  let fQuotaMb = $state('');
  let fUserId = $state('');
  let formError = $state('');
  let saving = $state(false);

  // Delete modal
  let deleteTarget = $state<FtpAccount | null>(null);
  let deleting = $state(false);

  function notifyJob(message: string, jobId?: string | null) {
    if (jobId) {
      toast.success(`${message} (${fmt(i18n.t.ftp.jobToast, { jobId })})`, {
        href: `#/jobs/${jobId}`,
        label: i18n.t.ftp.viewJob,
      });
    } else {
      toast.success(message);
    }
  }

  async function loadAccounts() {
    loading = true;
    try {
      const res = await api.get<PaginatedResponse<FtpAccount>>('/ftp');
      accounts = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
    }
  }

  async function loadUsers() {
    if (!canSelectOwner) return;
    try {
      const res = await api.get<PaginatedResponse<User>>('/users?limit=200&offset=0');
      users = res.items;
    } catch {
      // ignore
    }
  }

  onMount(() => {
    loadAccounts();
    loadUsers();
  });

  function openCreate() {
    fUsername = '';
    fPassword = '';
    fProtocol = 'sftp';
    fHomeDir = '';
    fQuotaMb = '';
    fUserId = auth.user?.id ?? '';
    formError = '';
    formOpen = true;
  }

  async function submitForm(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    saving = true;
    try {
      const body: Record<string, unknown> = {
        username: fUsername.trim(),
        password: fPassword,
        protocol: fProtocol,
      };
      if (fHomeDir.trim()) body.home_dir = fHomeDir.trim();
      if (fQuotaMb.trim() !== '') {
        const parsedQuota = Number(fQuotaMb.trim());
        if (Number.isFinite(parsedQuota)) body.quota_mb = parsedQuota;
      }
      if (canSelectOwner && fUserId) body.user_id = fUserId;

      const res = await api.post<{ ftp: FtpAccount; job_id: string }>('/ftp', body);
      notifyJob(i18n.t.ftp.createdSuccess, res.job_id);
      formOpen = false;
      await loadAccounts();
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
      const res = await api.delete<{ job_id: string }>(`/ftp/${deleteTarget.id}`);
      notifyJob(i18n.t.ftp.deletedSuccess, res?.job_id);
      deleteTarget = null;
      await loadAccounts();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      deleteTarget = null;
    } finally {
      deleting = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.ftp.title}</h1>
    <p class="page-subtitle">{i18n.t.ftp.subtitle}</p>
  </div>
  {#if canWrite}
    <button type="button" class="btn btn-primary" onclick={openCreate}>
      + {i18n.t.ftp.createAccount}
    </button>
  {/if}
</div>

<div class="toolbar">
  <div></div>
  <button type="button" class="btn btn-sm" onclick={loadAccounts} disabled={loading}>
    {i18n.t.common.refresh}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.ftp.username}</th>
        <th>{i18n.t.ftp.protocol}</th>
        <th>{i18n.t.ftp.homeDir}</th>
        <th>{i18n.t.ftp.quotaMb}</th>
        {#if canWrite}
          <th>{i18n.t.common.actions}</th>
        {/if}
      </tr>
    </thead>
    <tbody>
      {#if loading && accounts.length === 0}
        <tr><td colspan={canWrite ? 5 : 4} class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if accounts.length === 0}
        <tr><td colspan={canWrite ? 5 : 4} class="empty-state">{i18n.t.common.noData}</td></tr>
      {:else}
        {#each accounts as acc (acc.id)}
          <tr>
            <td class="mono"><strong>{acc.username}</strong></td>
            <td>
              <span class="badge {acc.protocol === 'sftp' ? 'badge-success' : 'badge-primary'}">
                {acc.protocol}
              </span>
            </td>
            <td class="mono">{acc.home_dir}</td>
            <td>{acc.quota_mb === 0 ? i18n.t.common.unlimited : formatQuota(acc.quota_mb, 'MB')}</td>
            {#if canWrite}
              <td>
                <div class="table-actions">
                  <button
                    type="button"
                    class="btn btn-sm btn-icon btn-danger"
                    title={i18n.t.common.delete}
                    aria-label={i18n.t.common.delete}
                    onclick={() => (deleteTarget = acc)}
                  >
                    <ActionIcon name="delete" />
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

<Modal open={formOpen} title={i18n.t.ftp.createAccount} onclose={() => (formOpen = false)}>
  <form id="ftp-form" onsubmit={submitForm}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="ftp-username">{i18n.t.ftp.username}</label>
        <input
          id="ftp-username"
          type="text"
          class="form-control"
          bind:value={fUsername}
          required
          placeholder="budi_ftp"
        />
      </div>
      <div class="form-group">
        <label class="form-label" for="ftp-protocol">{i18n.t.ftp.protocol}</label>
        <select id="ftp-protocol" class="form-control" bind:value={fProtocol}>
          <option value="sftp">SFTP</option>
          <option value="ftps">FTPS</option>
        </select>
      </div>
    </div>

    {#if canSelectOwner && users.length > 0}
      <div class="form-group">
        <label class="form-label" for="ftp-owner">{i18n.t.ftp.owner}</label>
        <select id="ftp-owner" class="form-control" bind:value={fUserId}>
          {#each users as u (u.id)}
            <option value={u.id}>{u.username} ({u.role})</option>
          {/each}
        </select>
      </div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="ftp-password">{i18n.t.ftp.password}</label>
      <input
        id="ftp-password"
        type="password"
        class="form-control"
        bind:value={fPassword}
        required
        autocomplete="new-password"
      />
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="ftp-homedir">{i18n.t.ftp.homeDir}</label>
        <input
          id="ftp-homedir"
          type="text"
          class="form-control"
          bind:value={fHomeDir}
          placeholder={i18n.t.ftp.homeDirPlaceholder}
        />
      </div>
      <div class="form-group">
        <label class="form-label" for="ftp-quota">{i18n.t.ftp.quotaMb}</label>
        <input
          id="ftp-quota"
          type="number"
          class="form-control"
          bind:value={fQuotaMb}
          placeholder="1024"
        />
        <p class="form-hint">{i18n.t.ftp.quotaHint}</p>
      </div>
    </div>
  </form>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (formOpen = false)} disabled={saving}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="ftp-form" class="btn btn-primary" disabled={saving}>
      {saving ? i18n.t.common.loading : i18n.t.common.create}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.ftp.deleteConfirmTitle}
  onclose={() => (deleteTarget = null)}
>
  {#if deleteTarget}
    <p>{fmt(i18n.t.ftp.deleteConfirmBody, { username: deleteTarget.username })}</p>
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

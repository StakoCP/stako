<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import Pagination from '../components/Pagination.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { Database, DatabaseEngine, PaginatedResponse, User } from '../lib/types';

  let databases = $state<Database[]>([]);
  let users = $state<User[]>([]);
  let total = $state(0);
  let limit = 25;
  let offset = $state(0);
  let loading = $state(true);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let canSelectOwner = $derived(auth.user?.role === 'admin' || auth.user?.role === 'reseller');

  // Create modal
  let formOpen = $state(false);
  let fName = $state('');
  let fEngine = $state<DatabaseEngine>('mariadb');
  let fDbUser = $state('');
  let fDbPassword = $state('');
  let fRemoteHosts = $state('');
  let fUserId = $state('');
  let formError = $state('');
  let saving = $state(false);
  let showPassword = $state(false);

  // Delete modal
  let deleteTarget = $state<Database | null>(null);
  let deleting = $state(false);

  function notifyJob(message: string, jobId?: string | null) {
    if (jobId) {
      toast.success(`${message} (${fmt(i18n.t.databases.jobToast, { jobId })})`, {
        href: `#/jobs/${jobId}`,
        label: i18n.t.databases.viewJob,
      });
    } else {
      toast.success(message);
    }
  }

  async function loadDatabases() {
    loading = true;
    try {
      const qs = new URLSearchParams({
        limit: String(limit),
        offset: String(offset),
      });
      const res = await api.get<PaginatedResponse<Database>>(`/databases?${qs.toString()}`);
      databases = res.items;
      total = res.total;
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
    loadDatabases();
    loadUsers();
  });

  function openCreate() {
    fName = '';
    fEngine = 'mariadb';
    fDbUser = '';
    fDbPassword = '';
    fRemoteHosts = '';
    fUserId = auth.user?.id ?? '';
    showPassword = false;
    formError = '';
    formOpen = true;
  }

  function generatePassword() {
    const charset = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*-_';
    const bytes = new Uint32Array(20);
    crypto.getRandomValues(bytes);
    let out = '';
    for (let i = 0; i < bytes.length; i++) {
      out += charset[bytes[i] % charset.length];
    }
    fDbPassword = out;
    showPassword = true;
  }

  async function copyPassword() {
    if (!fDbPassword) return;
    try {
      await navigator.clipboard.writeText(fDbPassword);
      toast.success(i18n.t.common.copied);
    } catch {
      // ignore
    }
  }

  async function submitForm(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    saving = true;
    try {
      const remoteHosts = fRemoteHosts
        .split(',')
        .map((h) => h.trim())
        .filter((h) => h.length > 0);

      const body: Record<string, unknown> = {
        name: fName.trim(),
        engine: fEngine,
        db_password: fDbPassword,
      };
      if (fDbUser.trim()) body.db_user = fDbUser.trim();
      if (remoteHosts.length > 0) body.remote_hosts = remoteHosts;
      if (canSelectOwner && fUserId) body.user_id = fUserId;

      const res = await api.post<{ database: Database; job_id: string }>('/databases', body);
      notifyJob(i18n.t.databases.createdSuccess, res.job_id);
      formOpen = false;
      await loadDatabases();
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

  async function triggerBackup(db: Database) {
    try {
      const res = await api.post<{ job_id: string }>(`/databases/${db.id}/backup`);
      notifyJob(i18n.t.databases.backupStarted, res.job_id);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    try {
      const res = await api.delete<{ job_id: string }>(`/databases/${deleteTarget.id}`);
      notifyJob(i18n.t.databases.deletedSuccess, res?.job_id);
      deleteTarget = null;
      await loadDatabases();
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
    <h1 class="page-title">{i18n.t.databases.title}</h1>
    <p class="page-subtitle">{i18n.t.databases.subtitle}</p>
  </div>
  {#if canWrite}
    <button type="button" class="btn btn-primary" onclick={openCreate}>
      + {i18n.t.databases.createDb}
    </button>
  {/if}
</div>

<div class="toolbar">
  <div></div>
  <button type="button" class="btn btn-sm" onclick={loadDatabases} disabled={loading}>
    {i18n.t.common.refresh}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.databases.name}</th>
        <th>{i18n.t.databases.engine}</th>
        <th>{i18n.t.databases.dbUser}</th>
        <th>{i18n.t.databases.remoteHosts}</th>
        <th>{i18n.t.common.createdAt}</th>
        {#if canWrite}
          <th>{i18n.t.common.actions}</th>
        {/if}
      </tr>
    </thead>
    <tbody>
      {#if loading && databases.length === 0}
        <tr><td colspan={canWrite ? 6 : 5} class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if databases.length === 0}
        <tr><td colspan={canWrite ? 6 : 5} class="empty-state">{i18n.t.common.noData}</td></tr>
      {:else}
        {#each databases as db (db.id)}
          <tr>
            <td class="mono"><strong>{db.name}</strong></td>
            <td>
              <span class="badge {db.engine === 'postgres' ? 'badge-info' : 'badge-primary'}">
                {db.engine}
              </span>
            </td>
            <td class="mono">{db.db_user}</td>
            <td class="mono">
              {db.remote_hosts && db.remote_hosts.length > 0 ? db.remote_hosts.join(', ') : '—'}
            </td>
            <td>{formatDateTime(db.created_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</td>
            {#if canWrite}
              <td>
                <div class="table-actions">
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.databases.backup}
                    aria-label={i18n.t.databases.backup}
                    onclick={() => triggerBackup(db)}
                  >
                    <ActionIcon name="backup" />
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-icon btn-danger"
                    title={i18n.t.common.delete}
                    aria-label={i18n.t.common.delete}
                    onclick={() => (deleteTarget = db)}
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

<Pagination
  {total}
  {limit}
  {offset}
  onchange={(nextOffset) => {
    offset = nextOffset;
    loadDatabases();
  }}
/>

<Modal open={formOpen} title={i18n.t.databases.createDb} onclose={() => (formOpen = false)}>
  <form id="db-form" onsubmit={submitForm}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="db-name">{i18n.t.databases.name}</label>
        <input
          id="db-name"
          type="text"
          class="form-control"
          bind:value={fName}
          required
          placeholder="app_prod"
        />
      </div>
      <div class="form-group">
        <label class="form-label" for="db-engine">{i18n.t.databases.engine}</label>
        <select id="db-engine" class="form-control" bind:value={fEngine}>
          <option value="mariadb">MariaDB</option>
          <option value="postgres">PostgreSQL</option>
        </select>
      </div>
    </div>

    {#if canSelectOwner && users.length > 0}
      <div class="form-group">
        <label class="form-label" for="db-owner">{i18n.t.databases.owner}</label>
        <select id="db-owner" class="form-control" bind:value={fUserId}>
          {#each users as u (u.id)}
            <option value={u.id}>{u.username} ({u.role})</option>
          {/each}
        </select>
      </div>
    {/if}

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="db-user">{i18n.t.databases.dbUser}</label>
        <input
          id="db-user"
          type="text"
          class="form-control"
          bind:value={fDbUser}
          placeholder={i18n.t.databases.dbUserPlaceholder}
        />
      </div>
      <div class="form-group">
        <label class="form-label" for="db-password">{i18n.t.databases.dbPassword}</label>
        <div class="input-group">
          <input
            id="db-password"
            type={showPassword ? 'text' : 'password'}
            class="form-control"
            bind:value={fDbPassword}
            required
            autocomplete="new-password"
          />
          <button
            type="button"
            class="btn btn-sm btn-icon"
            title={i18n.t.databases.generatePassword}
            aria-label={i18n.t.databases.generatePassword}
            onclick={generatePassword}
          >
            <ActionIcon name="key" />
          </button>
          <button
            type="button"
            class="btn btn-sm btn-icon"
            title={i18n.t.databases.togglePassword}
            aria-label={i18n.t.databases.togglePassword}
            onclick={() => (showPassword = !showPassword)}
          >
            <ActionIcon name="eye" />
          </button>
          <button
            type="button"
            class="btn btn-sm btn-icon"
            title={i18n.t.common.copy}
            aria-label={i18n.t.common.copy}
            disabled={!fDbPassword}
            onclick={copyPassword}
          >
            <ActionIcon name="copy" />
          </button>
        </div>
      </div>
    </div>

    <div class="form-group">
      <label class="form-label" for="db-remote-hosts">{i18n.t.databases.remoteHosts}</label>
      <input
        id="db-remote-hosts"
        type="text"
        class="form-control"
        bind:value={fRemoteHosts}
        placeholder="127.0.0.1, 10.0.0.5"
      />
      <p class="form-hint">{i18n.t.databases.remoteHostsHint}</p>
    </div>
  </form>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (formOpen = false)} disabled={saving}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="db-form" class="btn btn-primary" disabled={saving}>
      {saving ? i18n.t.common.loading : i18n.t.common.create}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.databases.deleteConfirmTitle}
  onclose={() => (deleteTarget = null)}
>
  {#if deleteTarget}
    <p>{fmt(i18n.t.databases.deleteConfirmBody, { name: deleteTarget.name })}</p>
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

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
  import type {
    Database,
    DatabaseEngine,
    PGAdvancedResponse,
    PGPITRConfig,
    PGReplicationConfig,
    PgBouncerConfig,
    PaginatedResponse,
    User,
  } from '../lib/types';

  let databases = $state<Database[]>([]);
  let users = $state<User[]>([]);
  let total = $state(0);
  let limit = 25;
  let offset = $state(0);
  let loading = $state(true);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let isAdmin = $derived(auth.user?.role === 'admin');
  let canSelectOwner = $derived(auth.user?.role === 'admin' || auth.user?.role === 'reseller');
  let selectedUsername = $derived(
    users.find((u) => u.id === fUserId)?.username ?? auth.user?.username ?? 'user'
  );
  let dbPrefix = $derived(`${selectedUsername.replace(/-/g, '_')}_`);

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

  // Restore modal
  let restoreTarget = $state<Database | null>(null);
  let restorePath = $state('');
  let restoreError = $state('');
  let restoring = $state(false);

  // Rotate Password modal
  let rotateTarget = $state<Database | null>(null);
  let rotatePassword = $state('');
  let rotateError = $state('');
  let rotating = $state(false);
  let rotatedResult = $state<{ username: string; password: string } | null>(null);

  // Advanced PostgreSQL (admin only)
  let pgAdvTab = $state<'pgbouncer' | 'replication' | 'pitr'>('pgbouncer');
  let pgAdvLoading = $state(false);
  let pgSaving = $state(false);
  let pgBouncer = $state<PgBouncerConfig>({
    enabled: false,
    listen_port: 6432,
    pool_mode: 'transaction',
    max_client_conn: 100,
    default_pool_size: 20,
    reserve_pool_size: 5,
  });
  let pgReplication = $state<PGReplicationConfig>({
    role: 'disabled',
    wal_level: 'replica',
    max_wal_senders: 5,
    wal_keep_size_mb: 256,
    replication_user: 'replicator',
    standby_hosts: [],
    primary_host: '',
    primary_port: 5432,
  });
  let pgRepPass = $state('');
  let standbyHostsInput = $state('');
  let pgPitr = $state<PGPITRConfig>({
    enabled: false,
    archive_dir: '/var/lib/postgresql/wal_archive',
    retention_days: 7,
  });
  let pgStatus = $state<PGAdvancedResponse['status']>({
    pgbouncer_active: false,
    in_recovery: false,
    replication_peers: [],
  });
  let pitrTargetTime = $state('');
  let pitrBusy = $state(false);

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

  async function loadPgAdvanced() {
    if (auth.user?.role !== 'admin') return;
    pgAdvLoading = true;
    try {
      const res = await api.get<PGAdvancedResponse>('/databases/postgres/advanced');
      pgBouncer = res.pgbouncer;
      pgReplication = res.replication;
      standbyHostsInput = (res.replication.standby_hosts ?? []).join(', ');
      pgRepPass = '';
      pgPitr = res.pitr;
      pgStatus = res.status;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      pgAdvLoading = false;
    }
  }

  onMount(() => {
    loadDatabases();
    loadUsers();
    loadPgAdvanced();
  });

  async function savePgBouncer(e: SubmitEvent) {
    e.preventDefault();
    pgSaving = true;
    try {
      const res = await api.put<{ job_id?: string }>('/databases/postgres/pgbouncer', pgBouncer);
      notifyJob(i18n.t.databases.pgAdvanced.pgBouncerSaved, res?.job_id);
      await loadPgAdvanced();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      pgSaving = false;
    }
  }

  async function saveReplication(e: SubmitEvent) {
    e.preventDefault();
    pgSaving = true;
    try {
      const standby_hosts = standbyHostsInput
        .split(',')
        .map((h) => h.trim())
        .filter((h) => h.length > 0);
      const payload: PGReplicationConfig = {
        ...pgReplication,
        standby_hosts,
      };
      if (pgRepPass) {
        payload.replication_pass = pgRepPass;
      }
      const res = await api.put<{ job_id?: string }>('/databases/postgres/replication', payload);
      notifyJob(i18n.t.databases.pgAdvanced.replicationSaved, res?.job_id);
      pgRepPass = '';
      await loadPgAdvanced();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      pgSaving = false;
    }
  }

  async function savePitr(e: SubmitEvent) {
    e.preventDefault();
    pgSaving = true;
    try {
      const res = await api.put<{ job_id?: string }>('/databases/postgres/pitr', pgPitr);
      notifyJob(i18n.t.databases.pgAdvanced.pitrSaved, res?.job_id);
      await loadPgAdvanced();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      pgSaving = false;
    }
  }

  async function takeBaseBackup() {
    pitrBusy = true;
    try {
      const res = await api.post<{ job_id?: string }>('/databases/postgres/pitr/basebackup');
      notifyJob(i18n.t.databases.pgAdvanced.baseBackupStarted, res?.job_id);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      pitrBusy = false;
    }
  }

  async function triggerPitrRestore(e: SubmitEvent) {
    e.preventDefault();
    if (!pitrTargetTime.trim()) return;
    pitrBusy = true;
    try {
      const res = await api.post<{ job_id?: string }>('/databases/postgres/pitr/restore', {
        target_time: pitrTargetTime.trim(),
      });
      notifyJob(i18n.t.databases.pgAdvanced.restoreStarted, res?.job_id);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      pitrBusy = false;
    }
  }

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

  function openRestore(db: Database) {
    restoreTarget = db;
    restorePath = '';
    restoreError = '';
  }

  async function submitRestore(e: SubmitEvent) {
    e.preventDefault();
    if (!restoreTarget) return;
    restoreError = '';
    restoring = true;
    try {
      const res = await api.post<{ job_id?: string }>(`/databases/${restoreTarget.id}/restore`, {
        backup_path: restorePath.trim(),
      });
      notifyJob(i18n.t.databases.restore_started, res?.job_id);
      restoreTarget = null;
    } catch (err) {
      if (err instanceof ApiError) {
        const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
        restoreError = fieldMsg || err.detail || err.title;
      } else {
        restoreError = i18n.t.common.errorGeneric;
      }
    } finally {
      restoring = false;
    }
  }

  function openRotatePassword(db: Database) {
    rotateTarget = db;
    rotatePassword = '';
    rotateError = '';
    rotatedResult = null;
  }

  async function submitRotatePassword(e: SubmitEvent) {
    e.preventDefault();
    if (!rotateTarget) return;
    rotateError = '';
    rotating = true;
    try {
      const body = rotatePassword.trim() ? { password: rotatePassword.trim() } : {};
      const res = await api.post<{ job_id?: string; username: string; password: string }>(
        `/databases/${rotateTarget.id}/rotate-password`,
        body,
      );
      rotatedResult = { username: res.username, password: res.password };
      notifyJob(i18n.t.databases.rotate_pw_success, res.job_id);
    } catch (err) {
      if (err instanceof ApiError) {
        const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
        rotateError = fieldMsg || err.detail || err.title;
      } else {
        rotateError = i18n.t.common.errorGeneric;
      }
    } finally {
      rotating = false;
    }
  }

  async function copyText(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(i18n.t.common.copied);
    } catch {
      toast.error(i18n.t.common.errorGeneric);
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
  <div class="toolbar-filters">
    <a class="btn btn-sm" href="/phpmyadmin/" target="_blank" rel="noopener noreferrer">
      phpMyAdmin
    </a>
    <a class="btn btn-sm" href="/phppgadmin/" target="_blank" rel="noopener noreferrer">
      phpPgAdmin
    </a>
  </div>
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
                    class="btn btn-sm btn-icon"
                    title={i18n.t.databases.restore}
                    aria-label={i18n.t.databases.restore}
                    onclick={() => openRestore(db)}
                  >
                    <ActionIcon name="restore" />
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.databases.rotate_pw}
                    aria-label={i18n.t.databases.rotate_pw}
                    onclick={() => openRotatePassword(db)}
                  >
                    <ActionIcon name="key" />
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

{#if isAdmin}
  <hr class="section-divider" />
  <div class="card">
    <div class="card-header-row">
      <div>
        <h2 class="card-title">{i18n.t.databases.pgAdvanced.title}</h2>
        <p class="form-hint">{i18n.t.databases.pgAdvanced.subtitle}</p>
      </div>
      <div class="table-actions">
        <span class="badge {pgStatus.pgbouncer_active ? 'badge-success' : 'badge-warning'}">
          {pgStatus.pgbouncer_active
            ? i18n.t.databases.pgAdvanced.activeBadge
            : i18n.t.databases.pgAdvanced.inactiveBadge}
        </span>
        {#if pgStatus.in_recovery}
          <span class="badge badge-info">{i18n.t.databases.pgAdvanced.inRecoveryBadge}</span>
        {/if}
      </div>
    </div>

    <div class="tabs" role="tablist" aria-label={i18n.t.databases.pgAdvanced.title}>
      <button
        type="button"
        role="tab"
        aria-selected={pgAdvTab === 'pgbouncer'}
        class="tab-btn"
        class:active={pgAdvTab === 'pgbouncer'}
        onclick={() => (pgAdvTab = 'pgbouncer')}
      >
        {i18n.t.databases.pgAdvanced.tabs.pgbouncer}
      </button>
      <button
        type="button"
        role="tab"
        aria-selected={pgAdvTab === 'replication'}
        class="tab-btn"
        class:active={pgAdvTab === 'replication'}
        onclick={() => (pgAdvTab = 'replication')}
      >
        {i18n.t.databases.pgAdvanced.tabs.replication}
      </button>
      <button
        type="button"
        role="tab"
        aria-selected={pgAdvTab === 'pitr'}
        class="tab-btn"
        class:active={pgAdvTab === 'pitr'}
        onclick={() => (pgAdvTab = 'pitr')}
      >
        {i18n.t.databases.pgAdvanced.tabs.pitr}
      </button>
    </div>

    {#if pgAdvLoading}
      <p class="empty-state">{i18n.t.common.loading}</p>
    {:else if pgAdvTab === 'pgbouncer'}
      <form onsubmit={savePgBouncer}>
        <div class="form-group">
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={pgBouncer.enabled} />
            <span>{i18n.t.databases.pgAdvanced.enabled}</span>
          </label>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="pgb-port">{i18n.t.databases.pgAdvanced.listenPort}</label>
            <input
              id="pgb-port"
              type="number"
              class="form-control"
              bind:value={pgBouncer.listen_port}
              min="1"
              max="65535"
              required
            />
          </div>
          <div class="form-group">
            <label class="form-label" for="pgb-mode">{i18n.t.databases.pgAdvanced.poolMode}</label>
            <select id="pgb-mode" class="form-control" bind:value={pgBouncer.pool_mode}>
              <option value="transaction">transaction</option>
              <option value="session">session</option>
              <option value="statement">statement</option>
            </select>
          </div>
          <div class="form-group">
            <label class="form-label" for="pgb-max-conn">
              {i18n.t.databases.pgAdvanced.maxClientConn}
            </label>
            <input
              id="pgb-max-conn"
              type="number"
              class="form-control"
              bind:value={pgBouncer.max_client_conn}
              min="1"
              required
            />
          </div>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="pgb-default-pool">
              {i18n.t.databases.pgAdvanced.defaultPoolSize}
            </label>
            <input
              id="pgb-default-pool"
              type="number"
              class="form-control"
              bind:value={pgBouncer.default_pool_size}
              min="1"
              required
            />
          </div>
          <div class="form-group">
            <label class="form-label" for="pgb-reserve-pool">
              {i18n.t.databases.pgAdvanced.reservePoolSize}
            </label>
            <input
              id="pgb-reserve-pool"
              type="number"
              class="form-control"
              bind:value={pgBouncer.reserve_pool_size}
              min="0"
              required
            />
          </div>
        </div>

        <button type="submit" class="btn btn-primary" disabled={pgSaving}>
          {pgSaving ? i18n.t.common.loading : i18n.t.databases.pgAdvanced.savePgBouncer}
        </button>
      </form>
    {:else if pgAdvTab === 'replication'}
      <form onsubmit={saveReplication}>
        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="pg-rep-role">{i18n.t.databases.pgAdvanced.role}</label>
            <select id="pg-rep-role" class="form-control" bind:value={pgReplication.role}>
              <option value="disabled">{i18n.t.databases.pgAdvanced.roleDisabled}</option>
              <option value="primary">{i18n.t.databases.pgAdvanced.rolePrimary}</option>
              <option value="standby">{i18n.t.databases.pgAdvanced.roleStandby}</option>
            </select>
          </div>
          <div class="form-group">
            <label class="form-label" for="pg-rep-wal">{i18n.t.databases.pgAdvanced.walLevel}</label>
            <select id="pg-rep-wal" class="form-control" bind:value={pgReplication.wal_level}>
              <option value="replica">replica</option>
              <option value="logical">logical</option>
            </select>
          </div>
          <div class="form-group">
            <label class="form-label" for="pg-rep-senders">
              {i18n.t.databases.pgAdvanced.maxWalSenders}
            </label>
            <input
              id="pg-rep-senders"
              type="number"
              class="form-control"
              bind:value={pgReplication.max_wal_senders}
              min="0"
            />
          </div>
          <div class="form-group">
            <label class="form-label" for="pg-rep-keep">
              {i18n.t.databases.pgAdvanced.walKeepSize}
            </label>
            <input
              id="pg-rep-keep"
              type="number"
              class="form-control"
              bind:value={pgReplication.wal_keep_size_mb}
              min="0"
            />
          </div>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="pg-rep-user">{i18n.t.databases.pgAdvanced.repUser}</label>
            <input
              id="pg-rep-user"
              type="text"
              class="form-control"
              bind:value={pgReplication.replication_user}
            />
          </div>
          <div class="form-group">
            <label class="form-label" for="pg-rep-pass">{i18n.t.databases.pgAdvanced.repPass}</label>
            <input
              id="pg-rep-pass"
              type="password"
              class="form-control"
              bind:value={pgRepPass}
              autocomplete="new-password"
              placeholder={pgReplication.has_replication_password
                ? i18n.t.databases.pgAdvanced.repPassHint
                : ''}
            />
            {#if pgReplication.has_replication_password}
              <p class="form-hint">{i18n.t.databases.pgAdvanced.repPassHint}</p>
            {/if}
          </div>
        </div>

        <div class="form-group">
          <label class="form-label" for="pg-rep-standbys">
            {i18n.t.databases.pgAdvanced.standbyHosts}
          </label>
          <input
            id="pg-rep-standbys"
            type="text"
            class="form-control"
            bind:value={standbyHostsInput}
            placeholder="10.0.0.11, 10.0.0.12"
          />
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="pg-rep-primary-host">
              {i18n.t.databases.pgAdvanced.primaryHost}
            </label>
            <input
              id="pg-rep-primary-host"
              type="text"
              class="form-control"
              bind:value={pgReplication.primary_host}
              placeholder="10.0.0.10"
            />
          </div>
          <div class="form-group">
            <label class="form-label" for="pg-rep-primary-port">
              {i18n.t.databases.pgAdvanced.primaryPort}
            </label>
            <input
              id="pg-rep-primary-port"
              type="number"
              class="form-control"
              bind:value={pgReplication.primary_port}
              min="1"
              max="65535"
            />
          </div>
        </div>

        <button type="submit" class="btn btn-primary" disabled={pgSaving}>
          {pgSaving ? i18n.t.common.loading : i18n.t.databases.pgAdvanced.saveReplication}
        </button>
      </form>
    {:else if pgAdvTab === 'pitr'}
      <form onsubmit={savePitr}>
        <div class="form-group">
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={pgPitr.enabled} />
            <span>{i18n.t.databases.pgAdvanced.walArchivingEnabled}</span>
          </label>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="pg-pitr-dir">{i18n.t.databases.pgAdvanced.archiveDir}</label>
            <input
              id="pg-pitr-dir"
              type="text"
              class="form-control"
              bind:value={pgPitr.archive_dir}
              required
            />
          </div>
          <div class="form-group">
            <label class="form-label" for="pg-pitr-retention">
              {i18n.t.databases.pgAdvanced.retentionDays}
            </label>
            <input
              id="pg-pitr-retention"
              type="number"
              class="form-control"
              bind:value={pgPitr.retention_days}
              min="1"
              required
            />
          </div>
        </div>

        <div class="quick-actions">
          <button type="submit" class="btn btn-primary" disabled={pgSaving}>
            {pgSaving ? i18n.t.common.loading : i18n.t.databases.pgAdvanced.savePitr}
          </button>
          <button type="button" class="btn" onclick={takeBaseBackup} disabled={pitrBusy}>
            {pitrBusy ? i18n.t.common.loading : i18n.t.databases.pgAdvanced.takeBaseBackup}
          </button>
        </div>
      </form>

      <hr class="section-divider" />

      <form onsubmit={triggerPitrRestore}>
        <div class="form-group">
          <label class="form-label" for="pg-pitr-target">
            {i18n.t.databases.pgAdvanced.restoreTargetTime}
          </label>
          <div class="input-group">
            <input
              id="pg-pitr-target"
              type="text"
              class="form-control mono"
              bind:value={pitrTargetTime}
              placeholder="2026-11-27T01:00:00Z"
              required
            />
            <button type="submit" class="btn btn-danger" disabled={pitrBusy}>
              {pitrBusy ? i18n.t.common.loading : i18n.t.databases.pgAdvanced.restoreBtn}
            </button>
          </div>
        </div>
      </form>
    {/if}
  </div>
{/if}

<Modal open={formOpen} title={i18n.t.databases.createDb} onclose={() => (formOpen = false)}>
  <form id="db-form" onsubmit={submitForm}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="db-name">{i18n.t.databases.name}</label>
        <div class="input-group">
          <span class="badge mono">{dbPrefix}</span>
          <input
            id="db-name"
            type="text"
            class="form-control"
            bind:value={fName}
            required
            placeholder="blog"
          />
        </div>
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
        <div class="input-group">
          <span class="badge mono">{dbPrefix}</span>
          <input
            id="db-user"
            type="text"
            class="form-control"
            bind:value={fDbUser}
            placeholder={fName.trim() || i18n.t.databases.dbUserPlaceholder}
          />
        </div>
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

<Modal
  open={restoreTarget !== null}
  title={`${i18n.t.databases.restore}${restoreTarget ? ` — ${restoreTarget.name}` : ''}`}
  onclose={() => (restoreTarget = null)}
>
  <form id="db-restore-form" onsubmit={submitRestore}>
    {#if restoreError}
      <div class="alert alert-danger" role="alert">{restoreError}</div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="db-restore-path">{i18n.t.databases.restore_path}</label>
      <input
        id="db-restore-path"
        type="text"
        class="form-control mono"
        bind:value={restorePath}
        required
        placeholder="/var/backups/stako/mydb.dump"
      />
    </div>
  </form>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (restoreTarget = null)} disabled={restoring}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="db-restore-form" class="btn btn-primary" disabled={restoring}>
      {restoring ? i18n.t.common.loading : i18n.t.databases.restore_run}
    </button>
  {/snippet}
</Modal>

<Modal
  open={rotateTarget !== null}
  title={`${i18n.t.databases.rotate_pw}${rotateTarget ? ` — ${rotateTarget.name}` : ''}`}
  onclose={() => (rotateTarget = null)}
>
  {#if rotatedResult}
    <p><strong>{i18n.t.databases.rotate_pw_result}</strong></p>
    <dl class="kv-list">
      <dt>{i18n.t.databases.dbUser}</dt>
      <dd class="mono">{rotatedResult.username}</dd>
      <dt>{i18n.t.databases.dbPassword}</dt>
      <dd>
        <span class="table-actions">
          <span class="mono">{rotatedResult.password}</span>
          <button
            type="button"
            class="btn btn-sm btn-icon"
            title={i18n.t.common.copy}
            aria-label={i18n.t.common.copy}
            onclick={() => rotatedResult && copyText(rotatedResult.password)}
          >
            <ActionIcon name="copy" />
          </button>
        </span>
      </dd>
    </dl>
  {:else}
    <form id="db-rotate-form" onsubmit={submitRotatePassword}>
      {#if rotateError}
        <div class="alert alert-danger" role="alert">{rotateError}</div>
      {/if}

      {#if rotateTarget}
        <p>{fmt(i18n.t.databases.rotate_pw_confirm, { name: rotateTarget.name })}</p>
      {/if}

      <div class="form-group">
        <label class="form-label" for="db-rotate-pw">{i18n.t.databases.rotate_pw_new}</label>
        <input
          id="db-rotate-pw"
          type="password"
          class="form-control"
          bind:value={rotatePassword}
          autocomplete="new-password"
        />
      </div>
    </form>
  {/if}
  {#snippet footer()}
    {#if rotatedResult}
      <button type="button" class="btn btn-primary" onclick={() => (rotateTarget = null)}>
        {i18n.t.common.close}
      </button>
    {:else}
      <button type="button" class="btn" onclick={() => (rotateTarget = null)} disabled={rotating}>
        {i18n.t.common.cancel}
      </button>
      <button type="submit" form="db-rotate-form" class="btn btn-primary" disabled={rotating}>
        {rotating ? i18n.t.common.loading : i18n.t.databases.rotate_pw}
      </button>
    {/if}
  {/snippet}
</Modal>

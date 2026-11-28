<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type {
    Backup,
    BackupConfig,
    BackupKind,
    BackupStatus,
    BackupTarget,
    PaginatedResponse,
    User,
  } from '../lib/types';

  let backups = $state<Backup[]>([]);
  let users = $state<User[]>([]);
  let loading = $state(true);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let isAdmin = $derived(auth.user?.role === 'admin');
  let canSelectOwner = $derived(auth.user?.role === 'admin' || auth.user?.role === 'reseller');
  let localeCode = $derived(i18n.locale === 'id' ? 'id-ID' : 'en-US');

  // Create modal
  let createOpen = $state(false);
  let fKind = $state<BackupKind>('user_data');
  let fTarget = $state<BackupTarget>('local');
  let fUserId = $state('');
  let formError = $state('');
  let saving = $state(false);

  // Restore modal
  let restoreTarget = $state<Backup | null>(null);
  let restoring = $state(false);

  // Delete modal
  let deleteTarget = $state<Backup | null>(null);
  let deleting = $state(false);

  // Remote config & retention policy (admin only)
  let configLoading = $state(false);
  let configSaving = $state(false);
  let pruning = $state(false);
  let cfgTarget = $state<BackupTarget>('local');
  let cfgEndpoint = $state('');
  let cfgBucket = $state('');
  let cfgRegion = $state('');
  let cfgAccessKey = $state('');
  let cfgSecretKey = $state('');
  let cfgHasSecret = $state(false);
  let cfgSftpHost = $state('');
  let cfgSftpPort = $state(22);
  let cfgSftpUser = $state('');
  let cfgSftpPath = $state('');
  let cfgKeepDaily = $state(7);
  let cfgKeepWeekly = $state(4);
  let cfgKeepMonthly = $state(6);
  let cfgScheduleCron = $state('0 2 * * *');

  let isObjectStorage = $derived(cfgTarget === 's3' || cfgTarget === 'r2' || cfgTarget === 'b2');

  function notifyJob(message: string, jobId?: string | null) {
    if (jobId) {
      toast.success(`${message} (${fmt(i18n.t.backups.jobToast, { jobId })})`, {
        href: `#/jobs/${jobId}`,
        label: i18n.t.backups.viewJob,
      });
    } else {
      toast.success(message);
    }
  }

  function kindLabel(kind: BackupKind): string {
    return kind === 'system_config'
      ? i18n.t.backups.kindSystemConfig
      : i18n.t.backups.kindUserData;
  }

  function statusBadge(status: BackupStatus): string {
    switch (status) {
      case 'completed':
        return 'badge-success';
      case 'running':
        return 'badge-info';
      case 'failed':
        return 'badge-danger';
    }
  }

  function statusLabel(status: BackupStatus): string {
    switch (status) {
      case 'completed':
        return i18n.t.backups.statusCompleted;
      case 'running':
        return i18n.t.backups.statusRunning;
      case 'failed':
        return i18n.t.backups.statusFailed;
    }
  }

  async function loadBackups() {
    loading = true;
    try {
      const res = await api.get<{ items: Backup[] }>('/backups');
      backups = res.items;
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

  async function loadBackupConfig() {
    if (!isAdmin) return;
    configLoading = true;
    try {
      const cfg = await api.get<BackupConfig>('/backups/config');
      cfgTarget = cfg.target || 'local';
      cfgEndpoint = cfg.endpoint || '';
      cfgBucket = cfg.bucket || '';
      cfgRegion = cfg.region || '';
      cfgAccessKey = cfg.access_key || '';
      cfgSecretKey = '';
      cfgHasSecret = cfg.has_secret;
      cfgSftpHost = cfg.sftp_host || '';
      cfgSftpPort = cfg.sftp_port || 22;
      cfgSftpUser = cfg.sftp_user || '';
      cfgSftpPath = cfg.sftp_path || '';
      cfgKeepDaily = cfg.keep_daily || 7;
      cfgKeepWeekly = cfg.keep_weekly || 4;
      cfgKeepMonthly = cfg.keep_monthly || 6;
      cfgScheduleCron = cfg.schedule_cron || '0 2 * * *';
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      configLoading = false;
    }
  }

  async function saveBackupConfig(e: SubmitEvent) {
    e.preventDefault();
    configSaving = true;
    try {
      await api.put('/backups/config', {
        target: cfgTarget,
        endpoint: cfgEndpoint,
        bucket: cfgBucket,
        region: cfgRegion,
        access_key: cfgAccessKey,
        secret_key: cfgSecretKey,
        sftp_host: cfgSftpHost,
        sftp_port: Number(cfgSftpPort) || 22,
        sftp_user: cfgSftpUser,
        sftp_path: cfgSftpPath,
        keep_daily: Number(cfgKeepDaily) || 7,
        keep_weekly: Number(cfgKeepWeekly) || 4,
        keep_monthly: Number(cfgKeepMonthly) || 6,
        schedule_cron: cfgScheduleCron,
      });
      if (cfgSecretKey.trim() !== '') {
        cfgHasSecret = true;
        cfgSecretKey = '';
      }
      toast.success(i18n.t.backups.config_saved);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      configSaving = false;
    }
  }

  async function pruneBackups() {
    pruning = true;
    try {
      const res = await api.post<{ job_id: string | null }>('/backups/prune');
      notifyJob(i18n.t.backups.prune_started, res.job_id);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      pruning = false;
    }
  }

  onMount(() => {
    loadBackups();
    loadUsers();
    loadBackupConfig();
  });

  function openCreate() {
    fKind = 'user_data';
    fTarget = 'local';
    fUserId = auth.user?.id ?? '';
    formError = '';
    createOpen = true;
  }

  async function submitCreate(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    saving = true;
    try {
      const body: Record<string, unknown> = {
        kind: isAdmin ? fKind : 'user_data',
        target: fTarget,
      };
      if (canSelectOwner && body.kind === 'user_data' && fUserId) {
        body.user_id = fUserId;
      }

      const res = await api.post<{ backup: Backup; job_id: string }>('/backups', body);
      notifyJob(i18n.t.backups.createdSuccess, res.job_id);
      createOpen = false;
      await loadBackups();
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

  async function confirmRestore() {
    if (!restoreTarget) return;
    restoring = true;
    try {
      const res = await api.post<{ job_id: string }>(`/backups/${restoreTarget.id}/restore`);
      notifyJob(i18n.t.backups.restoreStarted, res.job_id);
      restoreTarget = null;
      await loadBackups();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      restoring = false;
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    try {
      await api.delete(`/backups/${deleteTarget.id}`);
      toast.success(i18n.t.backups.deletedSuccess);
      deleteTarget = null;
      await loadBackups();
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
    <h1 class="page-title">{i18n.t.backups.title}</h1>
    <p class="page-subtitle">{i18n.t.backups.subtitle}</p>
  </div>
  {#if canWrite}
    <button type="button" class="btn btn-primary" onclick={openCreate}>
      + {i18n.t.backups.createBackup}
    </button>
  {/if}
</div>

<div class="toolbar">
  <div></div>
  <button type="button" class="btn btn-sm" onclick={loadBackups} disabled={loading}>
    {i18n.t.common.refresh}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.backups.kind}</th>
        <th>{i18n.t.backups.target}</th>
        <th>{i18n.t.backups.checksum}</th>
        <th>{i18n.t.common.status}</th>
        <th>{i18n.t.common.createdAt}</th>
        {#if canWrite}
          <th>{i18n.t.common.actions}</th>
        {/if}
      </tr>
    </thead>
    <tbody>
      {#if loading && backups.length === 0}
        <tr><td colspan={canWrite ? 6 : 5} class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if backups.length === 0}
        <tr><td colspan={canWrite ? 6 : 5} class="empty-state">{i18n.t.common.noData}</td></tr>
      {:else}
        {#each backups as b (b.id)}
          <tr>
            <td>
              <span class="badge {b.kind === 'system_config' ? 'badge-warning' : 'badge-primary'}">
                {kindLabel(b.kind)}
              </span>
            </td>
            <td class="mono">{b.target}</td>
            <td class="mono">{b.checksum || '—'}</td>
            <td>
              <span class="badge {statusBadge(b.status)}">{statusLabel(b.status)}</span>
            </td>
            <td>{formatDateTime(b.created_at, localeCode)}</td>
            {#if canWrite}
              <td>
                <div class="table-actions">
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.backups.restore}
                    aria-label={i18n.t.backups.restore}
                    onclick={() => (restoreTarget = b)}
                    disabled={b.status !== 'completed'}
                  >
                    <ActionIcon name="restore" />
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-icon btn-danger"
                    title={i18n.t.common.delete}
                    aria-label={i18n.t.common.delete}
                    onclick={() => (deleteTarget = b)}
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

{#if isAdmin}
  <hr class="section-divider" />
  <div class="card">
    <div class="card-header-row">
      <h2 class="card-title">{i18n.t.backups.remote_config}</h2>
      <button
        type="button"
        class="btn btn-sm"
        onclick={pruneBackups}
        disabled={pruning || configLoading}
      >
        {pruning ? i18n.t.common.loading : i18n.t.backups.prune_now}
      </button>
    </div>

    {#if configLoading}
      <p>{i18n.t.common.loading}</p>
    {:else}
      <form onsubmit={saveBackupConfig}>
        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="bk-cfg-target">{i18n.t.backups.target}</label>
            <select id="bk-cfg-target" class="form-control" bind:value={cfgTarget}>
              <option value="local">{i18n.t.backups.targetLocal}</option>
              <option value="s3">{i18n.t.backups.targetS3}</option>
              <option value="r2">{i18n.t.backups.target_r2}</option>
              <option value="b2">{i18n.t.backups.target_b2}</option>
              <option value="sftp">{i18n.t.backups.targetSftp}</option>
            </select>
          </div>
          <div class="form-group">
            <label class="form-label" for="bk-cfg-cron">{i18n.t.backups.schedule_cron}</label>
            <input
              id="bk-cfg-cron"
              type="text"
              class="form-control mono"
              bind:value={cfgScheduleCron}
              placeholder="0 2 * * *"
            />
          </div>
        </div>

        {#if isObjectStorage}
          <div class="form-row">
            <div class="form-group">
              <label class="form-label" for="bk-cfg-endpoint">{i18n.t.backups.endpoint}</label>
              <input
                id="bk-cfg-endpoint"
                type="text"
                class="form-control"
                bind:value={cfgEndpoint}
                placeholder="https://s3.example.com"
              />
            </div>
            <div class="form-group">
              <label class="form-label" for="bk-cfg-bucket">{i18n.t.backups.bucket}</label>
              <input
                id="bk-cfg-bucket"
                type="text"
                class="form-control"
                bind:value={cfgBucket}
              />
            </div>
            <div class="form-group">
              <label class="form-label" for="bk-cfg-region">{i18n.t.backups.region}</label>
              <input
                id="bk-cfg-region"
                type="text"
                class="form-control"
                bind:value={cfgRegion}
                placeholder="us-east-1"
              />
            </div>
          </div>

          <div class="form-row">
            <div class="form-group">
              <label class="form-label" for="bk-cfg-access-key">{i18n.t.backups.access_key}</label>
              <input
                id="bk-cfg-access-key"
                type="text"
                class="form-control mono"
                bind:value={cfgAccessKey}
              />
            </div>
            <div class="form-group">
              <label class="form-label" for="bk-cfg-secret-key">{i18n.t.backups.secret_key}</label>
              <input
                id="bk-cfg-secret-key"
                type="password"
                class="form-control mono"
                bind:value={cfgSecretKey}
                placeholder={cfgHasSecret ? '••••••••' : ''}
              />
            </div>
          </div>
        {:else if cfgTarget === 'sftp'}
          <div class="form-row">
            <div class="form-group">
              <label class="form-label" for="bk-cfg-sftp-host">{i18n.t.backups.sftp_host}</label>
              <input
                id="bk-cfg-sftp-host"
                type="text"
                class="form-control"
                bind:value={cfgSftpHost}
              />
            </div>
            <div class="form-group">
              <label class="form-label" for="bk-cfg-sftp-port">{i18n.t.backups.sftp_port}</label>
              <input
                id="bk-cfg-sftp-port"
                type="number"
                min="1"
                max="65535"
                class="form-control"
                bind:value={cfgSftpPort}
              />
            </div>
          </div>

          <div class="form-row">
            <div class="form-group">
              <label class="form-label" for="bk-cfg-sftp-user">{i18n.t.backups.sftp_user}</label>
              <input
                id="bk-cfg-sftp-user"
                type="text"
                class="form-control"
                bind:value={cfgSftpUser}
              />
            </div>
            <div class="form-group">
              <label class="form-label" for="bk-cfg-sftp-path">{i18n.t.backups.sftp_path}</label>
              <input
                id="bk-cfg-sftp-path"
                type="text"
                class="form-control mono"
                bind:value={cfgSftpPath}
                placeholder="/backups/stako"
              />
            </div>
          </div>
        {/if}

        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="bk-cfg-keep-daily">{i18n.t.backups.keep_daily}</label>
            <input
              id="bk-cfg-keep-daily"
              type="number"
              min="1"
              class="form-control"
              bind:value={cfgKeepDaily}
            />
          </div>
          <div class="form-group">
            <label class="form-label" for="bk-cfg-keep-weekly">{i18n.t.backups.keep_weekly}</label>
            <input
              id="bk-cfg-keep-weekly"
              type="number"
              min="1"
              class="form-control"
              bind:value={cfgKeepWeekly}
            />
          </div>
          <div class="form-group">
            <label class="form-label" for="bk-cfg-keep-monthly">{i18n.t.backups.keep_monthly}</label>
            <input
              id="bk-cfg-keep-monthly"
              type="number"
              min="1"
              class="form-control"
              bind:value={cfgKeepMonthly}
            />
          </div>
        </div>

        <button type="submit" class="btn btn-primary" disabled={configSaving}>
          {configSaving ? i18n.t.common.saving : i18n.t.backups.save_config}
        </button>
      </form>
    {/if}
  </div>
{/if}

<Modal open={createOpen} title={i18n.t.backups.createBackup} onclose={() => (createOpen = false)}>
  <form id="backup-form" onsubmit={submitCreate}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="bk-kind">{i18n.t.backups.kind}</label>
        <select id="bk-kind" class="form-control" bind:value={fKind}>
          <option value="user_data">{i18n.t.backups.kindUserData}</option>
          {#if isAdmin}
            <option value="system_config">{i18n.t.backups.kindSystemConfig}</option>
          {/if}
        </select>
      </div>
      <div class="form-group">
        <label class="form-label" for="bk-target">{i18n.t.backups.target}</label>
        <select id="bk-target" class="form-control" bind:value={fTarget}>
          <option value="local">{i18n.t.backups.targetLocal}</option>
          <option value="s3">{i18n.t.backups.targetS3}</option>
          <option value="r2">{i18n.t.backups.target_r2}</option>
          <option value="b2">{i18n.t.backups.target_b2}</option>
          <option value="sftp">{i18n.t.backups.targetSftp}</option>
        </select>
      </div>
    </div>

    {#if canSelectOwner && fKind === 'user_data' && users.length > 0}
      <div class="form-group">
        <label class="form-label" for="bk-owner">{i18n.t.backups.owner}</label>
        <select id="bk-owner" class="form-control" bind:value={fUserId}>
          {#each users as u (u.id)}
            <option value={u.id}>{u.username} ({u.role})</option>
          {/each}
        </select>
      </div>
    {/if}
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (createOpen = false)}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="backup-form" class="btn btn-primary" disabled={saving}>
      {saving ? i18n.t.common.loading : i18n.t.common.create}
    </button>
  {/snippet}
</Modal>

<Modal
  open={restoreTarget !== null}
  title={i18n.t.backups.restoreTitle}
  onclose={() => (restoreTarget = null)}
>
  <p>
    {fmt(i18n.t.backups.restoreBody, {
      id: restoreTarget?.id ?? '',
      kind: restoreTarget ? kindLabel(restoreTarget.kind) : '',
    })}
  </p>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (restoreTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-primary"
      onclick={confirmRestore}
      disabled={restoring}
    >
      {restoring ? i18n.t.common.loading : i18n.t.backups.restore}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.backups.deleteTitle}
  onclose={() => (deleteTarget = null)}
>
  <p>{fmt(i18n.t.backups.deleteBody, { id: deleteTarget?.id ?? '' })}</p>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (deleteTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-danger"
      onclick={confirmDelete}
      disabled={deleting}
    >
      {deleting ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  {/snippet}
</Modal>

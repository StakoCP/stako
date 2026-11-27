<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError } from '../lib/api';
  import { formatBytes, formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type {
    ImportSourcePanel,
    MigrationBundle,
    MigrationInspectResult,
    PaginatedResponse,
    User,
  } from '../lib/types';

  type MigrationTab = 'import' | 'export' | 'transfer';

  let activeTab = $state<MigrationTab>('import');
  let localeCode = $derived(i18n.locale === 'id' ? 'id-ID' : 'en-US');

  // Import tab state
  let sourcePanel = $state<ImportSourcePanel>('auto');
  let archivePath = $state('');
  let targetUsername = $state('');
  let targetEmail = $state('');
  let includeFiles = $state(true);
  let includeDatabases = $state(true);
  let includeMail = $state(true);
  let inspecting = $state(false);
  let importing = $state(false);
  let inspectResult = $state<MigrationInspectResult | null>(null);
  let importError = $state('');

  // Export tab state
  let users = $state<User[]>([]);
  let loadingUsers = $state(false);
  let selectedUserId = $state('');
  let exporting = $state(false);
  let exportError = $state('');
  let bundles = $state<MigrationBundle[]>([]);
  let loadingBundles = $state(false);

  // Transfer tab state
  let direction = $state<'pull' | 'push'>('pull');
  let remoteHost = $state('');
  let remotePort = $state(22);
  let remoteUser = $state('root');
  let remotePath = $state('');
  let localPath = $state('');
  let transferring = $state(false);
  let transferError = $state('');

  function extractError(err: unknown): string {
    if (err instanceof ApiError) {
      const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
      return fieldMsg || err.detail || err.title;
    }
    return i18n.t.common.errorGeneric;
  }

  function notifyJob(message: string, jobId?: string | null) {
    if (jobId) {
      toast.success(message, {
        href: `#/jobs/${jobId}`,
        label: i18n.t.migrations.viewJob,
      });
    } else {
      toast.success(message);
    }
  }

  async function loadUsers() {
    loadingUsers = true;
    try {
      const res = await api.get<PaginatedResponse<User>>('/users?limit=200&offset=0');
      users = res.items ?? [];
      if (!selectedUserId && users.length > 0) {
        selectedUserId = users[0].id;
      }
    } catch (err) {
      toast.error(extractError(err));
    } finally {
      loadingUsers = false;
    }
  }

  async function loadBundles() {
    loadingBundles = true;
    try {
      const res = await api.get<{ bundles: MigrationBundle[] | null }>('/migrations/bundles');
      bundles = res.bundles ?? [];
    } catch (err) {
      toast.error(extractError(err));
    } finally {
      loadingBundles = false;
    }
  }

  onMount(() => {
    loadUsers();
    loadBundles();
  });

  async function handleInspect() {
    importError = '';
    if (!archivePath.trim()) return;
    inspecting = true;
    try {
      const res = await api.post<MigrationInspectResult>('/migrations/inspect', {
        source_panel: sourcePanel,
        archive_path: archivePath.trim(),
      });
      inspectResult = res;
      if (!targetUsername.trim() && res.username) {
        targetUsername = res.username;
      }
    } catch (err) {
      importError = extractError(err);
      toast.error(importError);
    } finally {
      inspecting = false;
    }
  }

  async function handleImport(e: SubmitEvent) {
    e.preventDefault();
    importError = '';
    const trimmedArchive = archivePath.trim();
    if (!trimmedArchive) return;

    importing = true;
    try {
      let resolvedPanel = sourcePanel;
      let inspected = inspectResult;

      if (resolvedPanel === 'auto') {
        if (!inspected || inspected.archive_path !== trimmedArchive) {
          inspected = await api.post<MigrationInspectResult>('/migrations/inspect', {
            source_panel: 'auto',
            archive_path: trimmedArchive,
          });
          inspectResult = inspected;
          if (!targetUsername.trim() && inspected.username) {
            targetUsername = inspected.username;
          }
        }
        const detected = inspected.source_panel as ImportSourcePanel;
        if (
          detected === 'stako' ||
          detected === 'hestiacp' ||
          detected === 'cpanel' ||
          detected === 'vestacp'
        ) {
          resolvedPanel = detected;
        } else {
          importError = i18n.t.migrations.inspectNeedPanel;
          toast.error(importError);
          return;
        }
      }

      const body: Record<string, unknown> = {
        source_panel: resolvedPanel,
        archive_path: trimmedArchive,
        include_files: includeFiles,
        include_databases: includeDatabases,
        include_mail: includeMail,
      };
      if (targetUsername.trim()) body.target_username = targetUsername.trim();
      if (targetEmail.trim()) body.target_email = targetEmail.trim();
      if (inspected) {
        body.domains = inspected.domains ?? [];
        body.databases = inspected.databases ?? [];
        body.mail_domains = inspected.mail_domains ?? [];
      }

      const res = await api.post<{ job_id: string | null }>('/migrations/import', body);
      notifyJob(
        fmt(i18n.t.migrations.importStartedToast, { jobId: res.job_id ?? '' }),
        res.job_id
      );
    } catch (err) {
      importError = extractError(err);
      toast.error(importError);
    } finally {
      importing = false;
    }
  }

  async function handleExport(e: SubmitEvent) {
    e.preventDefault();
    exportError = '';
    if (!selectedUserId) return;
    exporting = true;
    try {
      const res = await api.post<{ job_id: string | null; username: string }>('/migrations/export', {
        user_id: selectedUserId,
      });
      notifyJob(
        fmt(i18n.t.migrations.exportStartedToast, {
          username: res.username,
          jobId: res.job_id ?? '',
        }),
        res.job_id
      );
      await loadBundles();
    } catch (err) {
      exportError = extractError(err);
      toast.error(exportError);
    } finally {
      exporting = false;
    }
  }

  async function copyBundlePath(path: string) {
    try {
      await navigator.clipboard.writeText(path);
      toast.success(i18n.t.common.copied);
    } catch {
      toast.error(i18n.t.common.errorGeneric);
    }
  }

  function useBundleInTransfer(bundle: MigrationBundle) {
    direction = 'push';
    localPath = bundle.path;
    activeTab = 'transfer';
  }

  async function handleTransfer(e: SubmitEvent) {
    e.preventDefault();
    transferError = '';
    transferring = true;
    try {
      const res = await api.post<{ job_id: string | null }>('/migrations/transfer', {
        direction,
        remote_host: remoteHost.trim(),
        remote_port: Number(remotePort) || 22,
        remote_user: remoteUser.trim(),
        remote_path: remotePath.trim(),
        local_path: localPath.trim(),
      });
      notifyJob(
        fmt(i18n.t.migrations.transferStartedToast, { jobId: res.job_id ?? '' }),
        res.job_id
      );
    } catch (err) {
      transferError = extractError(err);
      toast.error(transferError);
    } finally {
      transferring = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.migrations.title}</h1>
    <p class="page-subtitle">{i18n.t.migrations.subtitle}</p>
  </div>
</div>

<div class="tabs" role="tablist" aria-label={i18n.t.migrations.title}>
  <button
    type="button"
    role="tab"
    class="tab-btn"
    class:active={activeTab === 'import'}
    aria-selected={activeTab === 'import'}
    onclick={() => (activeTab = 'import')}
  >
    {i18n.t.migrations.tabImport}
  </button>
  <button
    type="button"
    role="tab"
    class="tab-btn"
    class:active={activeTab === 'export'}
    aria-selected={activeTab === 'export'}
    onclick={() => (activeTab = 'export')}
  >
    {i18n.t.migrations.tabExport}
  </button>
  <button
    type="button"
    role="tab"
    class="tab-btn"
    class:active={activeTab === 'transfer'}
    aria-selected={activeTab === 'transfer'}
    onclick={() => (activeTab = 'transfer')}
  >
    {i18n.t.migrations.tabTransfer}
  </button>
</div>

{#if activeTab === 'import'}
  <div class="card">
    <div class="toolbar">
      <h2 class="card-title">{i18n.t.migrations.importCardTitle}</h2>
    </div>

    {#if importError}
      <div class="alert alert-danger">{importError}</div>
    {/if}

    <form onsubmit={handleImport}>
      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="mig-source-panel">{i18n.t.migrations.sourcePanel}</label>
          <select id="mig-source-panel" class="form-control" bind:value={sourcePanel}>
            <option value="auto">{i18n.t.migrations.panelAuto}</option>
            <option value="stako">{i18n.t.migrations.panelStako}</option>
            <option value="hestiacp">{i18n.t.migrations.panelHestia}</option>
            <option value="cpanel">{i18n.t.migrations.panelCpanel}</option>
            <option value="vestacp">{i18n.t.migrations.panelVesta}</option>
          </select>
        </div>

        <div class="form-group">
          <label class="form-label" for="mig-archive-path">{i18n.t.migrations.archivePath}</label>
          <div class="table-actions">
            <input
              id="mig-archive-path"
              type="text"
              class="form-control mono"
              placeholder={i18n.t.migrations.archivePathPlaceholder}
              bind:value={archivePath}
              required
            />
            <button
              type="button"
              class="btn"
              onclick={handleInspect}
              disabled={inspecting || !archivePath.trim()}
            >
              {inspecting ? i18n.t.migrations.inspecting : i18n.t.migrations.inspectBtn}
            </button>
          </div>
        </div>
      </div>

      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="mig-target-user">{i18n.t.migrations.targetUsername}</label>
          <input
            id="mig-target-user"
            type="text"
            class="form-control mono"
            placeholder={i18n.t.migrations.targetUsernamePlaceholder}
            bind:value={targetUsername}
          />
        </div>

        <div class="form-group">
          <label class="form-label" for="mig-target-email">{i18n.t.migrations.targetEmail}</label>
          <input
            id="mig-target-email"
            type="email"
            class="form-control"
            placeholder={i18n.t.migrations.targetEmailPlaceholder}
            bind:value={targetEmail}
          />
        </div>
      </div>

      <div class="form-group">
        <div class="checkbox-group">
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={includeFiles} />
            <span>{i18n.t.migrations.includeFiles}</span>
          </label>
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={includeDatabases} />
            <span>{i18n.t.migrations.includeDatabases}</span>
          </label>
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={includeMail} />
            <span>{i18n.t.migrations.includeMail}</span>
          </label>
        </div>
      </div>

      <button type="submit" class="btn btn-primary" disabled={importing || !archivePath.trim()}>
        {importing ? i18n.t.migrations.importing : i18n.t.migrations.startImport}
      </button>
    </form>
  </div>

  {#if inspectResult}
    <div class="card">
      <div class="toolbar">
        <h2 class="card-title">{i18n.t.migrations.inspectCardTitle}</h2>
        <span class="badge badge-info">{inspectResult.source_panel}</span>
      </div>

      <div class="table-wrap">
        <table>
          <tbody>
            <tr>
              <th>{i18n.t.migrations.detectedPanel}</th>
              <td><span class="badge badge-primary">{inspectResult.source_panel}</span></td>
            </tr>
            <tr>
              <th>{i18n.t.migrations.username}</th>
              <td class="mono">{inspectResult.username || '—'}</td>
            </tr>
            <tr>
              <th>{i18n.t.migrations.size}</th>
              <td>{formatBytes(inspectResult.size_bytes)}</td>
            </tr>
            <tr>
              <th>{i18n.t.migrations.domains} ({inspectResult.domains?.length ?? 0})</th>
              <td class="mono">
                {inspectResult.domains?.length ? inspectResult.domains.join(', ') : i18n.t.common.none}
              </td>
            </tr>
            <tr>
              <th>{i18n.t.migrations.databases} ({inspectResult.databases?.length ?? 0})</th>
              <td class="mono">
                {inspectResult.databases?.length
                  ? inspectResult.databases.join(', ')
                  : i18n.t.common.none}
              </td>
            </tr>
            <tr>
              <th>{i18n.t.migrations.mailDomains} ({inspectResult.mail_domains?.length ?? 0})</th>
              <td class="mono">
                {inspectResult.mail_domains?.length
                  ? inspectResult.mail_domains.join(', ')
                  : i18n.t.common.none}
              </td>
            </tr>
            <tr>
              <th>{i18n.t.migrations.mailboxes} ({inspectResult.mailboxes?.length ?? 0})</th>
              <td class="mono">
                {inspectResult.mailboxes?.length
                  ? inspectResult.mailboxes.join(', ')
                  : i18n.t.common.none}
              </td>
            </tr>
            {#if inspectResult.cron_jobs?.length}
              <tr>
                <th>{i18n.t.migrations.cronJobs} ({inspectResult.cron_jobs.length})</th>
                <td class="mono">{inspectResult.cron_jobs.join(', ')}</td>
              </tr>
            {/if}
            {#if inspectResult.notes?.length}
              <tr>
                <th>{i18n.t.migrations.notes}</th>
                <td>{inspectResult.notes.join(' | ')}</td>
              </tr>
            {/if}
          </tbody>
        </table>
      </div>
    </div>
  {/if}
{:else if activeTab === 'export'}
  <div class="card">
    <div class="toolbar">
      <h2 class="card-title">{i18n.t.migrations.exportCardTitle}</h2>
    </div>

    {#if exportError}
      <div class="alert alert-danger">{exportError}</div>
    {/if}

    <form onsubmit={handleExport}>
      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="mig-export-user">{i18n.t.migrations.selectUser}</label>
          <select
            id="mig-export-user"
            class="form-control"
            bind:value={selectedUserId}
            disabled={loadingUsers}
            required
          >
            <option value="">{i18n.t.migrations.selectUserPlaceholder}</option>
            {#each users as u (u.id)}
              <option value={u.id}>{u.username} ({u.email})</option>
            {/each}
          </select>
        </div>
      </div>

      <button type="submit" class="btn btn-primary" disabled={exporting || !selectedUserId}>
        {exporting ? i18n.t.migrations.exporting : i18n.t.migrations.exportBtn}
      </button>
    </form>
  </div>

  <div class="card">
    <div class="toolbar">
      <h2 class="card-title">{i18n.t.migrations.bundlesCardTitle}</h2>
      <button type="button" class="btn btn-sm" onclick={loadBundles} disabled={loadingBundles}>
        {i18n.t.common.refresh}
      </button>
    </div>

    {#if loadingBundles}
      <p>{i18n.t.common.loading}</p>
    {:else if bundles.length === 0}
      <p>{i18n.t.migrations.noBundles}</p>
    {:else}
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>{i18n.t.migrations.bundleName}</th>
              <th>{i18n.t.migrations.bundlePath}</th>
              <th>{i18n.t.migrations.bundleSize}</th>
              <th>{i18n.t.migrations.bundleCreatedAt}</th>
              <th>{i18n.t.common.actions}</th>
            </tr>
          </thead>
          <tbody>
            {#each bundles as bundle (bundle.path)}
              <tr>
                <td class="mono">{bundle.name}</td>
                <td class="mono">{bundle.path}</td>
                <td>{formatBytes(bundle.size_bytes)}</td>
                <td>{formatDateTime(bundle.created_at, localeCode)}</td>
                <td>
                  <div class="table-actions">
                    <button
                      type="button"
                      class="btn btn-sm"
                      onclick={() => copyBundlePath(bundle.path)}
                    >
                      {i18n.t.migrations.copyPath}
                    </button>
                    <button
                      type="button"
                      class="btn btn-sm btn-primary"
                      onclick={() => useBundleInTransfer(bundle)}
                    >
                      {i18n.t.migrations.useInTransfer}
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </div>
{:else}
  <div class="card">
    <div class="toolbar">
      <h2 class="card-title">{i18n.t.migrations.transferCardTitle}</h2>
    </div>

    {#if transferError}
      <div class="alert alert-danger">{transferError}</div>
    {/if}

    <form onsubmit={handleTransfer}>
      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="mig-transfer-dir">{i18n.t.migrations.direction}</label>
          <select id="mig-transfer-dir" class="form-control" bind:value={direction}>
            <option value="pull">{i18n.t.migrations.directionPull}</option>
            <option value="push">{i18n.t.migrations.directionPush}</option>
          </select>
        </div>

        <div class="form-group">
          <label class="form-label" for="mig-remote-host">{i18n.t.migrations.remoteHost}</label>
          <input
            id="mig-remote-host"
            type="text"
            class="form-control mono"
            placeholder={i18n.t.migrations.remoteHostPlaceholder}
            bind:value={remoteHost}
            required
          />
        </div>
      </div>

      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="mig-remote-port">{i18n.t.migrations.remotePort}</label>
          <input
            id="mig-remote-port"
            type="number"
            min="1"
            max="65535"
            class="form-control mono"
            bind:value={remotePort}
            required
          />
        </div>

        <div class="form-group">
          <label class="form-label" for="mig-remote-user">{i18n.t.migrations.remoteUser}</label>
          <input
            id="mig-remote-user"
            type="text"
            class="form-control mono"
            placeholder={i18n.t.migrations.remoteUserPlaceholder}
            bind:value={remoteUser}
            required
          />
        </div>
      </div>

      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="mig-remote-path">{i18n.t.migrations.remotePath}</label>
          <input
            id="mig-remote-path"
            type="text"
            class="form-control mono"
            placeholder={i18n.t.migrations.remotePathPlaceholder}
            bind:value={remotePath}
            required
          />
        </div>

        <div class="form-group">
          <label class="form-label" for="mig-local-path">{i18n.t.migrations.localPath}</label>
          <input
            id="mig-local-path"
            type="text"
            class="form-control mono"
            placeholder={i18n.t.migrations.localPathPlaceholder}
            bind:value={localPath}
          />
        </div>
      </div>

      <button type="submit" class="btn btn-primary" disabled={transferring}>
        {transferring ? i18n.t.migrations.transferring : i18n.t.migrations.startTransfer}
      </button>
    </form>
  </div>
{/if}

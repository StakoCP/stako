<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '../components/Modal.svelte';
  import Pagination from '../components/Pagination.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { App, AppRuntime, Domain, PaginatedResponse, User } from '../lib/types';

  interface EnvEntry {
    key: string;
    value: string;
  }

  let apps = $state<App[]>([]);
  let domains = $state<Domain[]>([]);
  let users = $state<User[]>([]);
  let total = $state(0);
  let limit = 25;
  let offset = $state(0);
  let loading = $state(true);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let canSelectOwner = $derived(auth.user?.role === 'admin' || auth.user?.role === 'reseller');
  let domainMap = $derived(new Map(domains.map((d) => [d.id, d.domain])));

  // Create / Edit modal
  let formOpen = $state(false);
  let editingApp = $state<App | null>(null);
  let fName = $state('');
  let fUserId = $state('');
  let fRuntime = $state<AppRuntime>('node');
  let fRuntimeVersion = $state('22');
  let fWorkDir = $state('');
  let fCommand = $state('');
  let fPort = $state('');
  let fDomainId = $state('');
  let fEnvEntries = $state<EnvEntry[]>([]);
  let envRevealed = $state(false);
  let revealingEnv = $state(false);
  let formError = $state('');
  let saving = $state(false);

  // Logs modal
  let logsTarget = $state<App | null>(null);
  let logsText = $state('');
  let logsLoading = $state(false);

  // Delete modal
  let deleteTarget = $state<App | null>(null);
  let deleting = $state(false);

  function notifyJob(message: string, jobId?: string | null) {
    if (jobId) {
      toast.success(`${message} (${fmt(i18n.t.apps.jobToast, { jobId })})`, {
        href: `#/jobs/${jobId}`,
        label: i18n.t.apps.viewJob,
      });
    } else {
      toast.success(message);
    }
  }

  async function loadApps() {
    loading = true;
    try {
      const qs = new URLSearchParams({
        limit: String(limit),
        offset: String(offset),
      });
      const res = await api.get<PaginatedResponse<App>>(`/apps?${qs.toString()}`);
      apps = res.items;
      total = res.total;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
    }
  }

  async function loadDomains() {
    try {
      const res = await api.get<PaginatedResponse<Domain>>('/domains?limit=200&offset=0');
      domains = res.items;
    } catch {
      // ignore
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
    loadApps();
    loadDomains();
    loadUsers();
  });

  function openCreate() {
    editingApp = null;
    fName = '';
    fUserId = auth.user?.id ?? '';
    fRuntime = 'node';
    fRuntimeVersion = '22';
    fWorkDir = '';
    fCommand = '';
    fPort = '';
    fDomainId = '';
    fEnvEntries = [];
    envRevealed = true;
    formError = '';
    formOpen = true;
  }

  function openEdit(a: App) {
    editingApp = a;
    fName = a.name;
    fUserId = a.user_id;
    fRuntime = a.runtime;
    fRuntimeVersion = a.runtime_version;
    fWorkDir = a.work_dir;
    fCommand = a.command;
    fPort = a.port ? String(a.port) : '';
    fDomainId = a.domain_id ?? '';
    fEnvEntries = [];
    envRevealed = false;
    formError = '';
    formOpen = true;
  }

  async function revealEnv() {
    if (!editingApp) return;
    revealingEnv = true;
    formError = '';
    try {
      const res = await api.get<{ env: Record<string, string> }>(`/apps/${editingApp.id}/env`);
      fEnvEntries = Object.entries(res.env).map(([key, value]) => ({ key, value }));
      envRevealed = true;
    } catch (err) {
      formError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      revealingEnv = false;
    }
  }

  function addEnvRow() {
    fEnvEntries = [...fEnvEntries, { key: '', value: '' }];
  }

  function removeEnvRow(idx: number) {
    fEnvEntries = fEnvEntries.filter((_, i) => i !== idx);
  }

  function buildEnvMap(): Record<string, string> {
    const out: Record<string, string> = {};
    for (const entry of fEnvEntries) {
      const k = entry.key.trim();
      if (k) out[k] = entry.value;
    }
    return out;
  }

  async function submitForm(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    saving = true;
    const parsedPort = fPort.trim() ? Number(fPort.trim()) : 0;

    try {
      if (editingApp) {
        const body: Record<string, unknown> = {
          command: fCommand.trim(),
          runtime_version: fRuntimeVersion.trim(),
          port: Number.isFinite(parsedPort) ? parsedPort : 0,
          domain_id: fDomainId || null,
        };
        if (envRevealed) {
          body.env = buildEnvMap();
        }
        const res = await api.patch<{ app: App; job_id: string }>(`/apps/${editingApp.id}`, body);
        notifyJob(i18n.t.apps.updatedSuccess, res.job_id);
      } else {
        const body: Record<string, unknown> = {
          name: fName.trim(),
          runtime: fRuntime,
          runtime_version: fRuntimeVersion.trim(),
          command: fCommand.trim(),
          port: Number.isFinite(parsedPort) ? parsedPort : 0,
          env: buildEnvMap(),
        };
        if (canSelectOwner && fUserId) body.user_id = fUserId;
        if (fWorkDir.trim()) body.work_dir = fWorkDir.trim();
        if (fDomainId) body.domain_id = fDomainId;
        const res = await api.post<{ app: App; job_id: string }>('/apps', body);
        notifyJob(i18n.t.apps.createdSuccess, res.job_id);
      }
      formOpen = false;
      await loadApps();
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

  async function triggerAction(a: App, action: 'start' | 'stop' | 'restart') {
    try {
      const res = await api.post<{ job_id: string }>(`/apps/${a.id}/${action}`);
      notifyJob(fmt(i18n.t.apps.actionTriggered, { action }), res.job_id);
      await loadApps();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    }
  }

  async function openLogs(a: App) {
    logsTarget = a;
    logsText = '';
    logsLoading = true;
    try {
      const res = await api.get<{ logs: string }>(`/apps/${a.id}/logs`);
      logsText = res.logs || '';
    } catch (err) {
      logsText = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      logsLoading = false;
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    try {
      const res = await api.delete<{ job_id: string }>(`/apps/${deleteTarget.id}`);
      notifyJob(i18n.t.apps.deletedSuccess, res?.job_id);
      deleteTarget = null;
      await loadApps();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      deleting = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.apps.title}</h1>
    <p class="page-subtitle">{i18n.t.apps.subtitle}</p>
  </div>
  {#if canWrite}
    <button type="button" class="btn btn-primary" onclick={openCreate}>
      + {i18n.t.apps.createApp}
    </button>
  {/if}
</div>

<div class="toolbar">
  <div></div>
  <button type="button" class="btn btn-sm" onclick={loadApps} disabled={loading}>
    {i18n.t.common.refresh}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.apps.name}</th>
        <th>{i18n.t.apps.runtime}</th>
        <th>{i18n.t.apps.port}</th>
        <th>{i18n.t.common.status}</th>
        <th>{i18n.t.domains.domain}</th>
        <th>{i18n.t.common.actions}</th>
      </tr>
    </thead>
    <tbody>
      {#if loading && apps.length === 0}
        <tr><td colspan="6" class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if apps.length === 0}
        <tr><td colspan="6" class="empty-state">{i18n.t.common.noData}</td></tr>
      {:else}
        {#each apps as a (a.id)}
          <tr>
            <td class="mono">
              <strong>{a.name}</strong>
              <div class="form-hint">{a.command}</div>
            </td>
            <td>
              <span class="badge badge-primary">{a.runtime} {a.runtime_version}</span>
            </td>
            <td class="mono">{a.port || i18n.t.apps.portAuto}</td>
            <td>
              <span
                class="badge"
                class:badge-success={a.status === 'running'}
                class:badge-danger={a.status === 'failed'}
              >
                {a.status === 'running'
                  ? i18n.t.apps.statusRunning
                  : a.status === 'failed'
                    ? i18n.t.apps.statusFailed
                    : i18n.t.apps.statusStopped}
              </span>
            </td>
            <td class="mono">
              {#if a.domain_id}
                <a href="#/domains">{domainMap.get(a.domain_id) ?? a.domain_id}</a>
              {:else}
                —
              {/if}
            </td>
            <td>
              <div class="table-actions">
                {#if canWrite}
                  {#if a.status !== 'running'}
                    <button type="button" class="btn btn-sm" onclick={() => triggerAction(a, 'start')}>
                      {i18n.t.apps.start}
                    </button>
                  {:else}
                    <button type="button" class="btn btn-sm" onclick={() => triggerAction(a, 'stop')}>
                      {i18n.t.apps.stop}
                    </button>
                  {/if}
                  <button type="button" class="btn btn-sm" onclick={() => triggerAction(a, 'restart')}>
                    {i18n.t.apps.restart}
                  </button>
                  <button type="button" class="btn btn-sm" onclick={() => openEdit(a)}>
                    {i18n.t.common.edit}
                  </button>
                {/if}
                <button type="button" class="btn btn-sm" onclick={() => openLogs(a)}>
                  {i18n.t.apps.logs}
                </button>
                {#if canWrite}
                  <button
                    type="button"
                    class="btn btn-sm btn-danger"
                    onclick={() => (deleteTarget = a)}
                  >
                    {i18n.t.common.delete}
                  </button>
                {/if}
              </div>
            </td>
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
  onchange={(newOffset) => {
    offset = newOffset;
    loadApps();
  }}
/>

<Modal
  open={formOpen}
  title={editingApp ? i18n.t.apps.editApp : i18n.t.apps.createApp}
  size="lg"
  onclose={() => (formOpen = false)}
>
  <form id="app-form" onsubmit={submitForm}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="a-name">{i18n.t.apps.name}</label>
        <input
          id="a-name"
          type="text"
          class="form-control"
          bind:value={fName}
          disabled={editingApp !== null}
          required
          placeholder="api-go"
        />
      </div>
      {#if canSelectOwner && !editingApp && users.length > 0}
        <div class="form-group">
          <label class="form-label" for="a-owner">{i18n.t.apps.owner}</label>
          <select id="a-owner" class="form-control" bind:value={fUserId}>
            {#each users as u (u.id)}
              <option value={u.id}>{u.username} ({i18n.t.roles[u.role]})</option>
            {/each}
          </select>
        </div>
      {/if}
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="a-runtime">{i18n.t.apps.runtime}</label>
        <select
          id="a-runtime"
          class="form-control"
          bind:value={fRuntime}
          disabled={editingApp !== null}
        >
          <option value="node">node</option>
          <option value="go">go</option>
          <option value="python">python</option>
        </select>
      </div>
      <div class="form-group">
        <label class="form-label" for="a-runtime-ver">{i18n.t.apps.runtimeVersion}</label>
        <input
          id="a-runtime-ver"
          type="text"
          class="form-control"
          bind:value={fRuntimeVersion}
          placeholder="22"
        />
      </div>
      <div class="form-group">
        <label class="form-label" for="a-port">{i18n.t.apps.port}</label>
        <input
          id="a-port"
          type="number"
          min="0"
          max="65535"
          class="form-control"
          bind:value={fPort}
          placeholder="0"
        />
      </div>
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="a-workdir">{i18n.t.apps.workDir}</label>
        <input
          id="a-workdir"
          type="text"
          class="form-control"
          bind:value={fWorkDir}
          disabled={editingApp !== null}
          placeholder="/home/user/apps/my-app"
        />
      </div>
      <div class="form-group">
        <label class="form-label" for="a-domain">{i18n.t.apps.domain}</label>
        <select id="a-domain" class="form-control" bind:value={fDomainId}>
          <option value="">{i18n.t.common.none}</option>
          {#each domains as d (d.id)}
            <option value={d.id}>{d.domain}</option>
          {/each}
        </select>
      </div>
    </div>

    <div class="form-group">
      <label class="form-label" for="a-command">{i18n.t.apps.command}</label>
      <input
        id="a-command"
        type="text"
        class="form-control"
        bind:value={fCommand}
        required
        placeholder="node dist/server.js"
      />
    </div>

    <div class="form-group">
      <div class="env-header">
        <span class="form-label">{i18n.t.apps.envVars}</span>
        {#if envRevealed}
          <button type="button" class="btn btn-sm" onclick={addEnvRow}>
            + {i18n.t.apps.addEnvVar}
          </button>
        {:else if editingApp}
          <button
            type="button"
            class="btn btn-sm"
            onclick={revealEnv}
            disabled={revealingEnv}
          >
            {revealingEnv ? i18n.t.common.loading : i18n.t.apps.revealEnv}
          </button>
        {/if}
      </div>

      {#if !envRevealed && editingApp}
        <p class="form-hint">
          {i18n.t.apps.envHiddenHint}
          {#if editingApp.env_keys.length > 0}
            ({editingApp.env_keys.join(', ')})
          {/if}
        </p>
      {:else}
        {#each fEnvEntries as entry, idx (idx)}
          <div class="env-row">
            <input
              type="text"
              class="form-control mono"
              placeholder={i18n.t.apps.envKey}
              bind:value={entry.key}
            />
            <input
              type="text"
              class="form-control mono"
              placeholder={i18n.t.apps.envValue}
              bind:value={entry.value}
            />
            <button
              type="button"
              class="btn btn-sm btn-danger"
              onclick={() => removeEnvRow(idx)}
              aria-label={i18n.t.common.delete}
            >
              ✕
            </button>
          </div>
        {/each}
      {/if}
    </div>
  </form>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (formOpen = false)} disabled={saving}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="app-form" class="btn btn-primary" disabled={saving}>
      {saving ? i18n.t.common.loading : i18n.t.common.save}
    </button>
  {/snippet}
</Modal>

<Modal
  open={logsTarget !== null}
  title={logsTarget ? fmt(i18n.t.apps.logsTitle, { name: logsTarget.name }) : ''}
  size="lg"
  onclose={() => (logsTarget = null)}
>
  {#if logsLoading}
    <p>{i18n.t.common.loading}</p>
  {:else}
    <pre class="log-box">{logsText || i18n.t.apps.noLogs}</pre>
  {/if}
  {#snippet footer()}
    {#if logsTarget}
      <button type="button" class="btn" onclick={() => logsTarget && openLogs(logsTarget)} disabled={logsLoading}>
        {i18n.t.common.refresh}
      </button>
    {/if}
    <button type="button" class="btn" onclick={() => (logsTarget = null)}>
      {i18n.t.common.close}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.apps.deleteConfirmTitle}
  onclose={() => (deleteTarget = null)}
>
  {#if deleteTarget}
    <p>{fmt(i18n.t.apps.deleteConfirmBody, { name: deleteTarget.name })}</p>
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

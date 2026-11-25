<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '../components/Modal.svelte';
  import Pagination from '../components/Pagination.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { ComposeStack, Container, Domain, PaginatedResponse, User } from '../lib/types';

  interface EnvEntry {
    key: string;
    value: string;
  }

  let activeTab = $state<'containers' | 'stacks'>('containers');

  // Containers state
  let containers = $state<Container[]>([]);
  let totalContainers = $state(0);
  let limit = 25;
  let offset = $state(0);
  let loadingContainers = $state(true);

  // Compose stacks state
  let stacks = $state<ComposeStack[]>([]);
  let loadingStacks = $state(true);

  // Supporting data
  let domains = $state<Domain[]>([]);
  let users = $state<User[]>([]);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let canSelectOwner = $derived(auth.user?.role === 'admin' || auth.user?.role === 'reseller');

  // Create container modal
  let createContainerOpen = $state(false);
  let cName = $state('');
  let cImage = $state('');
  let cPorts = $state('');
  let cDomainId = $state('');
  let cCpuLimit = $state('1.0');
  let cMemoryMb = $state(512);
  let cUserId = $state('');
  let cEnvEntries = $state<EnvEntry[]>([]);
  let cFormError = $state('');
  let cSaving = $state(false);

  // Logs modal
  let logsTarget = $state<Container | null>(null);
  let logsText = $state('');
  let logsLoading = $state(false);

  // Delete container modal
  let deleteContainerTarget = $state<Container | null>(null);
  let deletingContainer = $state(false);

  // Deploy stack modal
  let deployStackOpen = $state(false);
  let sName = $state('');
  let sComposeYaml = $state('');
  let sUserId = $state('');
  let sFormError = $state('');
  let sSaving = $state(false);

  // Delete stack modal
  let deleteStackTarget = $state<ComposeStack | null>(null);
  let deletingStack = $state(false);

  function notifyJob(message: string, jobId?: string | null) {
    if (jobId) {
      toast.success(`${message} (${fmt(i18n.t.docker.jobToast, { jobId })})`, {
        href: `#/jobs/${jobId}`,
        label: i18n.t.docker.viewJob,
      });
    } else {
      toast.success(message);
    }
  }

  function formatApiError(err: unknown): string {
    if (err instanceof ApiError) {
      const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
      return fieldMsg || err.detail || err.title;
    }
    return i18n.t.common.errorGeneric;
  }

  async function loadContainers() {
    loadingContainers = true;
    try {
      const qs = new URLSearchParams({
        limit: String(limit),
        offset: String(offset),
      });
      const res = await api.get<PaginatedResponse<Container>>(`/docker/containers?${qs.toString()}`);
      containers = res.items;
      totalContainers = res.total;
    } catch (err) {
      toast.error(formatApiError(err));
    } finally {
      loadingContainers = false;
    }
  }

  async function loadStacks() {
    loadingStacks = true;
    try {
      const res = await api.get<{ items: ComposeStack[] }>('/docker/stacks');
      stacks = res.items;
    } catch (err) {
      toast.error(formatApiError(err));
    } finally {
      loadingStacks = false;
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
    loadContainers();
    loadStacks();
    loadDomains();
    loadUsers();
  });

  function openCreateContainer() {
    cName = '';
    cImage = '';
    cPorts = '';
    cDomainId = '';
    cCpuLimit = '1.0';
    cMemoryMb = 512;
    cUserId = auth.user?.id ?? '';
    cEnvEntries = [];
    cFormError = '';
    createContainerOpen = true;
  }

  function addEnvRow() {
    cEnvEntries = [...cEnvEntries, { key: '', value: '' }];
  }

  function removeEnvRow(idx: number) {
    cEnvEntries = cEnvEntries.filter((_, i) => i !== idx);
  }

  function buildEnvMap(): Record<string, string> {
    const out: Record<string, string> = {};
    for (const entry of cEnvEntries) {
      const k = entry.key.trim();
      if (k) out[k] = entry.value;
    }
    return out;
  }

  async function submitCreateContainer(e: SubmitEvent) {
    e.preventDefault();
    cFormError = '';
    cSaving = true;
    try {
      const ports = cPorts
        .split(',')
        .map((p) => p.trim())
        .filter((p) => p.length > 0);
      const env = buildEnvMap();

      const body: Record<string, unknown> = {
        name: cName.trim(),
        image: cImage.trim(),
        ports,
        memory_mb: Number(cMemoryMb) || 512,
      };
      if (cCpuLimit.trim()) body.cpu_limit = cCpuLimit.trim();
      if (cDomainId) body.domain_id = cDomainId;
      if (Object.keys(env).length > 0) body.env = env;
      if (canSelectOwner && cUserId) body.user_id = cUserId;

      const res = await api.post<{ container: Container; job_id: string }>('/docker/containers', body);
      notifyJob(i18n.t.docker.createdContainer, res.job_id);
      createContainerOpen = false;
      await loadContainers();
    } catch (err) {
      cFormError = formatApiError(err);
    } finally {
      cSaving = false;
    }
  }

  async function triggerContainerAction(c: Container, action: 'start' | 'stop' | 'restart') {
    try {
      const res = await api.post<{ job_id: string }>(`/docker/containers/${c.id}/${action}`);
      notifyJob(fmt(i18n.t.docker.actionTriggered, { action }), res.job_id);
      await loadContainers();
    } catch (err) {
      toast.error(formatApiError(err));
    }
  }

  async function openLogs(c: Container) {
    logsTarget = c;
    logsText = '';
    logsLoading = true;
    try {
      const res = await api.get<{ logs: string }>(`/docker/containers/${c.id}/logs`);
      logsText = res.logs || '';
    } catch (err) {
      logsText = formatApiError(err);
    } finally {
      logsLoading = false;
    }
  }

  async function confirmDeleteContainer() {
    if (!deleteContainerTarget) return;
    deletingContainer = true;
    try {
      const res = await api.delete<{ job_id: string }>(`/docker/containers/${deleteContainerTarget.id}`);
      notifyJob(i18n.t.docker.deletedContainer, res?.job_id);
      deleteContainerTarget = null;
      await loadContainers();
    } catch (err) {
      toast.error(formatApiError(err));
      deleteContainerTarget = null;
    } finally {
      deletingContainer = false;
    }
  }

  function openDeployStack() {
    sName = '';
    sComposeYaml = '';
    sUserId = auth.user?.id ?? '';
    sFormError = '';
    deployStackOpen = true;
  }

  async function submitDeployStack(e: SubmitEvent) {
    e.preventDefault();
    sFormError = '';
    sSaving = true;
    try {
      const body: Record<string, unknown> = {
        name: sName.trim(),
        compose_yaml: sComposeYaml,
      };
      if (canSelectOwner && sUserId) body.user_id = sUserId;

      const res = await api.post<{ stack: ComposeStack; job_id: string }>('/docker/stacks', body);
      notifyJob(i18n.t.docker.createdStack, res.job_id);
      deployStackOpen = false;
      await loadStacks();
    } catch (err) {
      sFormError = formatApiError(err);
    } finally {
      sSaving = false;
    }
  }

  async function confirmDeleteStack() {
    if (!deleteStackTarget) return;
    deletingStack = true;
    try {
      const res = await api.delete<{ job_id: string }>(`/docker/stacks/${deleteStackTarget.id}`);
      notifyJob(i18n.t.docker.deletedStack, res?.job_id);
      deleteStackTarget = null;
      await loadStacks();
    } catch (err) {
      toast.error(formatApiError(err));
      deleteStackTarget = null;
    } finally {
      deletingStack = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.docker.title}</h1>
    <p class="page-subtitle">{i18n.t.docker.subtitle}</p>
  </div>
  {#if canWrite}
    {#if activeTab === 'containers'}
      <button type="button" class="btn btn-primary" onclick={openCreateContainer}>
        + {i18n.t.docker.createContainer}
      </button>
    {:else}
      <button type="button" class="btn btn-primary" onclick={openDeployStack}>
        + {i18n.t.docker.deployStack}
      </button>
    {/if}
  {/if}
</div>

<div class="tabs" role="tablist">
  <button
    type="button"
    role="tab"
    aria-selected={activeTab === 'containers'}
    class="tab-btn"
    class:active={activeTab === 'containers'}
    onclick={() => (activeTab = 'containers')}
  >
    {i18n.t.docker.tabContainers}
  </button>
  <button
    type="button"
    role="tab"
    aria-selected={activeTab === 'stacks'}
    class="tab-btn"
    class:active={activeTab === 'stacks'}
    onclick={() => (activeTab = 'stacks')}
  >
    {i18n.t.docker.tabStacks}
  </button>
</div>

{#if activeTab === 'containers'}
  <div class="toolbar">
    <div></div>
    <button type="button" class="btn btn-sm" onclick={loadContainers} disabled={loadingContainers}>
      {i18n.t.common.refresh}
    </button>
  </div>

  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.docker.name}</th>
          <th>{i18n.t.docker.image}</th>
          <th>{i18n.t.docker.ports}</th>
          <th>{i18n.t.docker.resources}</th>
          <th>{i18n.t.common.status}</th>
          <th>{i18n.t.common.actions}</th>
        </tr>
      </thead>
      <tbody>
        {#if loadingContainers && containers.length === 0}
          <tr><td colspan="6" class="empty-state">{i18n.t.common.loading}</td></tr>
        {:else if containers.length === 0}
          <tr><td colspan="6" class="empty-state">{i18n.t.common.noData}</td></tr>
        {:else}
          {#each containers as c (c.id)}
            <tr>
              <td class="mono"><strong>{c.name}</strong></td>
              <td class="mono">{c.image}</td>
              <td class="mono">{c.ports && c.ports.length > 0 ? c.ports.join(', ') : '—'}</td>
              <td class="mono">{c.cpu_limit || '—'} CPU / {c.memory_mb} MB</td>
              <td>
                <span class="badge {c.status === 'running' ? 'badge-success' : 'badge-warning'}">
                  {c.status === 'running' ? i18n.t.docker.statusRunning : i18n.t.docker.statusStopped}
                </span>
              </td>
              <td>
                <div class="table-actions">
                  {#if canWrite}
                    {#if c.status !== 'running'}
                      <button
                        type="button"
                        class="btn btn-sm"
                        onclick={() => triggerContainerAction(c, 'start')}
                      >
                        {i18n.t.docker.start}
                      </button>
                    {:else}
                      <button
                        type="button"
                        class="btn btn-sm"
                        onclick={() => triggerContainerAction(c, 'stop')}
                      >
                        {i18n.t.docker.stop}
                      </button>
                    {/if}
                    <button
                      type="button"
                      class="btn btn-sm"
                      onclick={() => triggerContainerAction(c, 'restart')}
                    >
                      {i18n.t.docker.restart}
                    </button>
                  {/if}
                  <button
                    type="button"
                    class="btn btn-sm"
                    onclick={() => openLogs(c)}
                  >
                    {i18n.t.docker.logs}
                  </button>
                  {#if canWrite}
                    <button
                      type="button"
                      class="btn btn-sm btn-danger"
                      onclick={() => (deleteContainerTarget = c)}
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
    total={totalContainers}
    {limit}
    {offset}
    onchange={(nextOffset) => {
      offset = nextOffset;
      loadContainers();
    }}
  />
{:else}
  <div class="toolbar">
    <div></div>
    <button type="button" class="btn btn-sm" onclick={loadStacks} disabled={loadingStacks}>
      {i18n.t.common.refresh}
    </button>
  </div>

  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.docker.name}</th>
          <th>{i18n.t.common.status}</th>
          <th>{i18n.t.common.createdAt}</th>
          {#if canWrite}
            <th>{i18n.t.common.actions}</th>
          {/if}
        </tr>
      </thead>
      <tbody>
        {#if loadingStacks && stacks.length === 0}
          <tr><td colspan={canWrite ? 4 : 3} class="empty-state">{i18n.t.common.loading}</td></tr>
        {:else if stacks.length === 0}
          <tr><td colspan={canWrite ? 4 : 3} class="empty-state">{i18n.t.common.noData}</td></tr>
        {:else}
          {#each stacks as st (st.id)}
            <tr>
              <td class="mono"><strong>{st.name}</strong></td>
              <td>
                <span class="badge {st.status === 'running' ? 'badge-success' : 'badge-warning'}">
                  {st.status === 'running' ? i18n.t.docker.statusRunning : i18n.t.docker.statusStopped}
                </span>
              </td>
              <td>{formatDateTime(st.created_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</td>
              {#if canWrite}
                <td>
                  <div class="table-actions">
                    <button
                      type="button"
                      class="btn btn-sm btn-danger"
                      onclick={() => (deleteStackTarget = st)}
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
{/if}

<Modal
  open={createContainerOpen}
  title={i18n.t.docker.createContainer}
  size="lg"
  onclose={() => (createContainerOpen = false)}
>
  <form id="container-form" onsubmit={submitCreateContainer}>
    {#if cFormError}
      <div class="alert alert-danger" role="alert">{cFormError}</div>
    {/if}

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="c-name">{i18n.t.docker.name}</label>
        <input
          id="c-name"
          type="text"
          class="form-control mono"
          bind:value={cName}
          placeholder="web-redis"
          required
        />
      </div>
      <div class="form-group">
        <label class="form-label" for="c-image">{i18n.t.docker.image}</label>
        <input
          id="c-image"
          type="text"
          class="form-control mono"
          bind:value={cImage}
          placeholder="nginx:alpine"
          required
        />
      </div>
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="c-ports">{i18n.t.docker.ports}</label>
        <input
          id="c-ports"
          type="text"
          class="form-control mono"
          bind:value={cPorts}
          placeholder="127.0.0.1:8081:80"
        />
        <p class="form-hint">{i18n.t.docker.portsHint}</p>
      </div>
      <div class="form-group">
        <label class="form-label" for="c-domain">{i18n.t.docker.domain}</label>
        <select id="c-domain" class="form-control" bind:value={cDomainId}>
          <option value="">{i18n.t.common.none}</option>
          {#each domains as d (d.id)}
            <option value={d.id}>{d.domain}</option>
          {/each}
        </select>
      </div>
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="c-cpu">{i18n.t.docker.cpuLimit}</label>
        <input
          id="c-cpu"
          type="text"
          class="form-control mono"
          bind:value={cCpuLimit}
          placeholder="1.0"
        />
      </div>
      <div class="form-group">
        <label class="form-label" for="c-mem">{i18n.t.docker.memoryMb}</label>
        <input
          id="c-mem"
          type="number"
          class="form-control"
          bind:value={cMemoryMb}
          min="64"
          step="64"
          required
        />
      </div>
    </div>

    {#if canSelectOwner && users.length > 0}
      <div class="form-group">
        <label class="form-label" for="c-owner">{i18n.t.docker.owner}</label>
        <select id="c-owner" class="form-control" bind:value={cUserId}>
          {#each users as u (u.id)}
            <option value={u.id}>{u.username} ({u.role})</option>
          {/each}
        </select>
      </div>
    {/if}

    <div class="form-group">
      <div class="env-header">
        <span class="form-label">{i18n.t.docker.envVars}</span>
        <button type="button" class="btn btn-sm" onclick={addEnvRow}>
          + {i18n.t.docker.addEnv}
        </button>
      </div>
      {#each cEnvEntries as entry, idx (idx)}
        <div class="env-row">
          <input
            type="text"
            class="form-control mono"
            placeholder={i18n.t.docker.envKey}
            bind:value={entry.key}
          />
          <input
            type="text"
            class="form-control mono"
            placeholder={i18n.t.docker.envValue}
            bind:value={entry.value}
          />
          <button type="button" class="btn btn-sm btn-danger" onclick={() => removeEnvRow(idx)}>
            ×
          </button>
        </div>
      {/each}
    </div>
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (createContainerOpen = false)} disabled={cSaving}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="container-form" class="btn btn-primary" disabled={cSaving}>
      {cSaving ? i18n.t.common.loading : i18n.t.common.create}
    </button>
  {/snippet}
</Modal>

<Modal
  open={logsTarget !== null}
  title={logsTarget ? fmt(i18n.t.docker.logsTitle, { name: logsTarget.name }) : ''}
  size="lg"
  onclose={() => (logsTarget = null)}
>
  {#if logsLoading}
    <p>{i18n.t.common.loading}</p>
  {:else}
    <pre class="log-box">{logsText || i18n.t.docker.noLogs}</pre>
  {/if}
  {#snippet footer()}
    <button type="button" class="btn btn-primary" onclick={() => (logsTarget = null)}>
      {i18n.t.common.close}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteContainerTarget !== null}
  title={i18n.t.docker.deleteContainerTitle}
  onclose={() => (deleteContainerTarget = null)}
>
  {#if deleteContainerTarget}
    <p>{fmt(i18n.t.docker.deleteContainerBody, { name: deleteContainerTarget.name })}</p>
  {/if}
  {#snippet footer()}
    <button
      type="button"
      class="btn"
      onclick={() => (deleteContainerTarget = null)}
      disabled={deletingContainer}
    >
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-danger"
      onclick={confirmDeleteContainer}
      disabled={deletingContainer}
    >
      {deletingContainer ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deployStackOpen}
  title={i18n.t.docker.deployStack}
  size="lg"
  onclose={() => (deployStackOpen = false)}
>
  <form id="stack-form" onsubmit={submitDeployStack}>
    {#if sFormError}
      <div class="alert alert-danger" role="alert">{sFormError}</div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="s-name">{i18n.t.docker.name}</label>
      <input
        id="s-name"
        type="text"
        class="form-control mono"
        bind:value={sName}
        placeholder="my-stack"
        required
      />
    </div>

    {#if canSelectOwner && users.length > 0}
      <div class="form-group">
        <label class="form-label" for="s-owner">{i18n.t.docker.owner}</label>
        <select id="s-owner" class="form-control" bind:value={sUserId}>
          {#each users as u (u.id)}
            <option value={u.id}>{u.username} ({u.role})</option>
          {/each}
        </select>
      </div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="s-yaml">{i18n.t.docker.composeYaml}</label>
      <textarea
        id="s-yaml"
        class="form-control mono"
        rows="12"
        bind:value={sComposeYaml}
        placeholder="services:\n  web:\n    image: nginx:alpine"
        required
      ></textarea>
    </div>
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (deployStackOpen = false)} disabled={sSaving}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="stack-form" class="btn btn-primary" disabled={sSaving}>
      {sSaving ? i18n.t.common.loading : i18n.t.docker.deployStack}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteStackTarget !== null}
  title={i18n.t.docker.deleteStackTitle}
  onclose={() => (deleteStackTarget = null)}
>
  {#if deleteStackTarget}
    <p>{fmt(i18n.t.docker.deleteStackBody, { name: deleteStackTarget.name })}</p>
  {/if}
  {#snippet footer()}
    <button
      type="button"
      class="btn"
      onclick={() => (deleteStackTarget = null)}
      disabled={deletingStack}
    >
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-danger"
      onclick={confirmDeleteStack}
      disabled={deletingStack}
    >
      {deletingStack ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  {/snippet}
</Modal>

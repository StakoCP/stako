<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { router } from '../lib/router.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { SystemService, SystemServiceAction } from '../lib/types';

  type FilterTab = 'all' | 'installed' | 'running' | 'not-installed';

  let services = $state<SystemService[]>([]);
  let loading = $state(true);
  let activeFilter = $state<FilterTab>('all');
  let busyId = $state<string | null>(null);

  let uninstallTarget = $state<SystemService | null>(null);
  let uninstalling = $state(false);

  function isServiceActive(svc: SystemService): boolean {
    return svc.active === 'active';
  }

  let totalCount = $derived(services.length);
  let installedCount = $derived(services.filter((s) => s.installed).length);
  let runningCount = $derived(services.filter((s) => s.installed && isServiceActive(s)).length);
  let notInstalledCount = $derived(services.filter((s) => !s.installed).length);

  let filteredServices = $derived.by(() => {
    if (activeFilter === 'installed') return services.filter((s) => s.installed);
    if (activeFilter === 'running') return services.filter((s) => s.installed && isServiceActive(s));
    if (activeFilter === 'not-installed') return services.filter((s) => !s.installed);
    return services;
  });

  function formatApiError(err: unknown): string {
    if (err instanceof ApiError) {
      return err.detail || err.title;
    }
    return i18n.t.common.errorGeneric;
  }

  function notifyJob(message: string, jobId?: string | null) {
    if (jobId) {
      toast.success(`${message} (${fmt(i18n.t.sysServices.jobToast, { jobId })})`, {
        href: `#/jobs/${jobId}`,
        label: i18n.t.sysServices.viewJob,
      });
    } else {
      toast.success(message);
    }
  }

  async function loadServices() {
    loading = true;
    try {
      const res = await api.get<{ items: SystemService[] }>('/system/services');
      services = res.items;
    } catch (err) {
      toast.error(formatApiError(err));
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    loadServices();
  });

  async function handleAction(svc: SystemService, action: SystemServiceAction) {
    busyId = svc.id;
    try {
      const res = await api.post<{ ok: boolean; output: string }>(
        `/system/services/${encodeURIComponent(svc.id)}/action`,
        { action }
      );
      if (res.ok) {
        toast.success(fmt(i18n.t.sysServices.actionSuccess, { action, label: svc.label }));
      } else {
        toast.error(
          res.output?.trim() ||
            fmt(i18n.t.sysServices.actionFailed, { action, label: svc.label })
        );
      }
      await loadServices();
    } catch (err) {
      toast.error(formatApiError(err));
    } finally {
      busyId = null;
    }
  }

  async function handleInstall(svc: SystemService) {
    busyId = svc.id;
    try {
      const res = await api.post<{ job_id: string }>(
        `/system/services/${encodeURIComponent(svc.id)}/install`
      );
      notifyJob(fmt(i18n.t.sysServices.installQueued, { label: svc.label }), res?.job_id);
      await loadServices();
    } catch (err) {
      toast.error(formatApiError(err));
    } finally {
      busyId = null;
    }
  }

  async function confirmUninstall() {
    if (!uninstallTarget) return;
    const target = uninstallTarget;
    uninstalling = true;
    busyId = target.id;
    try {
      const res = await api.delete<{ job_id: string }>(
        `/system/services/${encodeURIComponent(target.id)}`
      );
      notifyJob(fmt(i18n.t.sysServices.uninstallQueued, { label: target.label }), res?.job_id);
      uninstallTarget = null;
      await loadServices();
    } catch (err) {
      toast.error(formatApiError(err));
      uninstallTarget = null;
    } finally {
      uninstalling = false;
      busyId = null;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.sysServices.title}</h1>
    <p class="page-subtitle">{i18n.t.sysServices.subtitle}</p>
  </div>
  <button
    type="button"
    class="btn btn-sm"
    onclick={loadServices}
    disabled={loading || busyId !== null}
  >
    {i18n.t.common.refresh}
  </button>
</div>

<div class="grid-cards">
  <div class="card">
    <div class="stat-label">{i18n.t.sysServices.total}</div>
    <div class="stat-value">{totalCount}</div>
  </div>
  <div class="card">
    <div class="stat-label">{i18n.t.sysServices.installed}</div>
    <div class="stat-value">{installedCount}</div>
  </div>
  <div class="card">
    <div class="stat-label">{i18n.t.sysServices.running}</div>
    <div class="stat-value">{runningCount}</div>
  </div>
  <div class="card">
    <div class="stat-label">{i18n.t.sysServices.notInstalled}</div>
    <div class="stat-value">{notInstalledCount}</div>
  </div>
</div>

<div class="card">
  <div class="tabs" role="tablist" aria-label={i18n.t.sysServices.title}>
    <button
      type="button"
      role="tab"
      aria-selected={activeFilter === 'all'}
      class="tab-btn"
      class:active={activeFilter === 'all'}
      onclick={() => (activeFilter = 'all')}
    >
      {i18n.t.sysServices.filterAll} ({totalCount})
    </button>
    <button
      type="button"
      role="tab"
      aria-selected={activeFilter === 'installed'}
      class="tab-btn"
      class:active={activeFilter === 'installed'}
      onclick={() => (activeFilter = 'installed')}
    >
      {i18n.t.sysServices.filterInstalled} ({installedCount})
    </button>
    <button
      type="button"
      role="tab"
      aria-selected={activeFilter === 'running'}
      class="tab-btn"
      class:active={activeFilter === 'running'}
      onclick={() => (activeFilter = 'running')}
    >
      {i18n.t.sysServices.filterRunning} ({runningCount})
    </button>
    <button
      type="button"
      role="tab"
      aria-selected={activeFilter === 'not-installed'}
      class="tab-btn"
      class:active={activeFilter === 'not-installed'}
      onclick={() => (activeFilter = 'not-installed')}
    >
      {i18n.t.sysServices.filterNotInstalled} ({notInstalledCount})
    </button>
  </div>

  {#if loading && services.length === 0}
    <p class="empty-state">{i18n.t.common.loading}</p>
  {:else if filteredServices.length === 0}
    <p class="empty-state">{i18n.t.common.noData}</p>
  {:else}
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{i18n.t.sysServices.colLabel}</th>
            <th>{i18n.t.sysServices.colCategory}</th>
            <th>{i18n.t.sysServices.colPackage}</th>
            <th>{i18n.t.sysServices.colStatus}</th>
            <th>{i18n.t.common.actions}</th>
          </tr>
        </thead>
        <tbody>
          {#each filteredServices as svc (svc.id)}
            {@const isBusy = busyId === svc.id}
            <tr>
              <td>
                <div class="mini-list-main">
                  <strong>{svc.label}</strong>
                  <span class="mini-list-meta mono">{svc.unit}</span>
                </div>
              </td>
              <td>
                <span class="badge">{svc.category}</span>
              </td>
              <td>
                <div class="mini-list-main">
                  <code class="mono">{svc.apt_pkg}</code>
                  <span class="mini-list-meta mono">{svc.version || '—'}</span>
                </div>
              </td>
              <td>
                <div class="table-actions">
                  {#if !svc.installed}
                    <span class="badge">{i18n.t.sysServices.statusNotInstalled}</span>
                  {:else if isServiceActive(svc)}
                    <span class="badge badge-success">{i18n.t.sysServices.statusActive}</span>
                  {:else}
                    <span class="badge badge-warning">{i18n.t.sysServices.statusInactive}</span>
                  {/if}
                  {#if svc.protected}
                    <span class="badge badge-info">{i18n.t.sysServices.protectedBadge}</span>
                  {/if}
                </div>
              </td>
              <td>
                <div class="table-actions">
                  {#if svc.installed}
                    {#if !isServiceActive(svc)}
                      <button
                        type="button"
                        class="btn btn-sm btn-primary"
                        onclick={() => handleAction(svc, 'start')}
                        disabled={isBusy}
                      >
                        {i18n.t.sysServices.actionStart}
                      </button>
                    {:else if !svc.protected}
                      <button
                        type="button"
                        class="btn btn-sm"
                        onclick={() => handleAction(svc, 'stop')}
                        disabled={isBusy}
                      >
                        {i18n.t.sysServices.actionStop}
                      </button>
                    {/if}
                    <button
                      type="button"
                      class="btn btn-sm"
                      onclick={() => handleAction(svc, 'restart')}
                      disabled={isBusy}
                    >
                      {i18n.t.sysServices.actionRestart}
                    </button>
                    {#if svc.config_service}
                      <button
                        type="button"
                        class="btn btn-sm"
                        onclick={() => router.navigate('/configs')}
                        disabled={isBusy}
                      >
                        {i18n.t.sysServices.actionConfig}
                      </button>
                    {/if}
                    {#if !svc.protected}
                      <button
                        type="button"
                        class="btn btn-sm btn-danger"
                        onclick={() => (uninstallTarget = svc)}
                        disabled={isBusy}
                      >
                        {i18n.t.sysServices.actionUninstall}
                      </button>
                    {/if}
                  {:else}
                    <button
                      type="button"
                      class="btn btn-sm btn-primary"
                      onclick={() => handleInstall(svc)}
                      disabled={isBusy}
                    >
                      {i18n.t.sysServices.actionInstall}
                    </button>
                  {/if}
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<Modal
  open={uninstallTarget !== null}
  title={i18n.t.sysServices.uninstallTitle}
  onclose={() => {
    if (!uninstalling) uninstallTarget = null;
  }}
>
  {#if uninstallTarget}
    <p>
      {fmt(i18n.t.sysServices.uninstallConfirm, {
        label: uninstallTarget.label,
        pkg: uninstallTarget.apt_pkg,
      })}
    </p>
  {/if}
  {#snippet footer()}
    <button
      type="button"
      class="btn"
      onclick={() => (uninstallTarget = null)}
      disabled={uninstalling}
    >
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-danger"
      onclick={confirmUninstall}
      disabled={uninstalling}
    >
      {uninstalling ? i18n.t.common.loading : i18n.t.sysServices.actionUninstall}
    </button>
  {/snippet}
</Modal>

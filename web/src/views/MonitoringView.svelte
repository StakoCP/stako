<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { AlertChannel, AlertRule, MonitoringMetrics, UptimeMonitor } from '../lib/types';

  let metrics = $state<MonitoringMetrics | null>(null);
  let alerts = $state<AlertRule[]>([]);
  let monitors = $state<UptimeMonitor[]>([]);
  let loadingMetrics = $state(true);
  let loadingAlerts = $state(true);
  let loadingMonitors = $state(true);
  let checkingId = $state<string | null>(null);

  let localeCode = $derived(i18n.locale === 'id' ? 'id-ID' : 'en-US');

  // Create Alert modal
  let createOpen = $state(false);
  let fName = $state('');
  let fChannel = $state<AlertChannel>('email');
  let fTarget = $state('');
  let fThreshold = $state(85);
  let formError = $state('');
  let saving = $state(false);

  // Delete Alert modal
  let deleteTarget = $state<AlertRule | null>(null);
  let deleting = $state(false);

  // Create Uptime Monitor modal
  let createMonitorOpen = $state(false);
  let mDomain = $state('');
  let mUrl = $state('');
  let mExpectedStatus = $state(200);
  let mIntervalSec = $state(300);
  let mPublicStatus = $state(true);
  let monitorFormError = $state('');
  let savingMonitor = $state(false);

  // Delete Uptime Monitor modal
  let deleteMonitorTarget = $state<UptimeMonitor | null>(null);
  let deletingMonitor = $state(false);

  function clampPct(val: number): number {
    return Math.max(0, Math.min(100, Math.round(val)));
  }

  async function loadMetrics() {
    loadingMetrics = true;
    try {
      metrics = await api.get<MonitoringMetrics>('/monitoring/metrics');
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingMetrics = false;
    }
  }

  async function loadAlerts() {
    loadingAlerts = true;
    try {
      const res = await api.get<{ items: AlertRule[] }>('/monitoring/alerts');
      alerts = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingAlerts = false;
    }
  }

  async function loadMonitors() {
    loadingMonitors = true;
    try {
      const res = await api.get<{ items?: UptimeMonitor[] } | UptimeMonitor[]>('/monitoring/uptime');
      monitors = Array.isArray(res) ? res : (res.items ?? []);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingMonitors = false;
    }
  }

  function refreshAll() {
    loadMetrics();
    loadAlerts();
    loadMonitors();
  }

  onMount(() => {
    refreshAll();
  });

  function openCreate() {
    fName = '';
    fChannel = 'email';
    fTarget = '';
    fThreshold = 85;
    formError = '';
    createOpen = true;
  }

  async function submitCreate(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    saving = true;
    try {
      await api.post<AlertRule>('/monitoring/alerts', {
        name: fName.trim(),
        channel: fChannel,
        target: fTarget.trim(),
        threshold: Number(fThreshold),
      });
      toast.success(i18n.t.monitoring.alertCreated);
      createOpen = false;
      await loadAlerts();
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
      await api.delete(`/monitoring/alerts/${deleteTarget.id}`);
      toast.success(i18n.t.monitoring.alertDeleted);
      deleteTarget = null;
      await loadAlerts();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      deleteTarget = null;
    } finally {
      deleting = false;
    }
  }

  function openCreateMonitor() {
    mDomain = '';
    mUrl = 'https://';
    mExpectedStatus = 200;
    mIntervalSec = 300;
    mPublicStatus = true;
    monitorFormError = '';
    createMonitorOpen = true;
  }

  async function submitCreateMonitor(e: SubmitEvent) {
    e.preventDefault();
    monitorFormError = '';
    savingMonitor = true;
    try {
      await api.post<UptimeMonitor>('/monitoring/uptime', {
        domain: mDomain.trim(),
        url: mUrl.trim(),
        expected_status: Number(mExpectedStatus),
        interval_sec: Number(mIntervalSec),
        public_status: mPublicStatus,
      });
      toast.success(i18n.t.monitoring.monitorCreated);
      createMonitorOpen = false;
      await loadMonitors();
    } catch (err) {
      if (err instanceof ApiError) {
        const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
        monitorFormError = fieldMsg || err.detail || err.title;
      } else {
        monitorFormError = i18n.t.common.errorGeneric;
      }
    } finally {
      savingMonitor = false;
    }
  }

  async function checkMonitorNow(m: UptimeMonitor) {
    checkingId = m.id;
    try {
      await api.post(`/monitoring/uptime/${m.id}/check`, {});
      toast.success(i18n.t.monitoring.monitorChecked);
      await loadMonitors();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      checkingId = null;
    }
  }

  async function confirmDeleteMonitor() {
    if (!deleteMonitorTarget) return;
    deletingMonitor = true;
    try {
      await api.delete(`/monitoring/uptime/${deleteMonitorTarget.id}`);
      toast.success(i18n.t.monitoring.monitorDeleted);
      deleteMonitorTarget = null;
      await loadMonitors();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      deleteMonitorTarget = null;
    } finally {
      deletingMonitor = false;
    }
  }

  function formatUptimePct(m: UptimeMonitor): string {
    if (!m.total_checks) return '-';
    return `${((m.up_checks / m.total_checks) * 100).toFixed(1)}%`;
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.monitoring.title}</h1>
    <p class="page-subtitle">{i18n.t.monitoring.subtitle}</p>
  </div>
  <button type="button" class="btn btn-sm" onclick={refreshAll} disabled={loadingMetrics || loadingAlerts || loadingMonitors}>
    {i18n.t.common.refresh}
  </button>
</div>

{#if loadingMetrics && !metrics}
  <p>{i18n.t.common.loading}</p>
{:else if metrics}
  {@const cpu = clampPct(metrics.cpu_percent)}
  {@const mem = clampPct(metrics.memory_percent)}
  {@const disk = clampPct(metrics.disk_percent)}

  <div class="grid-cards">
    <div class="card">
      <div class="stat-label">{i18n.t.monitoring.cpuUsage}</div>
      <div class="stat-value">{metrics.cpu_percent.toFixed(1)}%</div>
      <progress
        class="score-progress"
        class:score-high={cpu < 60}
        class:score-medium={cpu >= 60 && cpu < 85}
        class:score-low={cpu >= 85}
        value={cpu}
        max="100"
      ></progress>
    </div>

    <div class="card">
      <div class="stat-label">{i18n.t.monitoring.memoryUsage}</div>
      <div class="stat-value">{metrics.memory_percent.toFixed(1)}%</div>
      <progress
        class="score-progress"
        class:score-high={mem < 60}
        class:score-medium={mem >= 60 && mem < 85}
        class:score-low={mem >= 85}
        value={mem}
        max="100"
      ></progress>
    </div>

    <div class="card">
      <div class="stat-label">{i18n.t.monitoring.diskUsage}</div>
      <div class="stat-value">{metrics.disk_percent.toFixed(1)}%</div>
      <progress
        class="score-progress"
        class:score-high={disk < 60}
        class:score-medium={disk >= 60 && disk < 85}
        class:score-low={disk >= 85}
        value={disk}
        max="100"
      ></progress>
    </div>
  </div>

  {#if metrics.timestamp}
    <p class="form-hint">
      {fmt(i18n.t.monitoring.lastUpdated, { time: formatDateTime(metrics.timestamp, localeCode) })}
    </p>
  {/if}

  <hr class="section-divider" />

  <h2 class="section-heading">{i18n.t.monitoring.servicesTitle}</h2>
  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.monitoring.serviceName}</th>
          <th>{i18n.t.monitoring.serviceStatus}</th>
        </tr>
      </thead>
      <tbody>
        {#if metrics.services.length === 0}
          <tr>
            <td colspan="2" class="empty-state">{i18n.t.monitoring.noServices}</td>
          </tr>
        {:else}
          {#each metrics.services as svc (svc.name)}
            <tr>
              <td class="mono">{svc.name}</td>
              <td>
                <span
                  class="badge"
                  class:badge-success={svc.status === 'active' || svc.status === 'running'}
                  class:badge-danger={svc.status !== 'active' && svc.status !== 'running'}
                >
                  {svc.status}
                </span>
              </td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
{/if}

<hr class="section-divider" />

<div class="toolbar">
  <h2 class="section-heading">{i18n.t.monitoring.uptimeTitle}</h2>
  <div class="toolbar-filters">
    <a href="#/status" class="btn btn-sm">{i18n.t.monitoring.statusPage}</a>
    <button type="button" class="btn btn-sm btn-primary" onclick={openCreateMonitor}>
      + {i18n.t.monitoring.addMonitor}
    </button>
  </div>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.monitoring.uptimeDomain}</th>
        <th>{i18n.t.monitoring.uptimeStatus}</th>
        <th>{i18n.t.monitoring.httpLatency}</th>
        <th>{i18n.t.monitoring.sslExpiry}</th>
        <th>{i18n.t.monitoring.domainExpiry}</th>
        <th>{i18n.t.monitoring.uptimePct}</th>
        <th>{i18n.t.common.actions}</th>
      </tr>
    </thead>
    <tbody>
      {#if loadingMonitors && monitors.length === 0}
        <tr>
          <td colspan="7" class="empty-state">{i18n.t.common.loading}</td>
        </tr>
      {:else if monitors.length === 0}
        <tr>
          <td colspan="7" class="empty-state">{i18n.t.monitoring.noMonitors}</td>
        </tr>
      {:else}
        {#each monitors as m (m.id)}
          <tr>
            <td>
              <div><strong>{m.domain}</strong></div>
              <div class="form-hint mono">{m.url}</div>
            </td>
            <td>
              <span
                class="badge"
                class:badge-success={m.status === 'up'}
                class:badge-warning={m.status === 'degraded'}
                class:badge-danger={m.status === 'down'}
                class:badge-info={m.status !== 'up' && m.status !== 'degraded' && m.status !== 'down'}
              >
                {m.status || i18n.t.monitoring.statusUnknown}
              </span>
            </td>
            <td class="mono">
              {m.last_http_status ? `${m.last_http_status} (${m.last_latency_ms}ms)` : '-'}
            </td>
            <td>
              {m.ssl_expires_at || m.ssl_days_left > 0
                ? fmt(i18n.t.monitoring.daysLeft, { days: m.ssl_days_left })
                : '-'}
            </td>
            <td>
              {m.domain_expires_at || m.domain_days_left > 0
                ? fmt(i18n.t.monitoring.daysLeft, { days: m.domain_days_left })
                : '-'}
            </td>
            <td>{formatUptimePct(m)}</td>
            <td>
              <div class="table-actions">
                <button
                  type="button"
                  class="btn btn-sm btn-icon"
                  title={i18n.t.monitoring.checkNow}
                  aria-label={i18n.t.monitoring.checkNow}
                  onclick={() => checkMonitorNow(m)}
                  disabled={checkingId === m.id}
                >
                  <ActionIcon name="test" />
                </button>
                <button
                  type="button"
                  class="btn btn-sm btn-icon btn-danger"
                  title={i18n.t.common.delete}
                  aria-label={i18n.t.common.delete}
                  onclick={() => (deleteMonitorTarget = m)}
                >
                  <ActionIcon name="delete" />
                </button>
              </div>
            </td>
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>

<hr class="section-divider" />

<div class="toolbar">
  <h2 class="section-heading">{i18n.t.monitoring.alertsTitle}</h2>
  <button type="button" class="btn btn-sm btn-primary" onclick={openCreate}>
    + {i18n.t.monitoring.createAlert}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.monitoring.alertName}</th>
        <th>{i18n.t.monitoring.channel}</th>
        <th>{i18n.t.monitoring.target}</th>
        <th>{i18n.t.monitoring.threshold}</th>
        <th>{i18n.t.common.actions}</th>
      </tr>
    </thead>
    <tbody>
      {#if loadingAlerts && alerts.length === 0}
        <tr>
          <td colspan="5" class="empty-state">{i18n.t.common.loading}</td>
        </tr>
      {:else if alerts.length === 0}
        <tr>
          <td colspan="5" class="empty-state">{i18n.t.monitoring.noAlerts}</td>
        </tr>
      {:else}
        {#each alerts as rule (rule.id)}
          <tr>
            <td><strong>{rule.name}</strong></td>
            <td><span class="badge badge-info">{rule.channel}</span></td>
            <td class="mono">{rule.target}</td>
            <td>{rule.threshold}%</td>
            <td>
              <button
                type="button"
                class="btn btn-sm btn-danger"
                onclick={() => (deleteTarget = rule)}
              >
                {i18n.t.common.delete}
              </button>
            </td>
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>

<Modal open={createOpen} title={i18n.t.monitoring.createAlert} onclose={() => (createOpen = false)}>
  <form onsubmit={submitCreate}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}
    <div class="form-group">
      <label class="form-label" for="alert-name">{i18n.t.monitoring.alertName}</label>
      <input
        id="alert-name"
        class="form-control"
        type="text"
        bind:value={fName}
        placeholder={i18n.t.monitoring.alertNamePlaceholder}
        required
      />
    </div>
    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="alert-channel">{i18n.t.monitoring.channel}</label>
        <select id="alert-channel" class="form-control" bind:value={fChannel}>
          <option value="email">email</option>
          <option value="telegram">telegram</option>
          <option value="slack">slack</option>
          <option value="webhook">webhook</option>
        </select>
      </div>
      <div class="form-group">
        <label class="form-label" for="alert-threshold">{i18n.t.monitoring.threshold}</label>
        <input
          id="alert-threshold"
          class="form-control"
          type="number"
          min="1"
          max="100"
          bind:value={fThreshold}
          required
        />
      </div>
    </div>
    <div class="form-group">
      <label class="form-label" for="alert-target">{i18n.t.monitoring.target}</label>
      <input
        id="alert-target"
        class="form-control mono"
        type="text"
        bind:value={fTarget}
        placeholder={i18n.t.monitoring.targetPlaceholder}
        required
      />
    </div>
    <div class="modal-footer">
      <button type="button" class="btn" onclick={() => (createOpen = false)}>
        {i18n.t.common.cancel}
      </button>
      <button type="submit" class="btn btn-primary" disabled={saving}>
        {saving ? i18n.t.common.loading : i18n.t.common.create}
      </button>
    </div>
  </form>
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.monitoring.deleteAlertTitle}
  onclose={() => (deleteTarget = null)}
>
  <p>{fmt(i18n.t.monitoring.deleteAlertBody, { name: deleteTarget?.name ?? '' })}</p>
  <div class="modal-footer">
    <button type="button" class="btn" onclick={() => (deleteTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button type="button" class="btn btn-danger" onclick={confirmDelete} disabled={deleting}>
      {deleting ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  </div>
</Modal>

<Modal
  open={createMonitorOpen}
  title={i18n.t.monitoring.addMonitor}
  onclose={() => (createMonitorOpen = false)}
>
  <form onsubmit={submitCreateMonitor}>
    {#if monitorFormError}
      <div class="alert alert-danger" role="alert">{monitorFormError}</div>
    {/if}
    <div class="form-group">
      <label class="form-label" for="monitor-domain">{i18n.t.monitoring.monitorDomain}</label>
      <input
        id="monitor-domain"
        class="form-control"
        type="text"
        bind:value={mDomain}
        placeholder={i18n.t.monitoring.monitorDomainPlaceholder}
        required
      />
    </div>
    <div class="form-group">
      <label class="form-label" for="monitor-url">{i18n.t.monitoring.monitorUrl}</label>
      <input
        id="monitor-url"
        class="form-control mono"
        type="url"
        bind:value={mUrl}
        placeholder={i18n.t.monitoring.monitorUrlPlaceholder}
        required
      />
    </div>
    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="monitor-status">{i18n.t.monitoring.expectedStatus}</label>
        <input
          id="monitor-status"
          class="form-control"
          type="number"
          min="100"
          max="599"
          bind:value={mExpectedStatus}
          required
        />
      </div>
      <div class="form-group">
        <label class="form-label" for="monitor-interval">{i18n.t.monitoring.intervalSec}</label>
        <input
          id="monitor-interval"
          class="form-control"
          type="number"
          min="30"
          max="86400"
          bind:value={mIntervalSec}
          required
        />
      </div>
    </div>
    <div class="form-group">
      <label class="checkbox-row">
        <input type="checkbox" bind:checked={mPublicStatus} />
        <span>{i18n.t.monitoring.publicStatus}</span>
      </label>
    </div>
    <div class="modal-footer">
      <button type="button" class="btn" onclick={() => (createMonitorOpen = false)}>
        {i18n.t.common.cancel}
      </button>
      <button type="submit" class="btn btn-primary" disabled={savingMonitor}>
        {savingMonitor ? i18n.t.common.loading : i18n.t.common.create}
      </button>
    </div>
  </form>
</Modal>

<Modal
  open={deleteMonitorTarget !== null}
  title={i18n.t.monitoring.deleteMonitorTitle}
  onclose={() => (deleteMonitorTarget = null)}
>
  <p>{fmt(i18n.t.monitoring.deleteMonitorBody, { domain: deleteMonitorTarget?.domain ?? '' })}</p>
  <div class="modal-footer">
    <button type="button" class="btn" onclick={() => (deleteMonitorTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-danger"
      onclick={confirmDeleteMonitor}
      disabled={deletingMonitor}
    >
      {deletingMonitor ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  </div>
</Modal>

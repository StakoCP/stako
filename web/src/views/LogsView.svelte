<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, ApiError } from '../lib/api';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type {
    CentralLogEntry,
    CentralLogsResponse,
    LogRotationConfig,
    LogShippingConfig,
  } from '../lib/types';

  type TabId = 'viewer' | 'rotation' | 'shipping';

  let activeTab = $state<TabId>('viewer');

  // Tab 1: Log Viewer
  let entries = $state<CentralLogEntry[]>([]);
  let sources = $state<string[]>([]);
  let total = $state(0);
  let loadingLogs = $state(false);
  let filterService = $state('all');
  let filterLevel = $state('all');
  let filterSource = $state('');
  let filterQuery = $state('');
  let filterLines = $state(200);
  let autoRefresh = $state(false);
  let refreshTimer: number | null = null;

  // Tab 2: Log Rotation
  let rotation = $state<LogRotationConfig>({
    frequency: 'daily',
    rotate: 14,
    maxsize: '100M',
    compress: true,
  });
  let loadingRotation = $state(false);
  let savingRotation = $state(false);
  let rotatingNow = $state(false);
  let rotateOutput = $state('');

  // Tab 3: Log Shipping
  let shipping = $state<LogShippingConfig>({
    syslog_enabled: false,
    syslog_host: '',
    syslog_port: 514,
    syslog_protocol: 'udp',
    syslog_format: 'rfc5424',
    loki_enabled: false,
    loki_url: '',
    loki_tenant_id: '',
    loki_username: '',
    loki_password: '',
    has_loki_password: false,
    loki_labels: 'job=stako',
    loki_tls_skip_verify: false,
  });
  let loadingShipping = $state(false);
  let savingShipping = $state(false);
  let testingTarget = $state<'syslog' | 'loki' | null>(null);

  function errMessage(err: unknown): string {
    return err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
  }

  function levelBadgeClass(level: string): string {
    const l = level.toLowerCase();
    if (l === 'error' || l === 'err' || l === 'fatal' || l === 'crit') return 'badge-danger';
    if (l === 'warn' || l === 'warning') return 'badge-warning';
    if (l === 'info') return 'badge-info';
    return 'badge-neutral';
  }

  async function loadLogs() {
    loadingLogs = true;
    try {
      const qs = new URLSearchParams({
        service: filterService,
        level: filterLevel,
        lines: String(filterLines),
      });
      if (filterSource) qs.set('source', filterSource);
      if (filterQuery.trim()) qs.set('q', filterQuery.trim());
      const res = await api.get<CentralLogsResponse>(`/logs?${qs.toString()}`);
      entries = res.entries ?? [];
      sources = res.sources ?? [];
      total = res.total ?? entries.length;
    } catch (err) {
      toast.error(errMessage(err));
    } finally {
      loadingLogs = false;
    }
  }

  function handleViewerSearch(e: SubmitEvent) {
    e.preventDefault();
    loadLogs();
  }

  function downloadLogs() {
    const content = entries
      .map(
        (e) =>
          `${e.timestamp} [${e.level.toUpperCase()}] [${e.service}] (${e.source}) ${e.message}`
      )
      .join('\n');
    const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `stako-logs-${filterService}-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}.log`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  }

  function stopAutoRefresh() {
    if (refreshTimer !== null && typeof window !== 'undefined') {
      window.clearInterval(refreshTimer);
      refreshTimer = null;
    }
  }

  $effect(() => {
    stopAutoRefresh();
    if (autoRefresh && activeTab === 'viewer' && typeof window !== 'undefined') {
      refreshTimer = window.setInterval(() => {
        if (!loadingLogs) {
          loadLogs();
        }
      }, 5000);
    }
    return () => stopAutoRefresh();
  });

  async function loadRotation() {
    loadingRotation = true;
    try {
      rotation = await api.get<LogRotationConfig>('/logs/rotation');
    } catch (err) {
      toast.error(errMessage(err));
    } finally {
      loadingRotation = false;
    }
  }

  async function saveRotation(e: SubmitEvent) {
    e.preventDefault();
    savingRotation = true;
    try {
      const saved = await api.put<LogRotationConfig>('/logs/rotation', {
        frequency: rotation.frequency,
        rotate: Number(rotation.rotate),
        maxsize: rotation.maxsize.trim(),
        compress: rotation.compress,
      });
      rotation = saved;
      toast.success(i18n.t.logs.rotationSaved);
    } catch (err) {
      toast.error(errMessage(err));
    } finally {
      savingRotation = false;
    }
  }

  async function triggerRotateNow() {
    rotatingNow = true;
    rotateOutput = '';
    try {
      const res = await api.post<{ ok?: boolean; output?: string; message?: string }>(
        '/logs/rotate'
      );
      rotateOutput = res?.output || res?.message || '';
      toast.success(i18n.t.logs.rotationTriggered);
    } catch (err) {
      toast.error(errMessage(err));
    } finally {
      rotatingNow = false;
    }
  }

  async function loadShipping() {
    loadingShipping = true;
    try {
      const res = await api.get<LogShippingConfig>('/logs/shipping');
      shipping = {
        ...res,
        loki_password: '',
      };
    } catch (err) {
      toast.error(errMessage(err));
    } finally {
      loadingShipping = false;
    }
  }

  function buildShippingPayload(): Record<string, unknown> {
    const payload: Record<string, unknown> = {
      syslog_enabled: shipping.syslog_enabled,
      syslog_host: shipping.syslog_host.trim(),
      syslog_port: Number(shipping.syslog_port),
      syslog_protocol: shipping.syslog_protocol,
      syslog_format: shipping.syslog_format,
      loki_enabled: shipping.loki_enabled,
      loki_url: shipping.loki_url.trim(),
      loki_tenant_id: shipping.loki_tenant_id.trim(),
      loki_username: shipping.loki_username.trim(),
      loki_labels: shipping.loki_labels.trim(),
      loki_tls_skip_verify: shipping.loki_tls_skip_verify,
    };
    if (shipping.loki_password && shipping.loki_password.length > 0) {
      payload.loki_password = shipping.loki_password;
    }
    return payload;
  }

  async function saveShipping(e: SubmitEvent) {
    e.preventDefault();
    savingShipping = true;
    try {
      const res = await api.put<LogShippingConfig>('/logs/shipping', buildShippingPayload());
      shipping = {
        ...res,
        loki_password: '',
      };
      toast.success(i18n.t.logs.shippingSaved);
    } catch (err) {
      toast.error(errMessage(err));
    } finally {
      savingShipping = false;
    }
  }

  async function testShipping(target: 'syslog' | 'loki') {
    testingTarget = target;
    try {
      await api.post('/logs/shipping/test', {
        target,
        ...buildShippingPayload(),
      });
      toast.success(i18n.t.logs.testSuccess);
    } catch (err) {
      toast.error(errMessage(err));
    } finally {
      testingTarget = null;
    }
  }

  function switchTab(tab: TabId) {
    activeTab = tab;
    if (tab === 'rotation') {
      loadRotation();
    } else if (tab === 'shipping') {
      loadShipping();
    }
  }

  onMount(() => {
    loadLogs();
  });

  onDestroy(() => {
    stopAutoRefresh();
  });
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.logs.title}</h1>
    <p class="page-subtitle">{i18n.t.logs.subtitle}</p>
  </div>
</div>

<div class="tabs" role="tablist">
  <button
    type="button"
    role="tab"
    class="tab-btn"
    class:active={activeTab === 'viewer'}
    aria-selected={activeTab === 'viewer'}
    onclick={() => switchTab('viewer')}
  >
    {i18n.t.logs.tabViewer}
  </button>
  <button
    type="button"
    role="tab"
    class="tab-btn"
    class:active={activeTab === 'rotation'}
    aria-selected={activeTab === 'rotation'}
    onclick={() => switchTab('rotation')}
  >
    {i18n.t.logs.tabRotation}
  </button>
  <button
    type="button"
    role="tab"
    class="tab-btn"
    class:active={activeTab === 'shipping'}
    aria-selected={activeTab === 'shipping'}
    onclick={() => switchTab('shipping')}
  >
    {i18n.t.logs.tabShipping}
  </button>
</div>

{#if activeTab === 'viewer'}
  <div class="toolbar">
    <form class="toolbar-filters" onsubmit={handleViewerSearch}>
      <select
        class="form-control"
        bind:value={filterService}
        onchange={loadLogs}
        aria-label={i18n.t.logs.service}
      >
        <option value="all">{i18n.t.logs.serviceAll}</option>
        <option value="web">{i18n.t.logs.serviceWeb}</option>
        <option value="mail">{i18n.t.logs.serviceMail}</option>
        <option value="auth">{i18n.t.logs.serviceAuth}</option>
        <option value="container">{i18n.t.logs.serviceContainer}</option>
        <option value="panel">{i18n.t.logs.servicePanel}</option>
      </select>

      <select
        class="form-control"
        bind:value={filterLevel}
        onchange={loadLogs}
        aria-label={i18n.t.logs.level}
      >
        <option value="all">{i18n.t.logs.levelAll}</option>
        <option value="error">{i18n.t.logs.levelError}</option>
        <option value="warn">{i18n.t.logs.levelWarn}</option>
        <option value="info">{i18n.t.logs.levelInfo}</option>
      </select>

      <select
        class="form-control"
        bind:value={filterSource}
        onchange={loadLogs}
        aria-label={i18n.t.logs.source}
      >
        <option value="">{i18n.t.logs.sourceAll}</option>
        {#each sources as src (src)}
          <option value={src}>{src}</option>
        {/each}
      </select>

      <select
        class="form-control"
        bind:value={filterLines}
        onchange={loadLogs}
        aria-label={i18n.t.logs.linesLabel}
      >
        <option value={100}>100 {i18n.t.logs.linesLabel}</option>
        <option value={200}>200 {i18n.t.logs.linesLabel}</option>
        <option value={500}>500 {i18n.t.logs.linesLabel}</option>
        <option value={1000}>1000 {i18n.t.logs.linesLabel}</option>
      </select>

      <input
        type="text"
        class="form-control"
        placeholder={i18n.t.logs.searchPlaceholder}
        bind:value={filterQuery}
        aria-label={i18n.t.logs.searchPlaceholder}
      />

      <button type="submit" class="btn">{i18n.t.common.search}</button>
    </form>

    <div class="checkbox-group">
      <label class="checkbox-row">
        <input type="checkbox" bind:checked={autoRefresh} />
        <span>{i18n.t.logs.autoRefresh}</span>
      </label>
      <button type="button" class="btn btn-sm" onclick={loadLogs} disabled={loadingLogs}>
        {i18n.t.logs.refresh}
      </button>
      <button
        type="button"
        class="btn btn-sm"
        onclick={downloadLogs}
        disabled={entries.length === 0}
      >
        {i18n.t.logs.download}
      </button>
    </div>
  </div>

  <p class="form-hint">
    {fmt(i18n.t.logs.totalEntries, { count: entries.length, total })}
  </p>

  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.logs.timestamp}</th>
          <th>{i18n.t.logs.level}</th>
          <th>{i18n.t.logs.service}</th>
          <th>{i18n.t.logs.source}</th>
          <th>{i18n.t.logs.message}</th>
        </tr>
      </thead>
      <tbody>
        {#if loadingLogs && entries.length === 0}
          <tr><td colspan="5" class="empty-state">{i18n.t.common.loading}</td></tr>
        {:else if entries.length === 0}
          <tr><td colspan="5" class="empty-state">{i18n.t.logs.empty}</td></tr>
        {:else}
          {#each entries as entry, idx (`${entry.timestamp}-${entry.source}-${idx}`)}
            <tr>
              <td class="mono">{entry.timestamp}</td>
              <td>
                <span class="badge {levelBadgeClass(entry.level)}">{entry.level}</span>
              </td>
              <td>
                <span class="badge badge-neutral">{entry.service}</span>
              </td>
              <td class="mono">{entry.source}</td>
              <td class="mono">{entry.message}</td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
{:else if activeTab === 'rotation'}
  <div class="card">
    <div class="card-header-row">
      <div>
        <h2 class="card-title">{i18n.t.logs.rotationTitle}</h2>
        <p class="form-hint">{i18n.t.logs.rotationHint}</p>
      </div>
      <button
        type="button"
        class="btn btn-sm"
        onclick={triggerRotateNow}
        disabled={rotatingNow || loadingRotation}
      >
        {rotatingNow ? i18n.t.logs.rotating : i18n.t.logs.rotateNow}
      </button>
    </div>

    {#if loadingRotation}
      <p>{i18n.t.common.loading}</p>
    {:else}
      <form onsubmit={saveRotation}>
        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="rot-frequency">{i18n.t.logs.frequency}</label>
            <select id="rot-frequency" class="form-control" bind:value={rotation.frequency}>
              <option value="daily">{i18n.t.logs.frequencyDaily}</option>
              <option value="weekly">{i18n.t.logs.frequencyWeekly}</option>
            </select>
          </div>

          <div class="form-group">
            <label class="form-label" for="rot-count">{i18n.t.logs.rotateCount}</label>
            <input
              id="rot-count"
              type="number"
              min="1"
              max="365"
              class="form-control"
              bind:value={rotation.rotate}
              required
            />
          </div>

          <div class="form-group">
            <label class="form-label" for="rot-maxsize">{i18n.t.logs.maxSize}</label>
            <input
              id="rot-maxsize"
              type="text"
              class="form-control"
              bind:value={rotation.maxsize}
              placeholder="100M"
              required
            />
            <p class="form-hint">{i18n.t.logs.maxSizeHint}</p>
          </div>
        </div>

        <div class="form-group">
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={rotation.compress} />
            <span>{i18n.t.logs.compress}</span>
          </label>
        </div>

        <button type="submit" class="btn btn-primary" disabled={savingRotation}>
          {savingRotation ? i18n.t.common.saving : i18n.t.logs.saveRotation}
        </button>
      </form>
    {/if}
  </div>

  {#if rotateOutput}
    <div class="card">
      <h3 class="card-title">{i18n.t.logs.rotationOutput}</h3>
      <pre class="log-box">{rotateOutput}</pre>
    </div>
  {/if}
{:else}
  {#if loadingShipping}
    <p>{i18n.t.common.loading}</p>
  {:else}
    <form onsubmit={saveShipping}>
      <div class="card">
        <div class="card-header-row">
          <h2 class="card-title">{i18n.t.logs.syslogTitle}</h2>
          <button
            type="button"
            class="btn btn-sm"
            onclick={() => testShipping('syslog')}
            disabled={testingTarget !== null}
          >
            {testingTarget === 'syslog' ? i18n.t.logs.testing : i18n.t.logs.testSyslog}
          </button>
        </div>

        <div class="form-group">
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={shipping.syslog_enabled} />
            <span>{i18n.t.logs.syslogEnabled}</span>
          </label>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="syslog-host">{i18n.t.logs.syslogHost}</label>
            <input
              id="syslog-host"
              type="text"
              class="form-control"
              bind:value={shipping.syslog_host}
              placeholder="logs.example.com"
            />
          </div>

          <div class="form-group">
            <label class="form-label" for="syslog-port">{i18n.t.logs.syslogPort}</label>
            <input
              id="syslog-port"
              type="number"
              min="1"
              max="65535"
              class="form-control"
              bind:value={shipping.syslog_port}
            />
          </div>

          <div class="form-group">
            <label class="form-label" for="syslog-proto">{i18n.t.logs.syslogProtocol}</label>
            <select id="syslog-proto" class="form-control" bind:value={shipping.syslog_protocol}>
              <option value="udp">UDP</option>
              <option value="tcp">TCP</option>
              <option value="tls">TLS</option>
            </select>
          </div>

          <div class="form-group">
            <label class="form-label" for="syslog-format">{i18n.t.logs.syslogFormat}</label>
            <select id="syslog-format" class="form-control" bind:value={shipping.syslog_format}>
              <option value="rfc5424">RFC 5424</option>
              <option value="rfc3164">RFC 3164</option>
            </select>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-header-row">
          <h2 class="card-title">{i18n.t.logs.lokiTitle}</h2>
          <button
            type="button"
            class="btn btn-sm"
            onclick={() => testShipping('loki')}
            disabled={testingTarget !== null}
          >
            {testingTarget === 'loki' ? i18n.t.logs.testing : i18n.t.logs.testLoki}
          </button>
        </div>

        <div class="form-group">
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={shipping.loki_enabled} />
            <span>{i18n.t.logs.lokiEnabled}</span>
          </label>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="loki-url">{i18n.t.logs.lokiUrl}</label>
            <input
              id="loki-url"
              type="url"
              class="form-control"
              bind:value={shipping.loki_url}
              placeholder="https://loki.example.com/loki/api/v1/push"
            />
          </div>

          <div class="form-group">
            <label class="form-label" for="loki-tenant">{i18n.t.logs.lokiTenantId}</label>
            <input
              id="loki-tenant"
              type="text"
              class="form-control"
              bind:value={shipping.loki_tenant_id}
            />
          </div>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="loki-user">{i18n.t.logs.lokiUsername}</label>
            <input
              id="loki-user"
              type="text"
              class="form-control"
              bind:value={shipping.loki_username}
            />
          </div>

          <div class="form-group">
            <label class="form-label" for="loki-pass">{i18n.t.logs.lokiPassword}</label>
            <input
              id="loki-pass"
              type="password"
              class="form-control"
              bind:value={shipping.loki_password}
              autocomplete="new-password"
            />
            {#if shipping.has_loki_password}
              <p class="form-hint">{i18n.t.logs.lokiPasswordSetHint}</p>
            {/if}
          </div>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="loki-labels">{i18n.t.logs.lokiLabels}</label>
            <input
              id="loki-labels"
              type="text"
              class="form-control"
              bind:value={shipping.loki_labels}
              placeholder="job=stako,env=prod"
            />
            <p class="form-hint">{i18n.t.logs.lokiLabelsHint}</p>
          </div>
        </div>

        <div class="form-group">
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={shipping.loki_tls_skip_verify} />
            <span>{i18n.t.logs.lokiTlsSkipVerify}</span>
          </label>
        </div>

        <button type="submit" class="btn btn-primary" disabled={savingShipping}>
          {savingShipping ? i18n.t.common.saving : i18n.t.logs.saveShipping}
        </button>
      </div>
    </form>
  {/if}
{/if}

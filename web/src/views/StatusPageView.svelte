<script lang="ts">
  import { onMount } from 'svelte';
  import { auth } from '../lib/auth.svelte';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import type { PublicStatusResponse } from '../lib/types';

  let data = $state<PublicStatusResponse | null>(null);
  let loading = $state(true);
  let error = $state('');

  let localeCode = $derived(i18n.locale === 'id' ? 'id-ID' : 'en-US');

  async function loadStatus() {
    loading = true;
    error = '';
    try {
      const res = await fetch('/api/v1/status', { credentials: 'same-origin' });
      if (!res.ok) {
        error = i18n.t.statusPage.loadError;
        return;
      }
      data = (await res.json()) as PublicStatusResponse;
    } catch {
      error = i18n.t.statusPage.loadError;
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    loadStatus();
  });

  function bannerClass(status: string): string {
    if (status === 'operational') return 'alert alert-success';
    if (status === 'degraded') return 'alert alert-warning';
    if (status === 'outage') return 'alert alert-danger';
    return 'alert alert-info';
  }

  function bannerText(status: string): string {
    if (status === 'operational') return i18n.t.statusPage.operational;
    if (status === 'degraded') return i18n.t.statusPage.degraded;
    if (status === 'outage') return i18n.t.statusPage.outage;
    return i18n.t.statusPage.unknown;
  }
</script>

<div class="content">
  <div class="page-header">
    <div>
      <h1 class="page-title">{i18n.t.statusPage.title}</h1>
      <p class="page-subtitle">{i18n.t.statusPage.subtitle}</p>
    </div>
    <div class="toolbar-filters">
      <button type="button" class="btn btn-sm" onclick={loadStatus} disabled={loading}>
        {i18n.t.common.refresh}
      </button>
      {#if auth.user}
        <a href="#/monitoring" class="btn btn-sm btn-primary">{i18n.t.statusPage.backToPanel}</a>
      {:else}
        <a href="#/login" class="btn btn-sm btn-primary">{i18n.t.statusPage.signIn}</a>
      {/if}
    </div>
  </div>

  {#if error}
    <div class="alert alert-danger" role="alert">{error}</div>
  {:else if loading && !data}
    <p>{i18n.t.common.loading}</p>
  {:else if data}
    <div class={bannerClass(data.overall_status)} role="status">
      <strong>{bannerText(data.overall_status)}</strong>
    </div>

    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{i18n.t.statusPage.domain}</th>
            <th>{i18n.t.statusPage.status}</th>
            <th>{i18n.t.statusPage.httpLatency}</th>
            <th>{i18n.t.statusPage.sslExpiry}</th>
            <th>{i18n.t.statusPage.domainExpiry}</th>
            <th>{i18n.t.statusPage.uptimePct}</th>
          </tr>
        </thead>
        <tbody>
          {#if data.monitors.length === 0}
            <tr>
              <td colspan="6" class="empty-state">{i18n.t.statusPage.noMonitors}</td>
            </tr>
          {:else}
            {#each data.monitors as m (m.id)}
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
                    {m.status}
                  </span>
                </td>
                <td class="mono">
                  {m.last_http_status ? `${m.last_http_status} (${m.last_latency_ms}ms)` : '-'}
                </td>
                <td>
                  {m.ssl_expires_at || m.ssl_days_left > 0
                    ? fmt(i18n.t.statusPage.daysLeft, { days: m.ssl_days_left })
                    : '-'}
                </td>
                <td>
                  {m.domain_expires_at || m.domain_days_left > 0
                    ? fmt(i18n.t.statusPage.daysLeft, { days: m.domain_days_left })
                    : '-'}
                </td>
                <td>{m.uptime_pct.toFixed(2)}%</td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>

    {#if data.checked_at}
      <p class="form-hint">
        {fmt(i18n.t.statusPage.lastChecked, { time: formatDateTime(data.checked_at, localeCode) })}
      </p>
    {/if}
  {/if}
</div>

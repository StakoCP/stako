<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatBytes, formatDateTime, formatQuota, formatUptime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import type {
    AuditEntry,
    Job,
    MonitoringMetrics,
    Package,
    SecurityFindingsResponse,
    SystemInfo,
  } from '../lib/types';

  let sysInfo = $state<SystemInfo | null>(null);
  let myPackage = $state<Package | null>(null);
  let metrics = $state<MonitoringMetrics | null>(null);
  let securityScore = $state<number | null>(null);
  let domainCount = $state(0);
  let appCount = $state(0);
  let containerCount = $state(0);
  let dbCount = $state(0);
  let recentJobs = $state<Job[]>([]);
  let recentAudit = $state<AuditEntry[]>([]);
  let loading = $state(true);
  let error = $state('');

  let isAdmin = $derived(auth.user?.role === 'admin');
  let canWrite = $derived(auth.user?.role !== 'readonly');
  let localeCode = $derived(i18n.locale === 'id' ? 'id-ID' : 'en-US');

  async function load() {
    loading = true;
    error = '';
    try {
      const [domRes, appRes, cntRes, dbRes, jobRes] = await Promise.allSettled([
        api.get<{ total: number }>('/domains?limit=1'),
        api.get<{ total: number }>('/apps?limit=1'),
        api.get<{ total: number }>('/docker/containers?limit=1'),
        api.get<{ total: number }>('/databases?limit=1'),
        api.get<{ items: Job[] }>('/jobs?limit=5'),
      ]);

      if (domRes.status === 'fulfilled') domainCount = domRes.value.total;
      if (appRes.status === 'fulfilled') appCount = appRes.value.total;
      if (cntRes.status === 'fulfilled') containerCount = cntRes.value.total;
      if (dbRes.status === 'fulfilled') dbCount = dbRes.value.total;
      if (jobRes.status === 'fulfilled') recentJobs = jobRes.value.items;

      if (isAdmin) {
        const [sysRes, metRes, secRes, audRes] = await Promise.allSettled([
          api.get<SystemInfo>('/system/info'),
          api.get<MonitoringMetrics>('/monitoring/metrics'),
          api.get<SecurityFindingsResponse>('/security/findings'),
          api.get<{ items: AuditEntry[] }>('/audit?limit=5'),
        ]);
        if (sysRes.status === 'fulfilled') sysInfo = sysRes.value;
        else if (sysRes.reason instanceof ApiError) error = sysRes.reason.detail || sysRes.reason.title;
        if (metRes.status === 'fulfilled') metrics = metRes.value;
        if (secRes.status === 'fulfilled') securityScore = secRes.value.score;
        if (audRes.status === 'fulfilled') recentAudit = audRes.value.items;
      } else if (auth.user?.package_id) {
        myPackage = await api.get<Package>(`/packages/${auth.user.package_id}`);
      }
    } catch (err) {
      error = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      loading = false;
    }
  }

  onMount(load);

  let memUsed = $derived(
    sysInfo && sysInfo.memory_total_bytes !== null && sysInfo.memory_available_bytes !== null
      ? Math.max(0, sysInfo.memory_total_bytes - sysInfo.memory_available_bytes)
      : null
  );
  let memPct = $derived(
    sysInfo && sysInfo.memory_total_bytes && memUsed !== null
      ? Math.min(100, Math.round((memUsed / sysInfo.memory_total_bytes) * 100))
      : metrics
        ? Math.round(metrics.memory_percent)
        : 0
  );

  function jobBadgeClass(status: Job['status']): string {
    if (status === 'succeeded') return 'badge-success';
    if (status === 'failed') return 'badge-danger';
    if (status === 'running') return 'badge-primary';
    return 'badge-warning';
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.dashboard.title}</h1>
    {#if auth.user}
      <p class="page-subtitle">{auth.user.username} · {i18n.t.roles[auth.user.role]}</p>
    {/if}
  </div>
  <button type="button" class="btn btn-sm" onclick={load} disabled={loading}>
    {i18n.t.common.refresh}
  </button>
</div>

{#if error}
  <div class="alert alert-danger" role="alert">{error}</div>
{/if}

{#if canWrite}
  <div class="card dash-section">
    <div class="card-header-row">
      <h2 class="card-title">{i18n.t.dashboard.quickActions}</h2>
    </div>
    <div class="quick-actions">
      <a href="#/domains" class="btn btn-sm btn-primary">{i18n.t.dashboard.newDomain}</a>
      <a href="#/apps" class="btn btn-sm">{i18n.t.dashboard.newApp}</a>
      <a href="#/docker" class="btn btn-sm">{i18n.t.dashboard.newContainer}</a>
      <a href="#/databases" class="btn btn-sm">{i18n.t.dashboard.newDatabase}</a>
      <a href="#/catalog" class="btn btn-sm">{i18n.t.dashboard.appInstaller}</a>
      <a href="#/files" class="btn btn-sm">{i18n.t.dashboard.fileManager}</a>
      {#if isAdmin}
        <a href="#/security" class="btn btn-sm">{i18n.t.dashboard.runScan}</a>
        <a href="#/configs" class="btn btn-sm">{i18n.t.dashboard.sysConfigs}</a>
      {/if}
    </div>
  </div>
{/if}

{#if loading}
  <p class="dash-section">{i18n.t.common.loading}</p>
{:else if isAdmin && sysInfo}
  <div class="grid-cards dash-section">
    <div class="card">
      <div class="stat-label">{i18n.t.dashboard.countsUsers}</div>
      <div class="stat-value">{sysInfo.counts.users}</div>
    </div>
    <div class="card">
      <div class="stat-label">{i18n.t.dashboard.countsDomains}</div>
      <div class="stat-value">{domainCount}</div>
    </div>
    <div class="card">
      <div class="stat-label">{i18n.t.dashboard.countsApps}</div>
      <div class="stat-value">{appCount}</div>
    </div>
    <div class="card">
      <div class="stat-label">{i18n.t.dashboard.countsContainers}</div>
      <div class="stat-value">{containerCount}</div>
    </div>
    <div class="card">
      <div class="stat-label">{i18n.t.dashboard.countsDatabases}</div>
      <div class="stat-value">{dbCount}</div>
    </div>
    <div class="card">
      <div class="stat-label">{i18n.t.dashboard.securityScore}</div>
      <div class="stat-value">{securityScore !== null ? `${securityScore}/100` : '—'}</div>
    </div>
    <div class="card">
      <div class="stat-label">{i18n.t.dashboard.countsJobs}</div>
      <div class="stat-value">{sysInfo.counts.jobs_running}</div>
    </div>
    <div class="card">
      <div class="stat-label">{i18n.t.dashboard.agentStatus}</div>
      <div class="stat-value">
        <span class="badge" class:badge-success={sysInfo.agent.connected} class:badge-danger={!sysInfo.agent.connected}>
          {sysInfo.agent.connected ? i18n.t.dashboard.connected : i18n.t.dashboard.disconnected}
        </span>
      </div>
      <div class="stat-sub">
        v{sysInfo.agent.version}
        {#if sysInfo.agent.dry_run}
          · <span class="badge badge-warning">{i18n.t.dashboard.dryRun}</span>
        {/if}
      </div>
    </div>
  </div>

  <div class="grid-two dash-section">
    <div class="card">
      <h2 class="card-title">{i18n.t.dashboard.serverHealth}</h2>
      <div class="health-bars">
        {#if metrics}
          <div class="health-bar-item">
            <div class="health-bar-head">
              <span>{i18n.t.dashboard.cpuUsage}</span>
              <span class="mono">{metrics.cpu_percent.toFixed(1)}%</span>
            </div>
            <progress class="score-progress" value={metrics.cpu_percent} max="100"></progress>
          </div>
        {/if}

        <div class="health-bar-item">
          <div class="health-bar-head">
            <span>{i18n.t.dashboard.memory}</span>
            <span class="mono">
              {#if sysInfo.memory_total_bytes !== null && memUsed !== null}
                {memPct}% ({fmt(i18n.t.dashboard.memoryDetail, { used: formatBytes(memUsed), total: formatBytes(sysInfo.memory_total_bytes) })})
              {:else}
                {memPct}%
              {/if}
            </span>
          </div>
          <progress class="score-progress" value={memPct} max="100"></progress>
        </div>

        {#if metrics}
          <div class="health-bar-item">
            <div class="health-bar-head">
              <span>{i18n.t.dashboard.diskUsage}</span>
              <span class="mono">{metrics.disk_percent.toFixed(1)}%</span>
            </div>
            <progress class="score-progress" value={metrics.disk_percent} max="100"></progress>
          </div>
        {/if}
      </div>

      {#if metrics && metrics.services.length > 0}
        <div class="service-pill-grid" aria-label={i18n.t.dashboard.serviceStatus}>
          {#each metrics.services as svc (svc.name)}
            <div class="service-pill">
              <span class="mono">{svc.name}</span>
              <span class="badge" class:badge-success={svc.status === 'active'} class:badge-danger={svc.status !== 'active'}>
                {svc.status}
              </span>
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <div class="card">
      <h2 class="card-title">{i18n.t.dashboard.systemInfo}</h2>
      <dl class="kv-list">
        <dt>{i18n.t.dashboard.hostname}</dt>
        <dd class="mono">{sysInfo.hostname}</dd>
        <dt>{i18n.t.dashboard.os}</dt>
        <dd>{sysInfo.os}</dd>
        <dt>{i18n.t.dashboard.kernel}</dt>
        <dd class="mono">{sysInfo.kernel ?? '—'}</dd>
        <dt>{i18n.t.dashboard.version}</dt>
        <dd class="mono">{sysInfo.version}</dd>
        <dt>{i18n.t.dashboard.uptime}</dt>
        <dd>{formatUptime(sysInfo.uptime_seconds)}</dd>
        <dt>{i18n.t.dashboard.cpuCores}</dt>
        <dd>{sysInfo.cpu_cores}</dd>
        <dt>{i18n.t.dashboard.loadAvg}</dt>
        <dd class="mono">
          {sysInfo.load_avg ? sysInfo.load_avg.map((n) => n.toFixed(2)).join(' / ') : '—'}
        </dd>
      </dl>
    </div>
  </div>

  <div class="grid-two dash-section">
    <div class="card">
      <div class="card-header-row">
        <h2 class="card-title">{i18n.t.dashboard.recentJobs}</h2>
        <a href="#/jobs" class="btn btn-sm">{i18n.t.dashboard.viewAll}</a>
      </div>
      {#if recentJobs.length === 0}
        <p class="page-subtitle">{i18n.t.dashboard.noData}</p>
      {:else}
        <div class="mini-list">
          {#each recentJobs as j (j.id)}
            <a href={`#/jobs/${j.id}`} class="mini-list-item">
              <div class="mini-list-main">
                <span class="mono">{j.type}</span>
                <span class="mini-list-meta">{formatDateTime(j.created_at, localeCode)}</span>
              </div>
              <span class={`badge ${jobBadgeClass(j.status)}`}>{j.status}</span>
            </a>
          {/each}
        </div>
      {/if}
    </div>

    <div class="card">
      <div class="card-header-row">
        <h2 class="card-title">{i18n.t.dashboard.recentAudit}</h2>
        <a href="#/audit" class="btn btn-sm">{i18n.t.dashboard.viewAll}</a>
      </div>
      {#if recentAudit.length === 0}
        <p class="page-subtitle">{i18n.t.dashboard.noData}</p>
      {:else}
        <div class="mini-list">
          {#each recentAudit as a (a.id)}
            <div class="mini-list-item">
              <div class="mini-list-main">
                <span><strong>{a.actor_username}</strong> · <span class="mono">{a.action}</span></span>
                <span class="mini-list-meta">{a.ip} · {formatDateTime(a.at, localeCode)}</span>
              </div>
              <span class="badge" class:badge-success={a.result === 'success'} class:badge-danger={a.result !== 'success'}>
                {a.result}
              </span>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>
{:else if auth.user}
  <div class="grid-cards dash-section">
    <div class="card">
      <div class="stat-label">{i18n.t.dashboard.countsDomains}</div>
      <div class="stat-value">{domainCount}</div>
    </div>
    <div class="card">
      <div class="stat-label">{i18n.t.dashboard.countsApps}</div>
      <div class="stat-value">{appCount}</div>
    </div>
    <div class="card">
      <div class="stat-label">{i18n.t.dashboard.countsContainers}</div>
      <div class="stat-value">{containerCount}</div>
    </div>
    <div class="card">
      <div class="stat-label">{i18n.t.dashboard.countsDatabases}</div>
      <div class="stat-value">{dbCount}</div>
    </div>
  </div>

  <div class="grid-two dash-section">
    <div class="card">
      <h2 class="card-title">{i18n.t.dashboard.myProfile}</h2>
      <dl class="kv-list">
        <dt>{i18n.t.users.username}</dt>
        <dd class="mono">{auth.user.username}</dd>
        <dt>{i18n.t.users.email}</dt>
        <dd>{auth.user.email}</dd>
        <dt>{i18n.t.users.role}</dt>
        <dd><span class="badge badge-primary">{i18n.t.roles[auth.user.role]}</span></dd>
        <dt>{i18n.t.common.status}</dt>
        <dd>
          <span class="badge" class:badge-success={auth.user.status === 'active'} class:badge-danger={auth.user.status === 'suspended'}>
            {auth.user.status}
          </span>
        </dd>
        <dt>{i18n.t.users.shell}</dt>
        <dd class="mono">{auth.user.shell}</dd>
        <dt>{i18n.t.users.lastLogin}</dt>
        <dd>{formatDateTime(auth.user.last_login_at, localeCode)}</dd>
      </dl>
    </div>

    <div class="card">
      <h2 class="card-title">{i18n.t.dashboard.myPackage}</h2>
      {#if myPackage}
        <dl class="kv-list">
          <dt>{i18n.t.packages.name}</dt>
          <dd><strong>{myPackage.name}</strong></dd>
          <dt>{i18n.t.packages.diskMb}</dt>
          <dd>{formatQuota(myPackage.disk_mb, 'MB')}</dd>
          <dt>{i18n.t.packages.bandwidthMb}</dt>
          <dd>{formatQuota(myPackage.bandwidth_mb, 'MB')}</dd>
          <dt>{i18n.t.packages.domains}</dt>
          <dd>{formatQuota(myPackage.domains)}</dd>
          <dt>{i18n.t.packages.databases}</dt>
          <dd>{formatQuota(myPackage.databases)}</dd>
          <dt>{i18n.t.packages.mailAccounts}</dt>
          <dd>{formatQuota(myPackage.mail_accounts)}</dd>
          <dt>{i18n.t.packages.containers}</dt>
          <dd>{formatQuota(myPackage.containers)}</dd>
          <dt>{i18n.t.packages.memoryMb}</dt>
          <dd>{formatQuota(myPackage.memory_mb, 'MB')}</dd>
          <dt>PHP</dt>
          <dd class="mono">{myPackage.php_versions.join(', ') || '—'}</dd>
        </dl>
      {:else}
        <p class="page-subtitle">{i18n.t.dashboard.noPackage}</p>
      {/if}
    </div>
  </div>
{/if}

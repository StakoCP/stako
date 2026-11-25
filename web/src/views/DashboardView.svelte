<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatBytes, formatDateTime, formatQuota, formatUptime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import type { Package, SystemInfo } from '../lib/types';

  let sysInfo = $state<SystemInfo | null>(null);
  let myPackage = $state<Package | null>(null);
  let loading = $state(true);
  let error = $state('');

  let isAdmin = $derived(auth.user?.role === 'admin');

  async function load() {
    loading = true;
    error = '';
    try {
      if (isAdmin) {
        sysInfo = await api.get<SystemInfo>('/system/info');
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
      : 0
  );
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

{#if loading}
  <p>{i18n.t.common.loading}</p>
{:else if isAdmin && sysInfo}
  <div class="grid-cards">
    <div class="card">
      <div class="stat-label">{i18n.t.dashboard.countsUsers}</div>
      <div class="stat-value">{sysInfo.counts.users}</div>
    </div>
    <div class="card">
      <div class="stat-label">{i18n.t.dashboard.countsPackages}</div>
      <div class="stat-value">{sysInfo.counts.packages}</div>
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

  <div class="grid-two">
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

    <div class="card">
      <h2 class="card-title">{i18n.t.dashboard.memory}</h2>
      {#if sysInfo.memory_total_bytes !== null && memUsed !== null}
        <div class="stat-value">{memPct}%</div>
        <p class="stat-sub">
          {fmt(i18n.t.dashboard.memoryDetail, {
            used: formatBytes(memUsed),
            total: formatBytes(sysInfo.memory_total_bytes),
          })}
        </p>
        <div class="progress-track" role="progressbar" aria-valuenow={memPct} aria-valuemin={0} aria-valuemax={100}>
          <div class="progress-fill" style:width={`${memPct}%`}></div>
        </div>
      {:else}
        <p class="page-subtitle">—</p>
      {/if}
    </div>
  </div>
{:else if auth.user}
  <div class="grid-two">
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
        <dd>{formatDateTime(auth.user.last_login_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</dd>
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

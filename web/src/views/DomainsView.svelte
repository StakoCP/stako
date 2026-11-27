<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import Pagination from '../components/Pagination.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type {
    Domain,
    DomainCacheConfig,
    DomainSSLMode,
    DomainStack,
    DomainTemplate,
    IPAddress,
    PaginatedResponse,
    ProxyRule,
    User,
  } from '../lib/types';

  let domains = $state<Domain[]>([]);
  let ips = $state<IPAddress[]>([]);
  let users = $state<User[]>([]);
  let phpVersions = $state<string[]>([]);
  let total = $state(0);
  let limit = 25;
  let offset = $state(0);
  let search = $state('');
  let loading = $state(true);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let canSelectOwner = $derived(auth.user?.role === 'admin' || auth.user?.role === 'reseller');
  let ipv4List = $derived(ips.filter((ip) => ip.family === 'ipv4'));
  let ipv6List = $derived(ips.filter((ip) => ip.family === 'ipv6'));

  // Create / Edit modal
  let formOpen = $state(false);
  let editingDomain = $state<Domain | null>(null);
  let fDomain = $state('');
  let fUserId = $state('');
  let fAliases = $state('');
  let fStack = $state<DomainStack>('nginx');
  let fTemplate = $state<DomainTemplate>('php');
  let fDocroot = $state('');
  let fPhpVersion = $state('8.3');
  let fSslMode = $state<DomainSSLMode>('none');
  let fForceHttps = $state(false);
  let fHsts = $state(false);
  let fIpv4Id = $state('');
  let fIpv6Id = $state('');
  let fProxyTarget = $state('');
  let formError = $state('');
  let saving = $state(false);

  let selectedUsername = $derived(
    users.find((u) => u.id === fUserId)?.username ?? auth.user?.username ?? 'user'
  );

  // Proxy rules modal
  let proxyModalDomain = $state<Domain | null>(null);
  let proxyRules = $state<ProxyRule[]>([]);
  let proxyLoading = $state(false);
  let pPath = $state('');
  let pUpstream = $state('');
  let pWebsocket = $state(false);
  let pSse = $state(false);
  let pGrpc = $state(false);
  let proxySaving = $state(false);
  let proxyError = $state('');

  // Delete modal
  let deleteTarget = $state<Domain | null>(null);
  let deleting = $state(false);

  // SSL modal
  let sslModalDomain = $state<Domain | null>(null);
  let sslTab = $state<'letsencrypt' | 'custom'>('letsencrypt');
  let sslChallenge = $state<'http-01' | 'dns-01'>('http-01');
  let sslWildcard = $state(false);
  let sslDnsProvider = $state<'manual' | 'cloudflare'>('manual');
  let sslCertPem = $state('');
  let sslKeyPem = $state('');
  let sslChainPem = $state('');
  let sslSaving = $state(false);
  let sslError = $state('');
  let sslCustomResult = $state<{ subject: string; not_after: string } | null>(null);

  // Domain logs modal
  let logsModalDomain = $state<Domain | null>(null);
  let logsType = $state<'access' | 'error' | 'goaccess'>('access');
  let logsPath = $state('');
  let logsText = $state('');
  let logsLoading = $state(false);

  interface GoAccessItem {
    data: string;
    hits: { count: number; percent?: number };
  }
  interface GoAccessReport {
    general?: {
      total_requests?: number;
      unique_visitors?: number;
      bandwidth?: number;
      failed_requests?: number;
    };
    requests?: { data?: GoAccessItem[] };
    status_codes?: { data?: GoAccessItem[] };
  }

  let goaccessReport = $derived.by((): GoAccessReport | null => {
    if (logsType !== 'goaccess' || !logsText.trim().startsWith('{')) return null;
    try {
      return JSON.parse(logsText) as GoAccessReport;
    } catch {
      return null;
    }
  });

  async function loadDomainLogs(d: Domain, type: 'access' | 'error' | 'goaccess') {
    logsType = type;
    logsLoading = true;
    logsText = '';
    try {
      const res = await api.get<{ path: string; logs: string }>(
        `/domains/${d.id}/logs?type=${type}&lines=200`
      );
      logsPath = res.path || '';
      logsText = res.logs || '';
    } catch (err) {
      logsText = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      logsLoading = false;
    }
  }

  function openDomainLogs(d: Domain) {
    logsModalDomain = d;
    logsPath = '';
    loadDomainLogs(d, 'access');
  }

  // Custom Nginx config modal
  let customNginxDomain = $state<Domain | null>(null);
  let customNginxPath = $state('');
  let customNginxContent = $state('');
  let customNginxLoading = $state(false);
  let customNginxSaving = $state(false);
  let customNginxError = $state('');

  async function openCustomNginx(d: Domain) {
    customNginxDomain = d;
    customNginxPath = '';
    customNginxContent = '';
    customNginxError = '';
    customNginxLoading = true;
    try {
      const res = await api.get<{ path: string; content: string }>(`/domains/${d.id}/custom-nginx`);
      customNginxPath = res.path || '';
      customNginxContent = res.content || '';
    } catch (err) {
      customNginxError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      customNginxLoading = false;
    }
  }

  async function saveCustomNginx(e: SubmitEvent) {
    e.preventDefault();
    if (!customNginxDomain) return;
    customNginxError = '';
    customNginxSaving = true;
    try {
      await api.put<{ ok: boolean; path: string }>(`/domains/${customNginxDomain.id}/custom-nginx`, {
        content: customNginxContent,
      });
      toast.success(i18n.t.domains.customNginxSaved);
      customNginxDomain = null;
    } catch (err) {
      customNginxError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      customNginxSaving = false;
    }
  }

  function notifyJob(message: string, jobId?: string | null) {
    if (jobId) {
      toast.success(`${message} (${fmt(i18n.t.domains.jobToast, { jobId })})`, {
        href: `#/jobs/${jobId}`,
        label: i18n.t.domains.viewJob,
      });
    } else {
      toast.success(message);
    }
  }

  async function loadDomains() {
    loading = true;
    try {
      const qs = new URLSearchParams({
        limit: String(limit),
        offset: String(offset),
      });
      if (search.trim()) qs.set('q', search.trim());
      const res = await api.get<PaginatedResponse<Domain>>(`/domains?${qs.toString()}`);
      domains = res.items;
      total = res.total;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
    }
  }

  async function loadIPs() {
    try {
      const res = await api.get<PaginatedResponse<IPAddress>>('/ips?limit=200&offset=0');
      ips = res.items;
    } catch {
      // ignore if user has no IP list access
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

  async function loadPhpVersions() {
    try {
      const res = await api.get<{ items: string[] }>('/system/php-versions');
      phpVersions = res.items;
    } catch {
      phpVersions = [];
    }
  }

  onMount(() => {
    loadDomains();
    loadIPs();
    loadUsers();
    loadPhpVersions();
  });

  function handleSearch(e: SubmitEvent) {
    e.preventDefault();
    offset = 0;
    loadDomains();
  }

  function openCreate() {
    editingDomain = null;
    fDomain = '';
    fUserId = auth.user?.id ?? '';
    fAliases = '';
    fStack = 'nginx';
    fTemplate = 'php';
    fDocroot = '';
    fPhpVersion = phpVersions[0] ?? '';
    fSslMode = 'none';
    fForceHttps = false;
    fHsts = false;
    fIpv4Id = '';
    fIpv6Id = '';
    fProxyTarget = '';
    formError = '';
    formOpen = true;
  }

  function openEdit(d: Domain) {
    editingDomain = d;
    fDomain = d.domain;
    fUserId = d.user_id;
    fAliases = d.aliases.join(', ');
    fStack = d.stack;
    fTemplate = d.template;
    fDocroot = d.docroot || '';
    fPhpVersion = d.php_version || '';
    fSslMode = d.ssl_mode;
    fForceHttps = d.force_https;
    fHsts = d.hsts;
    fIpv4Id = d.ipv4_id ?? '';
    fIpv6Id = d.ipv6_id ?? '';
    fProxyTarget = d.proxy_target || '';
    formError = '';
    formOpen = true;
  }

  async function submitForm(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    saving = true;
    const aliases = fAliases
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);

    try {
      if (editingDomain) {
        const body: Record<string, unknown> = {
          aliases,
          stack: fStack,
          template: fTemplate,
          php_version: fPhpVersion.trim(),
          force_https: fForceHttps,
          hsts: fHsts,
          ipv4_id: fIpv4Id,
          ipv6_id: fIpv6Id,
        };
        if (fDocroot.trim()) {
          body.docroot = fDocroot.trim();
        }
        if (fTemplate === 'proxy') {
          body.proxy_target = fProxyTarget.trim();
        }
        const res = await api.patch<{ domain: Domain; job_id: string }>(
          `/domains/${editingDomain.id}`,
          body
        );
        notifyJob(i18n.t.domains.updatedSuccess, res.job_id);
      } else {
        const body: Record<string, unknown> = {
          domain: fDomain.trim(),
          aliases,
          stack: fStack,
          template: fTemplate,
          php_version: fPhpVersion.trim(),
          ssl_mode: fSslMode,
          force_https: fForceHttps,
          hsts: fHsts,
        };
        if (canSelectOwner && fUserId) body.user_id = fUserId;
        if (fDocroot.trim()) body.docroot = fDocroot.trim();
        if (fIpv4Id) body.ipv4_id = fIpv4Id;
        if (fIpv6Id) body.ipv6_id = fIpv6Id;
        if (fTemplate === 'proxy' && fProxyTarget.trim()) {
          body.proxy_target = fProxyTarget.trim();
        }
        const res = await api.post<{ domain: Domain; job_id: string }>('/domains', body);
        notifyJob(i18n.t.domains.createdSuccess, res.job_id);
      }
      formOpen = false;
      await loadDomains();
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

  function openSslModal(d: Domain) {
    sslModalDomain = d;
    sslTab = d.ssl_mode === 'custom' ? 'custom' : 'letsencrypt';
    sslChallenge = 'http-01';
    sslWildcard = false;
    sslDnsProvider = 'manual';
    sslCertPem = '';
    sslKeyPem = '';
    sslChainPem = '';
    sslError = '';
    sslCustomResult = null;
  }

  async function submitSslLetsEncrypt(e: SubmitEvent) {
    e.preventDefault();
    if (!sslModalDomain) return;
    sslError = '';
    sslSaving = true;
    try {
      const body: Record<string, unknown> = {
        challenge: sslChallenge,
      };
      if (sslChallenge === 'dns-01') {
        body.wildcard = sslWildcard;
        body.dns_provider = sslDnsProvider;
      }
      const res = await api.post<{ job_id: string }>(`/domains/${sslModalDomain.id}/ssl`, body);
      notifyJob(i18n.t.domains.sslIssuedSuccess, res.job_id);
      sslModalDomain = null;
      await loadDomains();
    } catch (err) {
      sslError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      sslSaving = false;
    }
  }

  async function submitSslCustom(e: SubmitEvent) {
    e.preventDefault();
    if (!sslModalDomain) return;
    sslError = '';
    sslCustomResult = null;
    sslSaving = true;
    try {
      const res = await api.post<{
        job_id?: string | null;
        subject?: string;
        not_after?: string;
      }>(`/domains/${sslModalDomain.id}/ssl/custom`, {
        certificate_pem: sslCertPem.trim(),
        private_key_pem: sslKeyPem.trim(),
        chain_pem: sslChainPem.trim(),
      });
      notifyJob(i18n.t.domains.ssl_custom_saved, res.job_id);
      if (res.subject || res.not_after) {
        sslCustomResult = {
          subject: res.subject || sslModalDomain.domain,
          not_after: res.not_after || '',
        };
      } else {
        sslModalDomain = null;
      }
      await loadDomains();
    } catch (err) {
      sslError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      sslSaving = false;
    }
  }

  async function openProxyRules(d: Domain) {
    proxyModalDomain = d;
    proxyRules = [];
    pPath = '';
    pUpstream = '';
    pWebsocket = false;
    pSse = false;
    pGrpc = false;
    proxyError = '';
    await loadProxyRules(d.id);
  }

  async function loadProxyRules(domainId: string) {
    proxyLoading = true;
    try {
      const res = await api.get<{ items: ProxyRule[] }>(`/domains/${domainId}/proxy-rules`);
      proxyRules = res.items;
    } catch (err) {
      proxyError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      proxyLoading = false;
    }
  }

  async function addProxyRule(e: SubmitEvent) {
    e.preventDefault();
    if (!proxyModalDomain) return;
    proxyError = '';
    proxySaving = true;
    try {
      await api.post<ProxyRule>(`/domains/${proxyModalDomain.id}/proxy-rules`, {
        path: pPath.trim(),
        upstream: pUpstream.trim(),
        websocket: pWebsocket,
        sse: pSse,
        grpc: pGrpc,
      });
      toast.success(i18n.t.domains.proxyRuleAdded);
      pPath = '';
      pUpstream = '';
      pWebsocket = false;
      pSse = false;
      pGrpc = false;
      await loadProxyRules(proxyModalDomain.id);
    } catch (err) {
      proxyError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      proxySaving = false;
    }
  }

  async function deleteProxyRule(ruleId: string) {
    if (!proxyModalDomain) return;
    try {
      await api.delete(`/domains/${proxyModalDomain.id}/proxy-rules/${ruleId}`);
      toast.success(i18n.t.domains.proxyRuleDeleted);
      await loadProxyRules(proxyModalDomain.id);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    try {
      const res = await api.delete<{ job_id: string }>(`/domains/${deleteTarget.id}`);
      notifyJob(i18n.t.domains.deletedSuccess, res?.job_id);
      deleteTarget = null;
      await loadDomains();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      deleting = false;
    }
  }

  // Cache & CDN modal
  let cacheModalDomain = $state<Domain | null>(null);
  let cacheConfig = $state<DomainCacheConfig | null>(null);
  let cacheLoading = $state(false);
  let cacheError = $state('');
  let mcEnabled = $state(false);
  let mcTtl = $state(10);
  let mcSaving = $state(false);
  let redisToggling = $state(false);
  let cfZoneId = $state('');
  let cfApiToken = $state('');
  let cfUnderAttack = $state(false);
  let cfDevMode = $state(false);
  let cfSaving = $state(false);
  let cfPurging = $state(false);

  function syncCacheForm(cfg: DomainCacheConfig) {
    cacheConfig = cfg;
    mcEnabled = cfg.microcache_enabled;
    mcTtl = cfg.microcache_ttl_sec > 0 ? cfg.microcache_ttl_sec : 10;
    cfZoneId = cfg.cf_zone_id || '';
    cfApiToken = '';
    cfUnderAttack = cfg.cf_under_attack;
    cfDevMode = cfg.cf_dev_mode;
  }

  async function openCacheModal(d: Domain) {
    cacheModalDomain = d;
    cacheConfig = null;
    cacheError = '';
    cacheLoading = true;
    try {
      const cfg = await api.get<DomainCacheConfig>(`/domains/${d.id}/cache`);
      syncCacheForm(cfg);
    } catch (err) {
      cacheError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      cacheLoading = false;
    }
  }

  async function saveMicrocache(e: SubmitEvent) {
    e.preventDefault();
    if (!cacheModalDomain) return;
    cacheError = '';
    mcSaving = true;
    try {
      const cfg = await api.put<DomainCacheConfig>(
        `/domains/${cacheModalDomain.id}/cache/microcache`,
        {
          enabled: mcEnabled,
          ttl_sec: Number(mcTtl) || 10,
        }
      );
      syncCacheForm(cfg);
      toast.success(i18n.t.domains.cache.microcacheSaved);
    } catch (err) {
      cacheError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      mcSaving = false;
    }
  }

  async function toggleRedisObjectCache() {
    if (!cacheModalDomain || !cacheConfig) return;
    cacheError = '';
    redisToggling = true;
    try {
      const cfg = await api.post<DomainCacheConfig>(
        `/domains/${cacheModalDomain.id}/cache/redis-object`,
        {
          enabled: !cacheConfig.redis_object_cache,
        }
      );
      syncCacheForm(cfg);
      toast.success(i18n.t.domains.cache.redisSaved);
    } catch (err) {
      cacheError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      redisToggling = false;
    }
  }

  async function saveCloudflare(e: SubmitEvent) {
    e.preventDefault();
    if (!cacheModalDomain) return;
    cacheError = '';
    cfSaving = true;
    try {
      const cfg = await api.put<DomainCacheConfig>(
        `/domains/${cacheModalDomain.id}/cloudflare`,
        {
          cf_zone_id: cfZoneId.trim(),
          cf_api_token: cfApiToken.trim(),
          cf_under_attack: cfUnderAttack,
          cf_dev_mode: cfDevMode,
        }
      );
      syncCacheForm(cfg);
      toast.success(i18n.t.domains.cache.cfSaved);
    } catch (err) {
      cacheError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      cfSaving = false;
    }
  }

  async function purgeCloudflareCache() {
    if (!cacheModalDomain) return;
    cacheError = '';
    cfPurging = true;
    try {
      await api.post(`/domains/${cacheModalDomain.id}/cloudflare/purge`, {});
      toast.success(i18n.t.domains.cache.purgedSuccess);
    } catch (err) {
      cacheError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      cfPurging = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.domains.title}</h1>
    <p class="page-subtitle">{i18n.t.domains.subtitle}</p>
  </div>
  {#if canWrite}
    <button type="button" class="btn btn-primary" onclick={openCreate}>
      + {i18n.t.domains.createDomain}
    </button>
  {/if}
</div>

<div class="toolbar">
  <form class="toolbar-filters" onsubmit={handleSearch}>
    <input
      type="search"
      class="form-control"
      placeholder={i18n.t.common.search}
      bind:value={search}
      aria-label={i18n.t.common.search}
    />
    <button type="submit" class="btn">{i18n.t.common.search}</button>
  </form>
  <button type="button" class="btn btn-sm" onclick={loadDomains} disabled={loading}>
    {i18n.t.common.refresh}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.domains.domain}</th>
        <th>{i18n.t.domains.stack}</th>
        <th>{i18n.t.domains.template}</th>
        <th>{i18n.t.domains.ssl}</th>
        <th>{i18n.t.domains.proxyTarget}</th>
        <th>{i18n.t.common.status}</th>
        <th>{i18n.t.common.actions}</th>
      </tr>
    </thead>
    <tbody>
      {#if loading && domains.length === 0}
        <tr><td colspan="7" class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if domains.length === 0}
        <tr><td colspan="7" class="empty-state">{i18n.t.common.noData}</td></tr>
      {:else}
        {#each domains as d (d.id)}
          <tr>
            <td class="mono">
              <strong>{d.domain}</strong>
              {#if d.aliases.length > 0}
                <div class="form-hint">{d.aliases.join(', ')}</div>
              {/if}
              {#if d.docroot}
                <div class="form-hint">{d.docroot}</div>
              {/if}
            </td>
            <td><span class="badge badge-primary">{d.stack}</span></td>
            <td class="mono">{d.template}</td>
            <td>
              <span
                class="badge"
                class:badge-success={d.ssl_enabled}
              >
                {d.ssl_enabled ? `${i18n.t.domains.sslActive} (${d.ssl_mode})` : i18n.t.domains.sslInactive}
              </span>
            </td>
            <td class="mono">{d.proxy_target || '—'}</td>
            <td>
              <span
                class="badge"
                class:badge-success={d.status === 'active'}
                class:badge-danger={d.status === 'suspended'}
              >
                {d.status}
              </span>
            </td>
            <td>
              <div class="table-actions">
                <a
                  class="btn btn-sm btn-icon"
                  href={`${d.ssl_enabled ? 'https' : 'http'}://${d.domain}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  title={`${i18n.t.domains.openUrl} (${d.domain})`}
                  aria-label={`${i18n.t.domains.openUrl} (${d.domain})`}
                >
                  <ActionIcon name="external" />
                </a>
                {#if canWrite}
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.common.edit}
                    aria-label={i18n.t.common.edit}
                    onclick={() => openEdit(d)}
                  >
                    <ActionIcon name="edit" />
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.domains.issueSsl}
                    aria-label={i18n.t.domains.issueSsl}
                    onclick={() => openSslModal(d)}
                  >
                    <ActionIcon name="ssl" />
                  </button>
                {/if}
                <button
                  type="button"
                  class="btn btn-sm btn-icon"
                  title={i18n.t.domains.proxyRules}
                  aria-label={i18n.t.domains.proxyRules}
                  onclick={() => openProxyRules(d)}
                >
                  <ActionIcon name="proxy" />
                </button>
                <button
                  type="button"
                  class="btn btn-sm btn-icon"
                  title={i18n.t.domains.logs}
                  aria-label={i18n.t.domains.logs}
                  onclick={() => openDomainLogs(d)}
                >
                  <ActionIcon name="logs" />
                </button>
                {#if canWrite}
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.domains.customNginx}
                    aria-label={i18n.t.domains.customNginx}
                    onclick={() => openCustomNginx(d)}
                  >
                    <ActionIcon name="config" />
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.domains.cacheCdn}
                    aria-label={i18n.t.domains.cacheCdn}
                    onclick={() => openCacheModal(d)}
                  >
                    <ActionIcon name="shield" />
                  </button>
                {/if}
                {#if canWrite}
                  <button
                    type="button"
                    class="btn btn-sm btn-icon btn-danger"
                    title={i18n.t.common.delete}
                    aria-label={i18n.t.common.delete}
                    onclick={() => (deleteTarget = d)}
                  >
                    <ActionIcon name="delete" />
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
    loadDomains();
  }}
/>

<Modal
  open={formOpen}
  title={editingDomain ? i18n.t.domains.editDomain : i18n.t.domains.createDomain}
  size="lg"
  onclose={() => (formOpen = false)}
>
  <form id="domain-form" onsubmit={submitForm}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="d-domain">{i18n.t.domains.domain}</label>
        <input
          id="d-domain"
          type="text"
          class="form-control"
          bind:value={fDomain}
          disabled={editingDomain !== null}
          required
          placeholder="example.com"
        />
      </div>
      {#if canSelectOwner && !editingDomain && users.length > 0}
        <div class="form-group">
          <label class="form-label" for="d-owner">{i18n.t.domains.owner}</label>
          <select id="d-owner" class="form-control" bind:value={fUserId}>
            {#each users as u (u.id)}
              <option value={u.id}>{u.username} ({i18n.t.roles[u.role]})</option>
            {/each}
          </select>
        </div>
      {/if}
    </div>

    <div class="form-group">
      <label class="form-label" for="d-aliases">{i18n.t.domains.aliases}</label>
      <input
        id="d-aliases"
        type="text"
        class="form-control"
        bind:value={fAliases}
        placeholder="www.example.com, app.example.com"
      />
    </div>

    <div class="form-group">
      <label class="form-label" for="d-docroot">{i18n.t.domains.docroot}</label>
      <input
        id="d-docroot"
        type="text"
        class="form-control"
        bind:value={fDocroot}
        placeholder={`/home/${selectedUsername}/web/${fDomain.trim() || 'example.com'}/public_html`}
      />
      <div class="form-hint">{i18n.t.domains.docrootHint}</div>
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="d-stack">{i18n.t.domains.stack}</label>
        <select id="d-stack" class="form-control" bind:value={fStack}>
          <option value="nginx">nginx</option>
          <option value="nginx_apache">nginx_apache</option>
          <option value="apache">apache</option>
        </select>
      </div>
      <div class="form-group">
        <label class="form-label" for="d-template">{i18n.t.domains.template}</label>
        <select id="d-template" class="form-control" bind:value={fTemplate}>
          <option value="php">php</option>
          <option value="wordpress">wordpress</option>
          <option value="laravel">laravel</option>
          <option value="static">static</option>
          <option value="spa">spa</option>
          <option value="proxy">proxy</option>
          <option value="redirect">redirect</option>
        </select>
      </div>
      <div class="form-group">
        <label class="form-label" for="d-php">{i18n.t.domains.phpVersion}</label>
        <select id="d-php" class="form-control" bind:value={fPhpVersion}>
          <option value="">{i18n.t.domains.noPhp}</option>
          {#each phpVersions as ver (ver)}
            <option value={ver}>PHP {ver}</option>
          {/each}
          {#if fPhpVersion && !phpVersions.includes(fPhpVersion)}
            <option value={fPhpVersion}>PHP {fPhpVersion}</option>
          {/if}
        </select>
      </div>
    </div>

    {#if fTemplate === 'proxy'}
      <div class="form-group">
        <label class="form-label" for="d-proxy-target">{i18n.t.domains.proxyTarget}</label>
        <input
          id="d-proxy-target"
          type="text"
          class="form-control"
          bind:value={fProxyTarget}
          placeholder="127.0.0.1:3000"
          required
        />
      </div>
    {/if}

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="d-ipv4">{i18n.t.domains.ipv4}</label>
        <select id="d-ipv4" class="form-control" bind:value={fIpv4Id}>
          <option value="">{i18n.t.common.none}</option>
          {#each ipv4List as ip (ip.id)}
            <option value={ip.id}>{ip.ip} ({ip.scope})</option>
          {/each}
        </select>
      </div>
      <div class="form-group">
        <label class="form-label" for="d-ipv6">{i18n.t.domains.ipv6}</label>
        <select id="d-ipv6" class="form-control" bind:value={fIpv6Id}>
          <option value="">{i18n.t.common.none}</option>
          {#each ipv6List as ip (ip.id)}
            <option value={ip.id}>{ip.ip} ({ip.scope})</option>
          {/each}
        </select>
      </div>
      {#if !editingDomain}
        <div class="form-group">
          <label class="form-label" for="d-ssl-mode">{i18n.t.domains.sslMode}</label>
          <select id="d-ssl-mode" class="form-control" bind:value={fSslMode}>
            <option value="none">{i18n.t.domains.sslNone}</option>
            <option value="letsencrypt">{i18n.t.domains.sslLetsEncrypt}</option>
          </select>
        </div>
      {/if}
    </div>

    <div class="checkbox-group">
      <label class="checkbox-row">
        <input type="checkbox" bind:checked={fForceHttps} />
        <span>{i18n.t.domains.forceHttps}</span>
      </label>
      <label class="checkbox-row">
        <input type="checkbox" bind:checked={fHsts} />
        <span>{i18n.t.domains.hsts}</span>
      </label>
    </div>
  </form>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (formOpen = false)} disabled={saving}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="domain-form" class="btn btn-primary" disabled={saving}>
      {saving ? i18n.t.common.loading : i18n.t.common.save}
    </button>
  {/snippet}
</Modal>

<Modal
  open={proxyModalDomain !== null}
  title={proxyModalDomain ? fmt(i18n.t.domains.proxyRulesTitle, { domain: proxyModalDomain.domain }) : ''}
  size="lg"
  onclose={() => (proxyModalDomain = null)}
>
  {#if proxyError}
    <div class="alert alert-danger" role="alert">{proxyError}</div>
  {/if}

  {#if proxyLoading}
    <p>{i18n.t.common.loading}</p>
  {:else if proxyRules.length === 0}
    <p class="page-subtitle">{i18n.t.domains.noProxyRules}</p>
  {:else}
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{i18n.t.domains.path}</th>
            <th>{i18n.t.domains.upstream}</th>
            <th>Flags</th>
            {#if canWrite}
              <th>{i18n.t.common.actions}</th>
            {/if}
          </tr>
        </thead>
        <tbody>
          {#each proxyRules as rule (rule.id)}
            <tr>
              <td class="mono">{rule.path}</td>
              <td class="mono">{rule.upstream}</td>
              <td>
                <div class="table-actions">
                  {#if rule.websocket}<span class="badge badge-info">WS</span>{/if}
                  {#if rule.sse}<span class="badge badge-info">SSE</span>{/if}
                  {#if rule.grpc}<span class="badge badge-info">gRPC</span>{/if}
                  {#if !rule.websocket && !rule.sse && !rule.grpc}—{/if}
                </div>
              </td>
              {#if canWrite}
                <td>
                  <button
                    type="button"
                    class="btn btn-sm btn-icon btn-danger"
                    title={i18n.t.common.delete}
                    aria-label={i18n.t.common.delete}
                    onclick={() => deleteProxyRule(rule.id)}
                  >
                    <ActionIcon name="delete" />
                  </button>
                </td>
              {/if}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}

  {#if canWrite}
    <hr class="section-divider" />
    <form onsubmit={addProxyRule}>
      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="pr-path">{i18n.t.domains.path}</label>
          <input
            id="pr-path"
            type="text"
            class="form-control"
            bind:value={pPath}
            placeholder="/api"
            required
          />
        </div>
        <div class="form-group">
          <label class="form-label" for="pr-upstream">{i18n.t.domains.upstream}</label>
          <input
            id="pr-upstream"
            type="text"
            class="form-control"
            bind:value={pUpstream}
            placeholder="127.0.0.1:8080"
            required
          />
        </div>
      </div>
      <div class="toolbar">
        <div class="checkbox-group">
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={pWebsocket} />
            <span>{i18n.t.domains.websocket}</span>
          </label>
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={pSse} />
            <span>{i18n.t.domains.sse}</span>
          </label>
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={pGrpc} />
            <span>{i18n.t.domains.grpc}</span>
          </label>
        </div>
        <button type="submit" class="btn btn-primary btn-sm" disabled={proxySaving}>
          + {i18n.t.domains.addProxyRule}
        </button>
      </div>
    </form>
  {/if}

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (proxyModalDomain = null)}>
      {i18n.t.common.close}
    </button>
  {/snippet}
</Modal>

<Modal
  open={customNginxDomain !== null}
  title={customNginxDomain ? `${i18n.t.domains.customNginx}: ${customNginxDomain.domain}` : ''}
  size="lg"
  onclose={() => (customNginxDomain = null)}
>
  <form id="custom-nginx-form" onsubmit={saveCustomNginx}>
    {#if customNginxError}
      <div class="alert alert-danger" role="alert">{customNginxError}</div>
    {/if}
    {#if customNginxPath}
      <p class="form-hint mono">{customNginxPath}</p>
    {/if}
    <p class="form-hint">{i18n.t.domains.customNginxHint}</p>
    {#if customNginxLoading}
      <p>{i18n.t.common.loading}</p>
    {:else}
      <div class="form-group">
        <textarea
          class="form-control mono"
          rows="12"
          bind:value={customNginxContent}
          placeholder="client_max_body_size 64M;"
          aria-label={i18n.t.domains.customNginx}
        ></textarea>
      </div>
    {/if}
  </form>
  {#snippet footer()}
    <button
      type="button"
      class="btn"
      onclick={() => (customNginxDomain = null)}
      disabled={customNginxSaving}
    >
      {i18n.t.common.cancel}
    </button>
    <button
      type="submit"
      form="custom-nginx-form"
      class="btn btn-primary"
      disabled={customNginxLoading || customNginxSaving}
    >
      {customNginxSaving ? i18n.t.common.loading : i18n.t.common.save}
    </button>
  {/snippet}
</Modal>

<Modal
  open={logsModalDomain !== null}
  title={logsModalDomain ? `${i18n.t.domains.logs}: ${logsModalDomain.domain}` : ''}
  size="lg"
  onclose={() => (logsModalDomain = null)}
>
  {#if logsModalDomain}
    <div class="toolbar">
      <div class="tabs">
        <button
          type="button"
          class="tab-btn"
          class:active={logsType === 'access'}
          onclick={() => logsModalDomain && loadDomainLogs(logsModalDomain, 'access')}
        >
          {i18n.t.domains.accessLog}
        </button>
        <button
          type="button"
          class="tab-btn"
          class:active={logsType === 'error'}
          onclick={() => logsModalDomain && loadDomainLogs(logsModalDomain, 'error')}
        >
          {i18n.t.domains.errorLog}
        </button>
        <button
          type="button"
          class="tab-btn"
          class:active={logsType === 'goaccess'}
          onclick={() => logsModalDomain && loadDomainLogs(logsModalDomain, 'goaccess')}
        >
          {i18n.t.domains.goaccessLog}
        </button>
      </div>
      <button
        type="button"
        class="btn btn-sm"
        disabled={logsLoading}
        onclick={() => logsModalDomain && loadDomainLogs(logsModalDomain, logsType)}
      >
        {i18n.t.common.refresh}
      </button>
    </div>
    {#if logsPath}
      <p class="form-hint mono">{logsPath}</p>
    {/if}
    {#if logsLoading}
      <p>{i18n.t.common.loading}</p>
    {:else if goaccessReport}
      <div class="grid-cards">
        <div class="card">
          <div class="stat-label">{i18n.t.domains.totalRequests}</div>
          <div class="stat-value">{goaccessReport.general?.total_requests ?? 0}</div>
        </div>
        <div class="card">
          <div class="stat-label">{i18n.t.domains.uniqueVisitors}</div>
          <div class="stat-value">{goaccessReport.general?.unique_visitors ?? 0}</div>
        </div>
      </div>
      {#if goaccessReport.requests?.data && goaccessReport.requests.data.length > 0}
        <h4 class="section-title">{i18n.t.domains.topUrls}</h4>
        <div class="table-wrap">
          <table>
            <thead><tr><th>URL</th><th>Hits</th></tr></thead>
            <tbody>
              {#each goaccessReport.requests.data.slice(0, 10) as row (row.data)}
                <tr><td class="mono">{row.data}</td><td>{row.hits?.count ?? 0}</td></tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
      {#if goaccessReport.status_codes?.data && goaccessReport.status_codes.data.length > 0}
        <h4 class="section-title">{i18n.t.domains.statusCodes}</h4>
        <div class="table-wrap">
          <table>
            <thead><tr><th>Status</th><th>Hits</th></tr></thead>
            <tbody>
              {#each goaccessReport.status_codes.data.slice(0, 10) as row (row.data)}
                <tr><td class="mono">{row.data}</td><td>{row.hits?.count ?? 0}</td></tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    {:else}
      <pre class="log-box">{logsText || i18n.t.domains.logsEmpty}</pre>
    {/if}
  {/if}
  {#snippet footer()}
    <button type="button" class="btn btn-primary" onclick={() => (logsModalDomain = null)}>
      {i18n.t.common.close}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.domains.deleteConfirmTitle}
  onclose={() => (deleteTarget = null)}
>
  {#if deleteTarget}
    <p>{fmt(i18n.t.domains.deleteConfirmBody, { domain: deleteTarget.domain })}</p>
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

<Modal
  open={cacheModalDomain !== null}
  title={cacheModalDomain ? fmt(i18n.t.domains.cache.modalTitle, { domain: cacheModalDomain.domain }) : ''}
  size="lg"
  onclose={() => (cacheModalDomain = null)}
>
  {#if cacheError}
    <div class="alert alert-danger" role="alert">{cacheError}</div>
  {/if}

  {#if cacheLoading}
    <p>{i18n.t.common.loading}</p>
  {:else if cacheConfig}
    <div class="card">
      <div class="card-header-row">
        <h3 class="card-title">{i18n.t.domains.cache.microcacheTitle}</h3>
      </div>
      <form onsubmit={saveMicrocache}>
        <div class="form-group">
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={mcEnabled} />
            <span>{i18n.t.domains.cache.microcacheEnabled}</span>
          </label>
        </div>
        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="mc-ttl">{i18n.t.domains.cache.microcacheTTL}</label>
            <input
              id="mc-ttl"
              type="number"
              class="form-control"
              min="1"
              max="3600"
              bind:value={mcTtl}
              required
            />
          </div>
        </div>
        <button type="submit" class="btn btn-primary btn-sm" disabled={mcSaving}>
          {mcSaving ? i18n.t.common.loading : i18n.t.domains.cache.saveMicrocache}
        </button>
      </form>
    </div>

    <div class="card">
      <div class="card-header-row">
        <h3 class="card-title">{i18n.t.domains.cache.redisTitle}</h3>
        <span
          class="badge"
          class:badge-success={cacheConfig.redis_object_cache}
        >
          {cacheConfig.redis_object_cache
            ? `${i18n.t.domains.cache.redisActive}${cacheConfig.redis_port > 0 ? ` :${cacheConfig.redis_port}` : ''}`
            : i18n.t.domains.cache.redisInactive}
        </span>
      </div>
      <p class="form-hint">{i18n.t.domains.cache.redisDesc}</p>
      {#if cacheConfig.redis_port > 0}
        <p class="form-hint mono">{i18n.t.domains.cache.redisPortLabel}: {cacheConfig.redis_port}</p>
      {/if}
      <button
        type="button"
        class={cacheConfig.redis_object_cache ? 'btn btn-danger btn-sm' : 'btn btn-primary btn-sm'}
        onclick={toggleRedisObjectCache}
        disabled={redisToggling}
      >
        {redisToggling
          ? i18n.t.common.loading
          : cacheConfig.redis_object_cache
            ? i18n.t.domains.cache.redisDisableBtn
            : i18n.t.domains.cache.redisEnabledBtn}
      </button>
    </div>

    <div class="card">
      <div class="card-header-row">
        <h3 class="card-title">{i18n.t.domains.cache.cfTitle}</h3>
      </div>
      <form onsubmit={saveCloudflare}>
        <div class="form-row">
          <div class="form-group">
            <label class="form-label" for="cf-zone-id">{i18n.t.domains.cache.cfZoneId}</label>
            <input
              id="cf-zone-id"
              type="text"
              class="form-control mono"
              bind:value={cfZoneId}
              placeholder="023e105f4ecef8ad9ca31a8372d0c353"
            />
          </div>
          <div class="form-group">
            <label class="form-label" for="cf-api-token">{i18n.t.domains.cache.cfApiToken}</label>
            <input
              id="cf-api-token"
              type="password"
              class="form-control"
              bind:value={cfApiToken}
              autocomplete="off"
            />
            {#if cacheConfig.has_cf_token}
              <p class="form-hint">{i18n.t.domains.cache.cfTokenHint}</p>
            {/if}
          </div>
        </div>
        <div class="checkbox-group">
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={cfUnderAttack} />
            <span>{i18n.t.domains.cache.cfUnderAttack}</span>
          </label>
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={cfDevMode} />
            <span>{i18n.t.domains.cache.cfDevMode}</span>
          </label>
        </div>
        <div class="toolbar">
          <button type="submit" class="btn btn-primary btn-sm" disabled={cfSaving}>
            {cfSaving ? i18n.t.common.loading : i18n.t.domains.cache.saveCf}
          </button>
          <button
            type="button"
            class="btn btn-danger btn-sm"
            onclick={purgeCloudflareCache}
            disabled={cfPurging || !cacheConfig.cf_zone_id || !cacheConfig.has_cf_token}
          >
            {cfPurging ? i18n.t.common.loading : i18n.t.domains.cache.purgeAllBtn}
          </button>
        </div>
      </form>
    </div>
  {/if}

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (cacheModalDomain = null)}>
      {i18n.t.common.close}
    </button>
  {/snippet}
</Modal>

<Modal
  open={sslModalDomain !== null}
  title={sslModalDomain ? fmt(i18n.t.domains.ssl_issue_title, { domain: sslModalDomain.domain }) : ''}
  onclose={() => (sslModalDomain = null)}
>
  {#if sslModalDomain}
    <div class="tabs mb-4">
      <button
        type="button"
        class="tab-btn"
        class:active={sslTab === 'letsencrypt'}
        onclick={() => {
          sslTab = 'letsencrypt';
          sslError = '';
        }}
      >
        {i18n.t.domains.sslLetsEncrypt}
      </button>
      <button
        type="button"
        class="tab-btn"
        class:active={sslTab === 'custom'}
        onclick={() => {
          sslTab = 'custom';
          sslError = '';
        }}
      >
        {i18n.t.domains.ssl_custom}
      </button>
    </div>

    {#if sslError}
      <div class="alert alert-danger" role="alert">{sslError}</div>
    {/if}

    {#if sslTab === 'letsencrypt'}
      <form id="ssl-le-form" onsubmit={submitSslLetsEncrypt}>
        <div class="form-group">
          <label class="form-label" for="ssl-challenge">{i18n.t.domains.ssl_challenge}</label>
          <select id="ssl-challenge" class="form-control" bind:value={sslChallenge}>
            <option value="http-01">HTTP-01</option>
            <option value="dns-01">DNS-01</option>
          </select>
        </div>

        {#if sslChallenge === 'dns-01'}
          <div class="form-group">
            <label class="checkbox-row">
              <input type="checkbox" bind:checked={sslWildcard} />
              <span>{fmt(i18n.t.domains.ssl_wildcard, { domain: sslModalDomain.domain })}</span>
            </label>
          </div>
          <div class="form-group">
            <label class="form-label" for="ssl-dns-provider">{i18n.t.domains.ssl_dns_provider}</label>
            <select id="ssl-dns-provider" class="form-control" bind:value={sslDnsProvider}>
              <option value="manual">Manual</option>
              <option value="cloudflare">Cloudflare</option>
            </select>
          </div>
        {/if}
      </form>
    {:else}
      {#if sslCustomResult}
        <div class="alert alert-success" role="status">
          <div><strong>{sslCustomResult.subject}</strong></div>
          {#if sslCustomResult.not_after}
            <div class="mono">{sslCustomResult.not_after}</div>
          {/if}
        </div>
      {/if}
      <form id="ssl-custom-form" onsubmit={submitSslCustom}>
        <div class="form-group">
          <label class="form-label" for="ssl-cert-pem">{i18n.t.domains.ssl_cert_pem}</label>
          <textarea
            id="ssl-cert-pem"
            class="form-control mono"
            rows="4"
            bind:value={sslCertPem}
            required
            placeholder="-----BEGIN CERTIFICATE-----"
          ></textarea>
        </div>
        <div class="form-group">
          <label class="form-label" for="ssl-key-pem">{i18n.t.domains.ssl_key_pem}</label>
          <textarea
            id="ssl-key-pem"
            class="form-control mono"
            rows="4"
            bind:value={sslKeyPem}
            required
            placeholder="-----BEGIN PRIVATE KEY-----"
          ></textarea>
        </div>
        <div class="form-group">
          <label class="form-label" for="ssl-chain-pem">{i18n.t.domains.ssl_chain_pem}</label>
          <textarea
            id="ssl-chain-pem"
            class="form-control mono"
            rows="3"
            bind:value={sslChainPem}
            placeholder="-----BEGIN CERTIFICATE-----"
          ></textarea>
        </div>
      </form>
    {/if}
  {/if}
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (sslModalDomain = null)} disabled={sslSaving}>
      {i18n.t.common.close}
    </button>
    {#if sslTab === 'letsencrypt'}
      <button type="submit" form="ssl-le-form" class="btn btn-primary" disabled={sslSaving}>
        {sslSaving ? i18n.t.common.loading : i18n.t.domains.issueSsl}
      </button>
    {:else}
      <button type="submit" form="ssl-custom-form" class="btn btn-primary" disabled={sslSaving}>
        {sslSaving ? i18n.t.common.loading : i18n.t.common.save}
      </button>
    {/if}
  {/snippet}
</Modal>

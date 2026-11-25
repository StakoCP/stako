<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '../components/Modal.svelte';
  import Pagination from '../components/Pagination.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type {
    Domain,
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
  let fPhpVersion = $state('8.3');
  let fSslMode = $state<DomainSSLMode>('none');
  let fForceHttps = $state(false);
  let fHsts = $state(false);
  let fIpv4Id = $state('');
  let fIpv6Id = $state('');
  let fProxyTarget = $state('');
  let formError = $state('');
  let saving = $state(false);

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

  onMount(() => {
    loadDomains();
    loadIPs();
    loadUsers();
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
    fPhpVersion = '8.3';
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
    fPhpVersion = d.php_version || '8.3';
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
          ipv4_id: fIpv4Id || null,
          ipv6_id: fIpv6Id || null,
        };
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

  async function issueSsl(d: Domain) {
    try {
      const res = await api.post<{ job_id: string }>(`/domains/${d.id}/ssl`, {
        mode: 'letsencrypt',
      });
      notifyJob(i18n.t.domains.sslIssuedSuccess, res.job_id);
      await loadDomains();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
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
                {#if canWrite}
                  <button type="button" class="btn btn-sm" onclick={() => openEdit(d)}>
                    {i18n.t.common.edit}
                  </button>
                  <button type="button" class="btn btn-sm" onclick={() => issueSsl(d)}>
                    {i18n.t.domains.issueSsl}
                  </button>
                {/if}
                <button type="button" class="btn btn-sm" onclick={() => openProxyRules(d)}>
                  {i18n.t.domains.proxyRules}
                </button>
                {#if canWrite}
                  <button
                    type="button"
                    class="btn btn-sm btn-danger"
                    onclick={() => (deleteTarget = d)}
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
        <input id="d-php" type="text" class="form-control" bind:value={fPhpVersion} placeholder="8.3" />
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
                    class="btn btn-sm btn-danger"
                    onclick={() => deleteProxyRule(rule.id)}
                  >
                    {i18n.t.common.delete}
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

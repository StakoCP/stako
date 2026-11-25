<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { AppInstall, CatalogApp, Domain, PaginatedResponse, User } from '../lib/types';

  let catalog = $state<CatalogApp[]>([]);
  let installs = $state<AppInstall[]>([]);
  let domains = $state<Domain[]>([]);
  let users = $state<User[]>([]);
  let loadingCatalog = $state(true);
  let loadingInstalls = $state(true);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let canSelectOwner = $derived(auth.user?.role === 'admin' || auth.user?.role === 'reseller');
  let domainMap = $derived(new Map(domains.map((d) => [d.id, d.domain])));

  // Install modal
  let installTarget = $state<CatalogApp | null>(null);
  let fDomainId = $state('');
  let fSiteTitle = $state('');
  let fAdminEmail = $state('');
  let fUserId = $state('');
  let formError = $state('');
  let installing = $state(false);

  function notifyJob(message: string, jobId?: string | null) {
    if (jobId) {
      toast.success(`${message} (${fmt(i18n.t.catalog.jobToast, { jobId })})`, {
        href: `#/jobs/${jobId}`,
        label: i18n.t.catalog.viewJob,
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

  async function loadCatalog() {
    loadingCatalog = true;
    try {
      const res = await api.get<{ items: CatalogApp[] }>('/catalog');
      catalog = res.items;
    } catch (err) {
      toast.error(formatApiError(err));
    } finally {
      loadingCatalog = false;
    }
  }

  async function loadInstalls() {
    loadingInstalls = true;
    try {
      const res = await api.get<{ items: AppInstall[] }>('/catalog/installs');
      installs = res.items;
    } catch (err) {
      toast.error(formatApiError(err));
    } finally {
      loadingInstalls = false;
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
    loadCatalog();
    loadInstalls();
    loadDomains();
    loadUsers();
  });

  function openInstall(app: CatalogApp) {
    installTarget = app;
    fDomainId = domains[0]?.id ?? '';
    fSiteTitle = '';
    fAdminEmail = auth.user?.email ?? '';
    fUserId = auth.user?.id ?? '';
    formError = '';
  }

  async function submitInstall(e: SubmitEvent) {
    e.preventDefault();
    if (!installTarget) return;
    formError = '';
    installing = true;
    try {
      const body: Record<string, unknown> = {
        app_slug: installTarget.slug,
        domain_id: fDomainId,
      };
      if (fSiteTitle.trim()) body.site_title = fSiteTitle.trim();
      if (fAdminEmail.trim()) body.admin_email = fAdminEmail.trim();
      if (canSelectOwner && fUserId) body.user_id = fUserId;

      const res = await api.post<{ install: AppInstall; job_id: string }>('/catalog/install', body);
      notifyJob(fmt(i18n.t.catalog.installStarted, { name: installTarget.name }), res.job_id);
      installTarget = null;
      await loadInstalls();
    } catch (err) {
      formError = formatApiError(err);
    } finally {
      installing = false;
    }
  }

  function installStatusBadge(status: AppInstall['status']): string {
    if (status === 'installed') return 'badge-success';
    if (status === 'installing') return 'badge-warning';
    return 'badge-danger';
  }

  function installStatusLabel(status: AppInstall['status']): string {
    if (status === 'installed') return i18n.t.catalog.statusInstalled;
    if (status === 'installing') return i18n.t.catalog.statusInstalling;
    return i18n.t.catalog.statusFailed;
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.catalog.title}</h1>
    <p class="page-subtitle">{i18n.t.catalog.subtitle}</p>
  </div>
  <button type="button" class="btn btn-sm" onclick={() => { loadCatalog(); loadInstalls(); }}>
    {i18n.t.common.refresh}
  </button>
</div>

{#if loadingCatalog && catalog.length === 0}
  <div class="card empty-state">{i18n.t.common.loading}</div>
{:else if catalog.length === 0}
  <div class="card empty-state">{i18n.t.common.noData}</div>
{:else}
  <div class="catalog-grid">
    {#each catalog as app (app.slug)}
      <article class="card catalog-card">
        <div>
          <div class="catalog-header">
            <div>
              <h2 class="catalog-title">{app.name}</h2>
              <div class="catalog-meta">
                <span>{app.category}</span>
                <span>•</span>
                <span class="mono">v{app.version}</span>
              </div>
            </div>
            <span class="badge {app.type === 'php' ? 'badge-primary' : 'badge-info'}">
              {app.type}
            </span>
          </div>
          <p class="catalog-desc">{app.description}</p>
        </div>
        {#if canWrite}
          <div class="catalog-footer">
            <button type="button" class="btn btn-sm btn-primary" onclick={() => openInstall(app)}>
              {i18n.t.catalog.install}
            </button>
          </div>
        {/if}
      </article>
    {/each}
  </div>
{/if}

<h2 class="section-heading">{i18n.t.catalog.installedTitle}</h2>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.catalog.app}</th>
        <th>{i18n.t.catalog.domain}</th>
        <th>{i18n.t.common.status}</th>
        <th>{i18n.t.common.createdAt}</th>
      </tr>
    </thead>
    <tbody>
      {#if loadingInstalls && installs.length === 0}
        <tr><td colspan="4" class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if installs.length === 0}
        <tr><td colspan="4" class="empty-state">{i18n.t.common.noData}</td></tr>
      {:else}
        {#each installs as inst (inst.id)}
          <tr>
            <td class="mono"><strong>{inst.app_slug}</strong></td>
            <td class="mono">{domainMap.get(inst.domain_id) ?? inst.domain_id}</td>
            <td>
              <span class="badge {installStatusBadge(inst.status)}">
                {installStatusLabel(inst.status)}
              </span>
            </td>
            <td>{formatDateTime(inst.created_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</td>
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>

<Modal
  open={installTarget !== null}
  title={installTarget ? fmt(i18n.t.catalog.installTitle, { name: installTarget.name }) : ''}
  onclose={() => (installTarget = null)}
>
  <form id="install-form" onsubmit={submitInstall}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="inst-domain">{i18n.t.catalog.domain}</label>
      {#if domains.length === 0}
        <div class="alert alert-warning">{i18n.t.catalog.noDomains}</div>
      {:else}
        <select id="inst-domain" class="form-control" bind:value={fDomainId} required>
          <option value="" disabled>{i18n.t.catalog.selectDomain}</option>
          {#each domains as d (d.id)}
            <option value={d.id}>{d.domain}</option>
          {/each}
        </select>
      {/if}
    </div>

    <div class="form-group">
      <label class="form-label" for="inst-title">{i18n.t.catalog.siteTitle}</label>
      <input
        id="inst-title"
        type="text"
        class="form-control"
        bind:value={fSiteTitle}
        placeholder={i18n.t.catalog.siteTitlePlaceholder}
      />
    </div>

    <div class="form-group">
      <label class="form-label" for="inst-email">{i18n.t.catalog.adminEmail}</label>
      <input
        id="inst-email"
        type="email"
        class="form-control"
        bind:value={fAdminEmail}
        placeholder={i18n.t.catalog.adminEmailPlaceholder}
      />
    </div>

    {#if canSelectOwner && users.length > 0}
      <div class="form-group">
        <label class="form-label" for="inst-owner">{i18n.t.catalog.owner}</label>
        <select id="inst-owner" class="form-control" bind:value={fUserId}>
          {#each users as u (u.id)}
            <option value={u.id}>{u.username} ({u.role})</option>
          {/each}
        </select>
      </div>
    {/if}
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (installTarget = null)} disabled={installing}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="submit"
      form="install-form"
      class="btn btn-primary"
      disabled={installing || domains.length === 0 || !fDomainId}
    >
      {installing ? i18n.t.common.loading : i18n.t.catalog.install}
    </button>
  {/snippet}
</Modal>

<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
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

  // Catalog filter & search
  let typeFilter = $state<'all' | 'php' | 'docker'>('all');
  let searchQuery = $state('');

  let filteredCatalog = $derived.by(() => {
    const q = searchQuery.trim().toLowerCase();
    if (!q) return catalog;
    return catalog.filter(
      (app) =>
        app.name.toLowerCase().includes(q) ||
        app.slug.toLowerCase().includes(q) ||
        app.category.toLowerCase().includes(q) ||
        app.description.toLowerCase().includes(q),
    );
  });

  let phpApps = $derived(filteredCatalog.filter((app) => app.type === 'php'));
  let dockerApps = $derived(filteredCatalog.filter((app) => app.type === 'docker'));

  // Install modal
  let installTarget = $state<CatalogApp | null>(null);
  let fDomainId = $state('');
  let fSiteTitle = $state('');
  let fAdminEmail = $state('');
  let fUserId = $state('');
  let formError = $state('');
  let installing = $state(false);

  // Edit install modal
  let editTarget = $state<AppInstall | null>(null);
  let editDomainId = $state('');
  let editReinstall = $state(false);
  let editError = $state('');
  let savingEdit = $state(false);

  // Redeploy & Uninstall state
  let reinstallingId = $state<string | null>(null);
  let deleteTarget = $state<AppInstall | null>(null);
  let deleting = $state(false);

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

  function openEdit(inst: AppInstall) {
    editTarget = inst;
    editDomainId = inst.domain_id;
    editReinstall = false;
    editError = '';
  }

  async function submitEdit(e: SubmitEvent) {
    e.preventDefault();
    if (!editTarget) return;
    editError = '';
    savingEdit = true;
    try {
      const res = await api.patch<{ install: AppInstall; job_id?: string | null }>(
        `/catalog/installs/${editTarget.id}`,
        { domain_id: editDomainId, reinstall: editReinstall },
      );
      notifyJob(fmt(i18n.t.catalog.updatedSuccess, { name: editTarget.app_slug }), res.job_id);
      editTarget = null;
      await loadInstalls();
    } catch (err) {
      editError = formatApiError(err);
    } finally {
      savingEdit = false;
    }
  }

  async function handleReinstall(inst: AppInstall) {
    reinstallingId = inst.id;
    try {
      const res = await api.patch<{ install: AppInstall; job_id?: string | null }>(
        `/catalog/installs/${inst.id}`,
        { domain_id: inst.domain_id, reinstall: true },
      );
      notifyJob(fmt(i18n.t.catalog.updatedSuccess, { name: inst.app_slug }), res.job_id);
      await loadInstalls();
    } catch (err) {
      toast.error(formatApiError(err));
    } finally {
      reinstallingId = null;
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    try {
      const res = await api.delete<{ job_id?: string | null } | void>(
        `/catalog/installs/${deleteTarget.id}`,
      );
      notifyJob(
        fmt(i18n.t.catalog.uninstalledSuccess, { name: deleteTarget.app_slug }),
        res && 'job_id' in res ? res.job_id : undefined,
      );
      deleteTarget = null;
      await loadInstalls();
    } catch (err) {
      toast.error(formatApiError(err));
    } finally {
      deleting = false;
    }
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

{#snippet appCard(app: CatalogApp)}
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
{/snippet}

{#if loadingCatalog && catalog.length === 0}
  <div class="card empty-state">{i18n.t.common.loading}</div>
{:else if catalog.length === 0}
  <div class="card empty-state">{i18n.t.common.noData}</div>
{:else}
  <div class="toolbar">
    <div class="toolbar-filters" role="tablist">
      <button
        type="button"
        class="tab-btn"
        class:active={typeFilter === 'all'}
        onclick={() => (typeFilter = 'all')}
      >
        {i18n.t.catalog.filterAll}
      </button>
      <button
        type="button"
        class="tab-btn"
        class:active={typeFilter === 'php'}
        onclick={() => (typeFilter = 'php')}
      >
        {i18n.t.catalog.filterPhp}
      </button>
      <button
        type="button"
        class="tab-btn"
        class:active={typeFilter === 'docker'}
        onclick={() => (typeFilter = 'docker')}
      >
        {i18n.t.catalog.filterDocker}
      </button>
    </div>
    <div class="toolbar-filters">
      <input
        type="search"
        class="form-control"
        bind:value={searchQuery}
        placeholder={i18n.t.catalog.searchPlaceholder}
        aria-label={i18n.t.catalog.searchPlaceholder}
      />
    </div>
  </div>

  {#if typeFilter === 'all'}
    {#if phpApps.length === 0 && dockerApps.length === 0}
      <div class="card empty-state">{i18n.t.common.noData}</div>
    {:else}
      {#if phpApps.length > 0}
        <h2 class="section-heading">
          {i18n.t.catalog.phpSectionTitle}
          <span class="badge badge-primary">{phpApps.length}</span>
        </h2>
        <div class="catalog-grid">
          {#each phpApps as app (app.slug)}
            {@render appCard(app)}
          {/each}
        </div>
      {/if}

      {#if dockerApps.length > 0}
        <h2 class="section-heading">
          {i18n.t.catalog.dockerSectionTitle}
          <span class="badge badge-info">{dockerApps.length}</span>
        </h2>
        <div class="catalog-grid">
          {#each dockerApps as app (app.slug)}
            {@render appCard(app)}
          {/each}
        </div>
      {/if}
    {/if}
  {:else if typeFilter === 'php'}
    {#if phpApps.length === 0}
      <div class="card empty-state">{i18n.t.common.noData}</div>
    {:else}
      <div class="catalog-grid">
        {#each phpApps as app (app.slug)}
          {@render appCard(app)}
        {/each}
      </div>
    {/if}
  {:else}
    {#if dockerApps.length === 0}
      <div class="card empty-state">{i18n.t.common.noData}</div>
    {:else}
      <div class="catalog-grid">
        {#each dockerApps as app (app.slug)}
          {@render appCard(app)}
        {/each}
      </div>
    {/if}
  {/if}
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
        {#if canWrite}
          <th>{i18n.t.common.actions}</th>
        {/if}
      </tr>
    </thead>
    <tbody>
      {#if loadingInstalls && installs.length === 0}
        <tr><td colspan={canWrite ? 5 : 4} class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if installs.length === 0}
        <tr><td colspan={canWrite ? 5 : 4} class="empty-state">{i18n.t.common.noData}</td></tr>
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
            {#if canWrite}
              <td>
                <div class="table-actions">
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.common.edit}
                    aria-label={i18n.t.common.edit}
                    onclick={() => openEdit(inst)}
                  >
                    <ActionIcon name="edit" />
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.catalog.reinstall}
                    aria-label={i18n.t.catalog.reinstall}
                    disabled={reinstallingId === inst.id}
                    onclick={() => handleReinstall(inst)}
                  >
                    <ActionIcon name="rebuild" />
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-icon btn-danger"
                    title={i18n.t.common.delete}
                    aria-label={i18n.t.common.delete}
                    onclick={() => (deleteTarget = inst)}
                  >
                    <ActionIcon name="delete" />
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

<Modal
  open={editTarget !== null}
  title={editTarget ? fmt(i18n.t.catalog.editTitle, { name: editTarget.app_slug }) : ''}
  onclose={() => (editTarget = null)}
>
  <form id="edit-install-form" onsubmit={submitEdit}>
    {#if editError}
      <div class="alert alert-danger" role="alert">{editError}</div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="edit-inst-domain">{i18n.t.catalog.domain}</label>
      {#if domains.length === 0}
        <div class="alert alert-warning">{i18n.t.catalog.noDomains}</div>
      {:else}
        <select id="edit-inst-domain" class="form-control" bind:value={editDomainId} required>
          <option value="" disabled>{i18n.t.catalog.selectDomain}</option>
          {#each domains as d (d.id)}
            <option value={d.id}>{d.domain}</option>
          {/each}
        </select>
      {/if}
    </div>

    <div class="form-group">
      <label class="form-check">
        <input type="checkbox" bind:checked={editReinstall} />
        <span>{i18n.t.catalog.reinstallCheck}</span>
      </label>
    </div>
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (editTarget = null)} disabled={savingEdit}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="submit"
      form="edit-install-form"
      class="btn btn-primary"
      disabled={savingEdit || domains.length === 0 || !editDomainId}
    >
      {savingEdit ? i18n.t.common.loading : i18n.t.common.save}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.catalog.uninstallTitle}
  onclose={() => (deleteTarget = null)}
>
  {#if deleteTarget}
    <p>
      {fmt(i18n.t.catalog.uninstallBody, {
        name: deleteTarget.app_slug,
        domain: domainMap.get(deleteTarget.domain_id) ?? deleteTarget.domain_id,
      })}
    </p>
  {/if}

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (deleteTarget = null)} disabled={deleting}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-danger"
      onclick={confirmDelete}
      disabled={deleting}
    >
      {deleting ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  {/snippet}
</Modal>

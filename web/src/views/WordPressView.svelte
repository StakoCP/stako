<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { WPSecurityIssue, WPSite } from '../lib/types';

  let sites = $state<WPSite[]>([]);
  let loading = $state(true);
  let scanning = $state(false);
  let bulkUpdating = $state(false);
  let selectedIds = $state<string[]>([]);

  // Security modal
  let securityTarget = $state<WPSite | null>(null);
  let hardening = $state(false);
  let securityError = $state('');

  // Staging modal
  let stagingTarget = $state<WPSite | null>(null);
  let stagingDomain = $state('');
  let creatingStaging = $state(false);
  let stagingError = $state('');

  // Push staging confirm modal
  let pushTarget = $state<WPSite | null>(null);
  let pushingStaging = $state(false);

  // Delete confirm modal
  let deleteTarget = $state<WPSite | null>(null);
  let deleting = $state(false);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let allSelected = $derived(sites.length > 0 && selectedIds.length === sites.length);
  let securityIssues = $derived(
    securityTarget ? parseSecurityIssues(securityTarget.security_issues) : [],
  );

  function parseSecurityIssues(raw: string): WPSecurityIssue[] {
    if (!raw) return [];
    try {
      const parsed: unknown = JSON.parse(raw);
      return Array.isArray(parsed) ? (parsed as WPSecurityIssue[]) : [];
    } catch {
      return [];
    }
  }

  function formatDate(value: string): string {
    if (!value) return '—';
    const d = new Date(value);
    return Number.isNaN(d.getTime()) ? value : d.toLocaleString();
  }

  function scoreBadgeClass(score: number): string {
    if (score >= 80) return 'badge-success';
    if (score >= 50) return 'badge-warning';
    return 'badge-danger';
  }

  function severityBadgeClass(severity: string): string {
    const s = severity.toLowerCase();
    if (s === 'critical' || s === 'high') return 'badge-danger';
    if (s === 'medium') return 'badge-warning';
    return 'badge-info';
  }

  async function loadSites() {
    loading = true;
    try {
      const res = await api.get<{ items: WPSite[] | null }>('/wordpress');
      sites = res.items ?? [];
      const validIds = new Set(sites.map((s) => s.id));
      selectedIds = selectedIds.filter((id) => validIds.has(id));
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    loadSites();
  });

  function toggleSelectAll() {
    if (allSelected) {
      selectedIds = [];
    } else {
      selectedIds = sites.map((s) => s.id);
    }
  }

  function toggleSelect(id: string) {
    if (selectedIds.includes(id)) {
      selectedIds = selectedIds.filter((x) => x !== id);
    } else {
      selectedIds = [...selectedIds, id];
    }
  }

  async function handleScan() {
    scanning = true;
    try {
      const res = await api.post<{ items: WPSite[] | null }>('/wordpress/scan');
      sites = res.items ?? [];
      selectedIds = [];
      toast.success(fmt(i18n.t.wordpress.scanSuccess, { count: sites.length }));
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      scanning = false;
    }
  }

  async function handleBulkUpdate() {
    if (selectedIds.length === 0) return;
    bulkUpdating = true;
    try {
      const res = await api.post<{ job_ids: string[] | null; updated_count: number }>(
        '/wordpress/bulk-update',
        { site_ids: selectedIds },
      );
      toast.success(
        fmt(i18n.t.wordpress.bulkUpdateStarted, { count: res.updated_count ?? selectedIds.length }),
      );
      selectedIds = [];
      await loadSites();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      bulkUpdating = false;
    }
  }

  async function handleAutoLogin(site: WPSite) {
    try {
      const res = await api.post<{ login_url: string }>(`/wordpress/${site.id}/autologin`);
      if (res.login_url) {
        window.open(res.login_url, '_blank', 'noopener');
        toast.success(fmt(i18n.t.wordpress.autoLoginOpened, { domain: site.domain }));
      }
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    }
  }

  function openSecurityModal(site: WPSite) {
    securityTarget = site;
    securityError = '';
  }

  async function handleHarden() {
    if (!securityTarget) return;
    hardening = true;
    securityError = '';
    try {
      const res = await api.post<{ ok: boolean; security_score: number }>(
        `/wordpress/${securityTarget.id}/harden`,
      );
      const updatedScore = res.security_score ?? 100;
      toast.success(
        fmt(i18n.t.wordpress.hardenedSuccess, {
          domain: securityTarget.domain,
          score: updatedScore,
        }),
      );
      sites = sites.map((s) =>
        s.id === securityTarget?.id
          ? { ...s, security_score: updatedScore, security_issues: '[]' }
          : s,
      );
      securityTarget = {
        ...securityTarget,
        security_score: updatedScore,
        security_issues: '[]',
      };
      await loadSites();
    } catch (err) {
      securityError =
        err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      hardening = false;
    }
  }

  function openStagingModal(site: WPSite) {
    stagingTarget = site;
    stagingDomain = `staging.${site.domain}`;
    stagingError = '';
  }

  async function handleCreateStaging(e: SubmitEvent) {
    e.preventDefault();
    if (!stagingTarget) return;
    creatingStaging = true;
    stagingError = '';
    try {
      await api.post<{ site: WPSite; job_id: string | null }>(
        `/wordpress/${stagingTarget.id}/staging`,
        { staging_domain: stagingDomain.trim() },
      );
      toast.success(fmt(i18n.t.wordpress.stagingCreated, { domain: stagingTarget.domain }));
      stagingTarget = null;
      await loadSites();
    } catch (err) {
      stagingError =
        err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      creatingStaging = false;
    }
  }

  async function handlePushStaging() {
    if (!pushTarget) return;
    pushingStaging = true;
    try {
      await api.post<{ job_id: string | null }>(`/wordpress/${pushTarget.id}/push-staging`);
      toast.success(i18n.t.wordpress.pushStarted);
      pushTarget = null;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      pushingStaging = false;
    }
  }

  async function handleDelete() {
    if (!deleteTarget) return;
    deleting = true;
    try {
      await api.delete(`/wordpress/${deleteTarget.id}`);
      sites = sites.filter((s) => s.id !== deleteTarget?.id);
      selectedIds = selectedIds.filter((id) => id !== deleteTarget?.id);
      deleteTarget = null;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      deleting = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.wordpress.title}</h1>
    <p class="page-subtitle">{i18n.t.wordpress.subtitle}</p>
  </div>
</div>

<div class="toolbar">
  <div class="toolbar-filters">
    {#if canWrite}
      <button
        type="button"
        class="btn btn-primary btn-sm"
        onclick={handleScan}
        disabled={scanning}
      >
        {i18n.t.wordpress.scanBtn}
      </button>
      <button
        type="button"
        class="btn btn-sm"
        onclick={handleBulkUpdate}
        disabled={selectedIds.length === 0 || bulkUpdating}
      >
        {i18n.t.wordpress.bulkUpdateBtn}
        {#if selectedIds.length > 0}({selectedIds.length}){/if}
      </button>
    {/if}
  </div>
  <button type="button" class="btn btn-sm" onclick={loadSites} disabled={loading}>
    {i18n.t.common.refresh}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>
          <input
            type="checkbox"
            checked={allSelected}
            onchange={toggleSelectAll}
            disabled={sites.length === 0 || !canWrite}
            aria-label={i18n.t.wordpress.bulkUpdateBtn}
          />
        </th>
        <th>{i18n.t.wordpress.domain}</th>
        <th>{i18n.t.wordpress.version}</th>
        <th>{i18n.t.wordpress.updates}</th>
        <th>{i18n.t.wordpress.securityScore}</th>
        <th>{i18n.t.wordpress.lastScanned}</th>
        <th>{i18n.t.common.actions}</th>
      </tr>
    </thead>
    <tbody>
      {#if loading && sites.length === 0}
        <tr><td colspan="7" class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if sites.length === 0}
        <tr><td colspan="7" class="empty-state">{i18n.t.wordpress.emptyState}</td></tr>
      {:else}
        {#each sites as site (site.id)}
          <tr>
            <td>
              <input
                type="checkbox"
                checked={selectedIds.includes(site.id)}
                onchange={() => toggleSelect(site.id)}
                disabled={!canWrite}
                aria-label={site.domain}
              />
            </td>
            <td>
              <div class="mono">
                <strong>{site.domain}</strong>
                {#if site.is_staging}
                  <span class="badge badge-warning">{i18n.t.wordpress.stagingBadge}</span>
                {/if}
              </div>
              <div class="form-hint mono">{site.path}</div>
            </td>
            <td class="mono">
              <div>WP {site.wp_version || '—'}</div>
              <div class="form-hint">PHP {site.php_version || '—'}</div>
            </td>
            <td>
              {#if site.updates_core === 0 && site.updates_plugins === 0 && site.updates_themes === 0}
                <span class="badge badge-success">{i18n.t.wordpress.upToDate}</span>
              {:else}
                <div class="table-actions">
                  {#if site.updates_core > 0}
                    <span class="badge badge-danger">
                      {fmt(i18n.t.wordpress.coreUpdates, { count: site.updates_core })}
                    </span>
                  {/if}
                  {#if site.updates_plugins > 0}
                    <span class="badge badge-warning">
                      {fmt(i18n.t.wordpress.pluginUpdates, { count: site.updates_plugins })}
                    </span>
                  {/if}
                  {#if site.updates_themes > 0}
                    <span class="badge badge-info">
                      {fmt(i18n.t.wordpress.themeUpdates, { count: site.updates_themes })}
                    </span>
                  {/if}
                </div>
              {/if}
            </td>
            <td>
              <button
                type="button"
                class="link-btn"
                onclick={() => openSecurityModal(site)}
                title={i18n.t.wordpress.hardenSecurity}
              >
                <span class="badge {scoreBadgeClass(site.security_score)}">
                  {site.security_score}/100
                </span>
              </button>
            </td>
            <td>{formatDate(site.last_scanned_at)}</td>
            <td>
              <div class="table-actions">
                {#if canWrite}
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.wordpress.autoLogin}
                    aria-label={i18n.t.wordpress.autoLogin}
                    onclick={() => handleAutoLogin(site)}
                  >
                    <ActionIcon name="external" />
                  </button>
                  {#if !site.is_staging}
                    <button
                      type="button"
                      class="btn btn-sm btn-icon"
                      title={i18n.t.wordpress.createStaging}
                      aria-label={i18n.t.wordpress.createStaging}
                      onclick={() => openStagingModal(site)}
                    >
                      <ActionIcon name="copy" />
                    </button>
                  {:else}
                    <button
                      type="button"
                      class="btn btn-sm btn-icon"
                      title={i18n.t.wordpress.pushStaging}
                      aria-label={i18n.t.wordpress.pushStaging}
                      onclick={() => (pushTarget = site)}
                    >
                      <ActionIcon name="deploy" />
                    </button>
                  {/if}
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.wordpress.hardenSecurity}
                    aria-label={i18n.t.wordpress.hardenSecurity}
                    onclick={() => openSecurityModal(site)}
                  >
                    <ActionIcon name="shield" />
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-icon btn-danger"
                    title={i18n.t.common.delete}
                    aria-label={i18n.t.common.delete}
                    onclick={() => (deleteTarget = site)}
                  >
                    <ActionIcon name="delete" />
                  </button>
                {:else}
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.wordpress.hardenSecurity}
                    aria-label={i18n.t.wordpress.hardenSecurity}
                    onclick={() => openSecurityModal(site)}
                  >
                    <ActionIcon name="shield" />
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

<Modal
  open={securityTarget !== null}
  title={securityTarget
    ? fmt(i18n.t.wordpress.securityIssuesTitle, { domain: securityTarget.domain })
    : ''}
  size="lg"
  onclose={() => (securityTarget = null)}
>
  {#if securityError}
    <div class="alert alert-danger">{securityError}</div>
  {/if}
  {#if securityIssues.length === 0}
    <p>{i18n.t.wordpress.noSecurityIssues}</p>
  {:else}
    <div class="table-wrap">
      <table>
        <tbody>
          {#each securityIssues as issue (issue.id)}
            <tr>
              <td>
                <span class="badge {severityBadgeClass(issue.severity)}">{issue.severity}</span>
              </td>
              <td>{issue.title}</td>
              <td>
                {#if issue.fixed}
                  <span class="badge badge-success">{i18n.t.common.yes}</span>
                {:else}
                  <span class="badge badge-danger">{i18n.t.common.no}</span>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (securityTarget = null)}>
      {i18n.t.common.close}
    </button>
    {#if canWrite}
      <button type="button" class="btn btn-primary" onclick={handleHarden} disabled={hardening}>
        {i18n.t.wordpress.hardenSecurity}
      </button>
    {/if}
  {/snippet}
</Modal>

<Modal
  open={stagingTarget !== null}
  title={stagingTarget
    ? fmt(i18n.t.wordpress.stagingModalTitle, { domain: stagingTarget.domain })
    : ''}
  onclose={() => (stagingTarget = null)}
>
  <form id="wp-staging-form" onsubmit={handleCreateStaging}>
    {#if stagingError}
      <div class="alert alert-danger">{stagingError}</div>
    {/if}
    <div class="form-group">
      <label class="form-label" for="wp-staging-domain">
        {i18n.t.wordpress.stagingDomainLabel}
      </label>
      <input
        id="wp-staging-domain"
        type="text"
        class="form-control"
        bind:value={stagingDomain}
        placeholder={i18n.t.wordpress.stagingDomainPlaceholder}
        required
      />
    </div>
  </form>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (stagingTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="wp-staging-form" class="btn btn-primary" disabled={creatingStaging}>
      {i18n.t.wordpress.createStaging}
    </button>
  {/snippet}
</Modal>

<Modal
  open={pushTarget !== null}
  title={i18n.t.wordpress.pushStaging}
  onclose={() => (pushTarget = null)}
>
  {#if pushTarget}
    <p>{fmt(i18n.t.wordpress.pushStagingConfirm, { domain: pushTarget.domain })}</p>
  {/if}
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (pushTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-primary"
      onclick={handlePushStaging}
      disabled={pushingStaging}
    >
      {i18n.t.common.confirm}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.common.delete}
  onclose={() => (deleteTarget = null)}
>
  {#if deleteTarget}
    <p>{fmt(i18n.t.wordpress.deleteConfirm, { domain: deleteTarget.domain })}</p>
  {/if}
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (deleteTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button type="button" class="btn btn-danger" onclick={handleDelete} disabled={deleting}>
      {i18n.t.common.delete}
    </button>
  {/snippet}
</Modal>

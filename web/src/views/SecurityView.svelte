<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type {
    LockdownStatus,
    PackageUpdate,
    PackageUpdateCategory,
    SecurityFinding,
    SecurityFindingStatus,
    SecurityFindingsResponse,
    SecuritySeverity,
    SystemUpdateInfo,
    UpdatesResponse,
    WafConfig,
    WafEngine,
    WafMode,
  } from '../lib/types';

  let isAdmin = $derived(auth.user?.role === 'admin');

  let panelUpdate = $state<SystemUpdateInfo | null>(null);
  let loadingPanelUpdate = $state(false);
  let updatingPanel = $state(false);

  let lockdownEnabled = $state(false);
  let lockdownAllowedIpsText = $state('');
  let loadingLockdown = $state(true);
  let lockdownConfirmOpen = $state(false);
  let savingLockdown = $state(false);

  let wafEnabled = $state(false);
  let wafEngine = $state<WafEngine>('coraza');
  let wafMode = $state<WafMode>('detection');
  let wafParanoia = $state(1);
  let loadingWaf = $state(true);
  let savingWaf = $state(false);

  let score = $state(0);
  let findings = $state<SecurityFinding[]>([]);
  let loadingFindings = $state(true);
  let scanning = $state(false);
  let updatingFindingId = $state<string | null>(null);

  let rebootRequired = $state(false);
  let updates = $state<PackageUpdate[]>([]);
  let loadingUpdates = $state(true);
  let applyingUpdates = $state(false);

  let scoreTierClass = $derived(
    score >= 80 ? 'score-high' : score >= 50 ? 'score-medium' : 'score-low'
  );

  let scoreBadgeClass = $derived(
    score >= 80 ? 'badge-success' : score >= 50 ? 'badge-warning' : 'badge-danger'
  );

  function notifyJob(message: string, jobId?: string | null) {
    if (jobId) {
      toast.success(`${message} (${fmt(i18n.t.security.jobToast, { jobId })})`, {
        href: `#/jobs/${jobId}`,
        label: i18n.t.security.viewJob,
      });
    } else {
      toast.success(message);
    }
  }

  function severityBadge(sev: SecuritySeverity): string {
    switch (sev) {
      case 'critical':
      case 'high':
        return 'badge-danger';
      case 'medium':
        return 'badge-warning';
      case 'low':
        return 'badge-info';
    }
  }

  function severityLabel(sev: SecuritySeverity): string {
    switch (sev) {
      case 'critical':
        return i18n.t.security.severityCritical;
      case 'high':
        return i18n.t.security.severityHigh;
      case 'medium':
        return i18n.t.security.severityMedium;
      case 'low':
        return i18n.t.security.severityLow;
    }
  }

  function updateCategoryBadge(cat: PackageUpdateCategory): string {
    switch (cat) {
      case 'security':
        return 'badge-danger';
      case 'kernel':
        return 'badge-warning';
      case 'regular':
        return 'badge-info';
    }
  }

  function updateCategoryLabel(cat: PackageUpdateCategory): string {
    switch (cat) {
      case 'security':
        return i18n.t.security.catSecurity;
      case 'kernel':
        return i18n.t.security.catKernel;
      case 'regular':
        return i18n.t.security.catRegular;
    }
  }

  async function loadFindings() {
    loadingFindings = true;
    try {
      const res = await api.get<SecurityFindingsResponse>('/security/findings');
      score = res.score;
      findings = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingFindings = false;
    }
  }

  async function loadUpdates() {
    loadingUpdates = true;
    try {
      const res = await api.get<UpdatesResponse>('/updates');
      rebootRequired = res.reboot_required;
      updates = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingUpdates = false;
    }
  }

  async function loadLockdown() {
    loadingLockdown = true;
    try {
      const res = await api.get<LockdownStatus>('/security/lockdown');
      lockdownEnabled = res.enabled;
      lockdownAllowedIpsText = (res.allowed_ips ?? []).join('\n');
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingLockdown = false;
    }
  }

  async function confirmLockdown() {
    savingLockdown = true;
    try {
      const allowed_ips = lockdownAllowedIpsText
        .split(/[\n,]+/)
        .map((s) => s.trim())
        .filter(Boolean);
      const res = await api.post<LockdownStatus>('/security/lockdown', {
        enabled: lockdownEnabled,
        allowed_ips,
      });
      lockdownEnabled = res.enabled;
      lockdownAllowedIpsText = (res.allowed_ips ?? []).join('\n');
      lockdownConfirmOpen = false;
      toast.success(
        res.enabled ? i18n.t.lockdown.updatedEnabled : i18n.t.lockdown.updatedDisabled
      );
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      savingLockdown = false;
    }
  }

  async function loadWaf() {
    loadingWaf = true;
    try {
      const res = await api.get<WafConfig>('/waf');
      wafEnabled = res.enabled;
      wafEngine = res.engine;
      wafMode = res.mode;
      wafParanoia = res.paranoia_level;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingWaf = false;
    }
  }

  async function saveWaf(e: SubmitEvent) {
    e.preventDefault();
    savingWaf = true;
    try {
      const res = await api.post<{ waf: WafConfig; job_id: string }>('/waf', {
        enabled: wafEnabled,
        engine: wafEngine,
        mode: wafMode,
        paranoia_level: Number(wafParanoia),
      });
      wafEnabled = res.waf.enabled;
      wafEngine = res.waf.engine;
      wafMode = res.waf.mode;
      wafParanoia = res.waf.paranoia_level;
      notifyJob(i18n.t.waf.savedSuccess, res.job_id);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      savingWaf = false;
    }
  }

  onMount(() => {
    if (isAdmin) {
      loadPanelUpdate();
    }
    loadLockdown();
    loadWaf();
    loadFindings();
    loadUpdates();
  });

  async function loadPanelUpdate() {
    loadingPanelUpdate = true;
    try {
      panelUpdate = await api.get<SystemUpdateInfo>('/system/update');
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingPanelUpdate = false;
    }
  }

  async function applyPanelUpdate() {
    updatingPanel = true;
    try {
      const res = await api.post<{ job_id: string }>('/system/update', {
        version: 'latest',
      });
      notifyJob(i18n.t.security.panelUpdateStarted, res.job_id);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      updatingPanel = false;
    }
  }

  async function runScan() {
    scanning = true;
    try {
      const res = await api.post<{ score: number; items: SecurityFinding[] }>('/security/scan');
      score = res.score;
      findings = res.items;
      toast.success(fmt(i18n.t.security.scanComplete, { score: res.score }));
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      scanning = false;
    }
  }

  async function changeFindingStatus(finding: SecurityFinding, nextStatus: SecurityFindingStatus) {
    if (finding.status === nextStatus) return;
    updatingFindingId = finding.id;
    try {
      const updated = await api.patch<SecurityFinding>(`/security/findings/${finding.id}`, {
        status: nextStatus,
      });
      findings = findings.map((f) => (f.id === finding.id ? updated : f));
      toast.success(i18n.t.security.statusUpdated);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      updatingFindingId = null;
    }
  }

  async function applySecurityUpdates(dryRun: boolean) {
    applyingUpdates = true;
    try {
      const res = await api.post<{ job_id: string }>('/updates/apply', {
        mode: 'security',
        dry_run: dryRun,
      });
      notifyJob(
        dryRun ? i18n.t.security.dryRunStarted : i18n.t.security.applyStarted,
        res.job_id
      );
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      applyingUpdates = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.security.title}</h1>
    <p class="page-subtitle">{i18n.t.security.subtitle}</p>
  </div>
</div>

{#if isAdmin}
  <div class="card">
    <div class="toolbar">
      <div>
        <h2 class="card-title">{i18n.t.security.panelUpdateTitle}</h2>
        <p class="page-subtitle">{i18n.t.security.panelUpdateSubtitle}</p>
      </div>
      {#if panelUpdate}
        <span class="badge {panelUpdate.update_available ? 'badge-warning' : 'badge-success'}">
          {panelUpdate.update_available ? i18n.t.security.updateAvailable : i18n.t.security.upToDate}
        </span>
      {/if}
    </div>

    <div class="grid-two">
      <div class="form-group">
        <div class="stat-label">{i18n.t.security.currentVersion}</div>
        <div class="stat-value mono">
          {panelUpdate ? panelUpdate.current_version : loadingPanelUpdate ? i18n.t.common.loading : '—'}
        </div>
      </div>
      <div class="form-group">
        <div class="stat-label">{i18n.t.security.latestVersion}</div>
        <div class="stat-value mono">
          {panelUpdate ? panelUpdate.latest_version : loadingPanelUpdate ? i18n.t.common.loading : '—'}
        </div>
      </div>
    </div>

    <div class="alert alert-info" role="note">
      {i18n.t.security.panelUpdatePreserveNote}
    </div>

    <div class="table-actions">
      <button
        type="button"
        class="btn btn-primary"
        onclick={applyPanelUpdate}
        disabled={loadingPanelUpdate || updatingPanel || !panelUpdate?.update_available}
      >
        {updatingPanel ? i18n.t.security.updatingPanel : i18n.t.security.updateInPlace}
      </button>
      <button
        type="button"
        class="btn"
        onclick={loadPanelUpdate}
        disabled={loadingPanelUpdate || updatingPanel}
      >
        {loadingPanelUpdate ? i18n.t.security.checkingUpdates : i18n.t.security.checkForUpdates}
      </button>
      {#if panelUpdate?.release_url}
        <a
          class="btn btn-ghost"
          href={panelUpdate.release_url}
          target="_blank"
          rel="noopener noreferrer"
        >
          {i18n.t.security.releaseNotes}
        </a>
      {/if}
    </div>
  </div>
{/if}

<div class="card">
  <div class="toolbar">
    <div>
      <h2 class="card-title">{i18n.t.lockdown.title}</h2>
      <p class="page-subtitle">{i18n.t.lockdown.subtitle}</p>
    </div>
    <span class="badge {lockdownEnabled ? 'badge-danger' : 'badge-success'}">
      {lockdownEnabled ? i18n.t.lockdown.enabledBadge : i18n.t.lockdown.disabledBadge}
    </span>
  </div>

  <div class="form-group">
    <label class="checkbox-row">
      <input type="checkbox" bind:checked={lockdownEnabled} disabled={loadingLockdown} />
      <span><strong>{i18n.t.lockdown.toggleLabel}</strong></span>
    </label>
  </div>

  <div class="form-group">
    <label class="form-label" for="lockdown-ips">{i18n.t.lockdown.allowedIps}</label>
    <textarea
      id="lockdown-ips"
      class="form-control mono"
      rows="3"
      bind:value={lockdownAllowedIpsText}
      disabled={loadingLockdown}
      placeholder={i18n.t.lockdown.allowedIpsPlaceholder}
    ></textarea>
  </div>

  <div class="table-actions">
    <button
      type="button"
      class="btn {lockdownEnabled ? 'btn-danger' : 'btn-primary'}"
      onclick={() => (lockdownConfirmOpen = true)}
      disabled={loadingLockdown || savingLockdown}
    >
      {i18n.t.lockdown.applyButton}
    </button>
    <button type="button" class="btn btn-sm" onclick={loadLockdown} disabled={loadingLockdown}>
      {i18n.t.common.refresh}
    </button>
  </div>
</div>

<div class="card">
  <div class="toolbar">
    <div>
      <h2 class="card-title">{i18n.t.waf.title}</h2>
      <p class="page-subtitle">{i18n.t.waf.subtitle}</p>
    </div>
    <span class="badge {wafEnabled ? 'badge-success' : 'badge-warning'}">
      {wafEnabled ? i18n.t.waf.enabledBadge : i18n.t.waf.disabledBadge}
    </span>
  </div>

  <form onsubmit={saveWaf}>
    <div class="form-group">
      <label class="checkbox-row">
        <input type="checkbox" bind:checked={wafEnabled} disabled={loadingWaf} />
        <span><strong>{i18n.t.waf.toggleLabel}</strong></span>
      </label>
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="waf-engine">{i18n.t.waf.engine}</label>
        <select id="waf-engine" class="form-control" bind:value={wafEngine} disabled={loadingWaf}>
          <option value="coraza">{i18n.t.waf.engineCoraza}</option>
          <option value="modsecurity">{i18n.t.waf.engineModsecurity}</option>
        </select>
      </div>

      <div class="form-group">
        <label class="form-label" for="waf-mode">{i18n.t.waf.mode}</label>
        <select id="waf-mode" class="form-control" bind:value={wafMode} disabled={loadingWaf}>
          <option value="detection">{i18n.t.waf.modeDetection}</option>
          <option value="blocking">{i18n.t.waf.modeBlocking}</option>
        </select>
      </div>

      <div class="form-group">
        <label class="form-label" for="waf-paranoia">{i18n.t.waf.paranoiaLevel}</label>
        <select id="waf-paranoia" class="form-control" bind:value={wafParanoia} disabled={loadingWaf}>
          <option value={1}>{i18n.t.waf.paranoia1}</option>
          <option value={2}>{i18n.t.waf.paranoia2}</option>
          <option value={3}>{i18n.t.waf.paranoia3}</option>
          <option value={4}>{i18n.t.waf.paranoia4}</option>
        </select>
      </div>
    </div>

    <div class="table-actions">
      <button type="submit" class="btn btn-primary" disabled={loadingWaf || savingWaf}>
        {savingWaf ? i18n.t.common.loading : i18n.t.waf.saveButton}
      </button>
      <button type="button" class="btn btn-sm" onclick={loadWaf} disabled={loadingWaf}>
        {i18n.t.common.refresh}
      </button>
    </div>
  </form>
</div>

<div class="card">
  <div class="score-header">
    <div>
      <div class="stat-label">{i18n.t.security.scoreTitle}</div>
      <div class="score-value-row">
        <span class="stat-value">{fmt(i18n.t.security.scoreOutOf, { score })}</span>
        <span class="badge {scoreBadgeClass}">{score}%</span>
      </div>
    </div>
    <button type="button" class="btn btn-primary" onclick={runScan} disabled={scanning}>
      {scanning ? i18n.t.security.scanning : i18n.t.security.runScan}
    </button>
  </div>
  <progress class="score-progress {scoreTierClass}" max="100" value={score}></progress>
</div>

<div class="card">
  <div class="toolbar">
    <h2 class="card-title">{i18n.t.security.findingsTitle}</h2>
    <button type="button" class="btn btn-sm" onclick={loadFindings} disabled={loadingFindings}>
      {i18n.t.common.refresh}
    </button>
  </div>

  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.security.severity}</th>
          <th>{i18n.t.security.category}</th>
          <th>{i18n.t.security.component}</th>
          <th>{i18n.t.security.finding}</th>
          <th>{i18n.t.security.remediation}</th>
          <th>{i18n.t.common.status}</th>
        </tr>
      </thead>
      <tbody>
        {#if loadingFindings && findings.length === 0}
          <tr><td colspan="6" class="empty-state">{i18n.t.common.loading}</td></tr>
        {:else if findings.length === 0}
          <tr><td colspan="6" class="empty-state">{i18n.t.common.noData}</td></tr>
        {:else}
          {#each findings as f (f.id)}
            <tr>
              <td>
                <span class="badge {severityBadge(f.severity)}">{severityLabel(f.severity)}</span>
              </td>
              <td><span class="badge">{f.category}</span></td>
              <td class="mono">{f.component || '—'}</td>
              <td>
                <div><strong>{f.title}</strong></div>
                {#if f.description}
                  <div class="finding-desc">{f.description}</div>
                {/if}
              </td>
              <td>{f.remediation || '—'}</td>
              <td>
                <select
                  class="form-control"
                  aria-label={f.title}
                  value={f.status}
                  disabled={updatingFindingId === f.id}
                  onchange={(e) =>
                    changeFindingStatus(
                      f,
                      (e.currentTarget as HTMLSelectElement).value as SecurityFindingStatus
                    )}
                >
                  <option value="open">{i18n.t.security.statusOpen}</option>
                  <option value="resolved">{i18n.t.security.statusResolved}</option>
                  <option value="accepted">{i18n.t.security.statusAccepted}</option>
                </select>
              </td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
</div>

<div class="card">
  <div class="toolbar">
    <h2 class="card-title">{i18n.t.security.updatesTitle}</h2>
    <div class="table-actions">
      <button
        type="button"
        class="btn btn-sm"
        onclick={() => applySecurityUpdates(true)}
        disabled={applyingUpdates}
      >
        {i18n.t.security.dryRunSecurity}
      </button>
      <button
        type="button"
        class="btn btn-sm btn-primary"
        onclick={() => applySecurityUpdates(false)}
        disabled={applyingUpdates}
      >
        {i18n.t.security.applySecurity}
      </button>
      <button type="button" class="btn btn-sm" onclick={loadUpdates} disabled={loadingUpdates}>
        {i18n.t.common.refresh}
      </button>
    </div>
  </div>

  {#if rebootRequired}
    <div class="alert alert-warning" role="status">{i18n.t.security.rebootRequired}</div>
  {/if}

  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.security.pkgName}</th>
          <th>{i18n.t.security.pkgCurrent}</th>
          <th>{i18n.t.security.pkgCandidate}</th>
          <th>{i18n.t.security.pkgCategory}</th>
        </tr>
      </thead>
      <tbody>
        {#if loadingUpdates && updates.length === 0}
          <tr><td colspan="4" class="empty-state">{i18n.t.common.loading}</td></tr>
        {:else if updates.length === 0}
          <tr><td colspan="4" class="empty-state">{i18n.t.common.noData}</td></tr>
        {:else}
          {#each updates as pkg (pkg.package)}
            <tr>
              <td class="mono"><strong>{pkg.package}</strong></td>
              <td class="mono">{pkg.current}</td>
              <td class="mono">{pkg.candidate}</td>
              <td>
                <span class="badge {updateCategoryBadge(pkg.category)}">
                  {updateCategoryLabel(pkg.category)}
                </span>
              </td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
</div>

<Modal
  open={lockdownConfirmOpen}
  title={lockdownEnabled ? i18n.t.lockdown.confirmEnableTitle : i18n.t.lockdown.confirmDisableTitle}
  onclose={() => (lockdownConfirmOpen = false)}
>
  <p>
    {lockdownEnabled ? i18n.t.lockdown.confirmEnableBody : i18n.t.lockdown.confirmDisableBody}
  </p>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (lockdownConfirmOpen = false)}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn {lockdownEnabled ? 'btn-danger' : 'btn-primary'}"
      onclick={confirmLockdown}
      disabled={savingLockdown}
    >
      {savingLockdown ? i18n.t.common.loading : i18n.t.common.confirm}
    </button>
  {/snippet}
</Modal>

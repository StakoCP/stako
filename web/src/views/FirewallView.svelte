<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type {
    BannedIP,
    FirewallAction,
    FirewallProtocol,
    FirewallRule,
    PaginatedResponse,
  } from '../lib/types';

  let rules = $state<FirewallRule[]>([]);
  let bans = $state<BannedIP[]>([]);
  let loadingRules = $state(true);
  let loadingBans = $state(true);

  // Add Rule modal
  let formOpen = $state(false);
  let fAction = $state<FirewallAction>('allow');
  let fProtocol = $state<FirewallProtocol>('tcp');
  let fPort = $state('');
  let fSourceCidr = $state('');
  let fComment = $state('');
  let formError = $state('');
  let saving = $state(false);

  // Delete Rule modal
  let deleteTarget = $state<FirewallRule | null>(null);
  let deleting = $state(false);

  // Unban tracking
  let unbanningIp = $state<string | null>(null);

  function notifyJob(message: string, jobId?: string | null) {
    if (jobId) {
      toast.success(`${message} (${fmt(i18n.t.firewall.jobToast, { jobId })})`, {
        href: `#/jobs/${jobId}`,
        label: i18n.t.firewall.viewJob,
      });
    } else {
      toast.success(message);
    }
  }

  async function loadRules() {
    loadingRules = true;
    try {
      const res = await api.get<PaginatedResponse<FirewallRule>>('/firewall/rules');
      rules = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingRules = false;
    }
  }

  async function loadBans() {
    loadingBans = true;
    try {
      const res = await api.get<{ items: BannedIP[] }>('/firewall/bans');
      bans = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingBans = false;
    }
  }

  onMount(() => {
    loadRules();
    loadBans();
  });

  function openCreate() {
    fAction = 'allow';
    fProtocol = 'tcp';
    fPort = '';
    fSourceCidr = '';
    fComment = '';
    formError = '';
    formOpen = true;
  }

  async function submitForm(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    saving = true;
    try {
      const body: Record<string, unknown> = {
        action: fAction,
        protocol: fProtocol,
        port: fPort.trim(),
      };
      if (fSourceCidr.trim()) body.source_cidr = fSourceCidr.trim();
      if (fComment.trim()) body.comment = fComment.trim();

      const res = await api.post<{ rule: FirewallRule; job_id: string }>('/firewall/rules', body);
      notifyJob(i18n.t.firewall.ruleCreated, res.job_id);
      formOpen = false;
      await loadRules();
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

  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    try {
      const res = await api.delete<{ job_id: string }>(`/firewall/rules/${deleteTarget.id}`);
      notifyJob(i18n.t.firewall.ruleDeleted, res?.job_id);
      deleteTarget = null;
      await loadRules();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      deleteTarget = null;
    } finally {
      deleting = false;
    }
  }

  async function unbanIp(ban: BannedIP) {
    unbanningIp = `${ban.ip}:${ban.jail}`;
    try {
      const body: Record<string, string> = { ip: ban.ip };
      if (ban.jail) body.jail = ban.jail;
      const res = await api.post<{ job_id: string }>('/firewall/bans/unban', body);
      notifyJob(fmt(i18n.t.firewall.unbanSuccess, { ip: ban.ip }), res.job_id);
      await loadBans();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      unbanningIp = null;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.firewall.title}</h1>
    <p class="page-subtitle">{i18n.t.firewall.subtitle}</p>
  </div>
  <button type="button" class="btn btn-primary" onclick={openCreate}>
    + {i18n.t.firewall.addRule}
  </button>
</div>

<div class="card">
  <div class="toolbar">
    <h2 class="card-title">{i18n.t.firewall.rulesTitle}</h2>
    <button type="button" class="btn btn-sm" onclick={loadRules} disabled={loadingRules}>
      {i18n.t.common.refresh}
    </button>
  </div>

  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.firewall.action}</th>
          <th>{i18n.t.firewall.protocol}</th>
          <th>{i18n.t.firewall.port}</th>
          <th>{i18n.t.firewall.sourceCidr}</th>
          <th>{i18n.t.firewall.comment}</th>
          <th>{i18n.t.common.actions}</th>
        </tr>
      </thead>
      <tbody>
        {#if loadingRules && rules.length === 0}
          <tr><td colspan="6" class="empty-state">{i18n.t.common.loading}</td></tr>
        {:else if rules.length === 0}
          <tr><td colspan="6" class="empty-state">{i18n.t.common.noData}</td></tr>
        {:else}
          {#each rules as rule (rule.id)}
            <tr>
              <td>
                <span class="badge {rule.action === 'allow' ? 'badge-success' : 'badge-danger'}">
                  {rule.action === 'allow' ? i18n.t.firewall.allow : i18n.t.firewall.deny}
                </span>
              </td>
              <td class="mono">{rule.protocol}</td>
              <td class="mono"><strong>{rule.port}</strong></td>
              <td class="mono">{rule.source_cidr || '0.0.0.0/0'}</td>
              <td>{rule.comment || '—'}</td>
              <td>
                <div class="table-actions">
                  <button
                    type="button"
                    class="btn btn-sm btn-icon btn-danger"
                    title={i18n.t.common.delete}
                    aria-label={i18n.t.common.delete}
                    onclick={() => (deleteTarget = rule)}
                  >
                    <ActionIcon name="delete" />
                  </button>
                </div>
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
    <h2 class="card-title">{i18n.t.firewall.bansTitle}</h2>
    <button type="button" class="btn btn-sm" onclick={loadBans} disabled={loadingBans}>
      {i18n.t.common.refresh}
    </button>
  </div>

  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.firewall.ip}</th>
          <th>{i18n.t.firewall.jail}</th>
          <th>{i18n.t.firewall.reason}</th>
          <th>{i18n.t.firewall.bannedAt}</th>
          <th>{i18n.t.common.actions}</th>
        </tr>
      </thead>
      <tbody>
        {#if loadingBans && bans.length === 0}
          <tr><td colspan="5" class="empty-state">{i18n.t.common.loading}</td></tr>
        {:else if bans.length === 0}
          <tr><td colspan="5" class="empty-state">{i18n.t.common.noData}</td></tr>
        {:else}
          {#each bans as ban (`${ban.ip}:${ban.jail}`)}
            <tr>
              <td class="mono"><strong>{ban.ip}</strong></td>
              <td><span class="badge badge-warning">{ban.jail}</span></td>
              <td>{ban.reason || '—'}</td>
              <td>{formatDateTime(ban.banned_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</td>
              <td>
                <div class="table-actions">
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={unbanningIp === `${ban.ip}:${ban.jail}`
                      ? i18n.t.common.loading
                      : i18n.t.firewall.unban}
                    aria-label={unbanningIp === `${ban.ip}:${ban.jail}`
                      ? i18n.t.common.loading
                      : i18n.t.firewall.unban}
                    onclick={() => unbanIp(ban)}
                    disabled={unbanningIp === `${ban.ip}:${ban.jail}`}
                  >
                    <ActionIcon name="unban" />
                  </button>
                </div>
              </td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
</div>

<Modal open={formOpen} title={i18n.t.firewall.addRule} onclose={() => (formOpen = false)}>
  <form id="fw-form" onsubmit={submitForm}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="fw-action">{i18n.t.firewall.action}</label>
        <select id="fw-action" class="form-control" bind:value={fAction}>
          <option value="allow">{i18n.t.firewall.allow}</option>
          <option value="deny">{i18n.t.firewall.deny}</option>
        </select>
      </div>
      <div class="form-group">
        <label class="form-label" for="fw-protocol">{i18n.t.firewall.protocol}</label>
        <select id="fw-protocol" class="form-control" bind:value={fProtocol}>
          <option value="tcp">TCP</option>
          <option value="udp">UDP</option>
          <option value="any">Any</option>
        </select>
      </div>
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="fw-port">{i18n.t.firewall.port}</label>
        <input
          id="fw-port"
          type="text"
          class="form-control"
          bind:value={fPort}
          required
          placeholder={i18n.t.firewall.portPlaceholder}
        />
      </div>
      <div class="form-group">
        <label class="form-label" for="fw-cidr">{i18n.t.firewall.sourceCidr}</label>
        <input
          id="fw-cidr"
          type="text"
          class="form-control"
          bind:value={fSourceCidr}
          placeholder={i18n.t.firewall.sourceCidrPlaceholder}
        />
      </div>
    </div>

    <div class="form-group">
      <label class="form-label" for="fw-comment">{i18n.t.firewall.comment}</label>
      <input
        id="fw-comment"
        type="text"
        class="form-control"
        bind:value={fComment}
        placeholder="HTTPS"
      />
    </div>
  </form>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (formOpen = false)} disabled={saving}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="fw-form" class="btn btn-primary" disabled={saving}>
      {saving ? i18n.t.common.loading : i18n.t.common.save}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.firewall.deleteConfirmTitle}
  onclose={() => (deleteTarget = null)}
>
  {#if deleteTarget}
    <p>
      {fmt(i18n.t.firewall.deleteConfirmBody, {
        action: deleteTarget.action,
        protocol: deleteTarget.protocol,
        port: deleteTarget.port,
      })}
    </p>
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

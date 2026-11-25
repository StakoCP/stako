<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { Domain, GitDeploy, PaginatedResponse } from '../lib/types';

  let deploys = $state<GitDeploy[]>([]);
  let domains = $state<Domain[]>([]);
  let loading = $state(true);
  let triggeringId = $state<string | null>(null);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let localeCode = $derived(i18n.locale === 'id' ? 'id-ID' : 'en-US');

  let domainNameById = $derived(
    new Map(domains.map((d) => [d.id, d.domain]))
  );

  // Create modal
  let createOpen = $state(false);
  let fDomainId = $state('');
  let fRepoUrl = $state('');
  let fBranch = $state('main');
  let fBuildCmd = $state('');
  let formError = $state('');
  let saving = $state(false);

  // One-time webhook_secret modal
  let createdSecret = $state<string | null>(null);
  let copiedSecret = $state(false);

  // Delete modal
  let deleteTarget = $state<GitDeploy | null>(null);
  let deleting = $state(false);

  async function loadDeploys() {
    loading = true;
    try {
      const res = await api.get<{ items: GitDeploy[] }>('/git-deploys');
      deploys = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
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

  onMount(() => {
    loadDeploys();
    loadDomains();
  });

  function openCreate() {
    fDomainId = domains[0]?.id ?? '';
    fRepoUrl = '';
    fBranch = 'main';
    fBuildCmd = '';
    formError = '';
    createOpen = true;
  }

  async function submitCreate(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    saving = true;
    try {
      const body: Record<string, unknown> = {
        domain_id: fDomainId,
        repo_url: fRepoUrl.trim(),
      };
      if (fBranch.trim()) body.branch = fBranch.trim();
      if (fBuildCmd.trim()) body.build_cmd = fBuildCmd.trim();

      const res = await api.post<{ deploy: GitDeploy; webhook_secret: string }>(
        '/git-deploys',
        body
      );
      toast.success(i18n.t.gitDeploy.createdSuccess);
      createOpen = false;
      createdSecret = res.webhook_secret;
      copiedSecret = false;
      await loadDeploys();
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

  async function triggerDeploy(deploy: GitDeploy) {
    triggeringId = deploy.id;
    try {
      const res = await api.post<{ job_id: string }>(`/git-deploys/${deploy.id}/trigger`);
      if (res.job_id) {
        toast.success(fmt(i18n.t.gitDeploy.triggeredToast, { jobId: res.job_id }), {
          href: `#/jobs/${res.job_id}`,
          label: i18n.t.gitDeploy.viewJob,
        });
      }
      await loadDeploys();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      triggeringId = null;
    }
  }

  async function copySecret() {
    if (!createdSecret) return;
    try {
      await navigator.clipboard.writeText(createdSecret);
      copiedSecret = true;
      toast.success(i18n.t.common.copied);
    } catch {
      toast.error(i18n.t.common.errorGeneric);
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    try {
      await api.delete(`/git-deploys/${deleteTarget.id}`);
      toast.success(i18n.t.gitDeploy.deletedSuccess);
      deleteTarget = null;
      await loadDeploys();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      deleting = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.gitDeploy.title}</h1>
    <p class="page-subtitle">{i18n.t.gitDeploy.subtitle}</p>
  </div>
  <div class="table-actions">
    <button type="button" class="btn" onclick={loadDeploys} disabled={loading}>
      {i18n.t.common.refresh}
    </button>
    {#if canWrite}
      <button type="button" class="btn btn-primary" onclick={openCreate}>
        {i18n.t.gitDeploy.add}
      </button>
    {/if}
  </div>
</div>

<div class="card">
  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.gitDeploy.domain}</th>
          <th>{i18n.t.gitDeploy.repoUrl}</th>
          <th>{i18n.t.gitDeploy.branch}</th>
          <th>{i18n.t.gitDeploy.buildCmd}</th>
          <th>{i18n.t.gitDeploy.lastDeployed}</th>
          {#if canWrite}
            <th>{i18n.t.common.actions}</th>
          {/if}
        </tr>
      </thead>
      <tbody>
        {#if loading && deploys.length === 0}
          <tr>
            <td colspan={canWrite ? 6 : 5} class="empty-state">{i18n.t.common.loading}</td>
          </tr>
        {:else if deploys.length === 0}
          <tr>
            <td colspan={canWrite ? 6 : 5} class="empty-state">{i18n.t.common.noData}</td>
          </tr>
        {:else}
          {#each deploys as d (d.id)}
            <tr>
              <td class="mono">{domainNameById.get(d.domain_id) ?? d.domain_id}</td>
              <td class="mono">{d.repo_url}</td>
              <td><span class="badge badge-info mono">{d.branch}</span></td>
              <td class="mono">{d.build_cmd || '—'}</td>
              <td>
                {d.last_deployed_at
                  ? formatDateTime(d.last_deployed_at, localeCode)
                  : i18n.t.gitDeploy.never}
              </td>
              {#if canWrite}
                <td>
                  <div class="table-actions">
                    <button
                      type="button"
                      class="btn btn-sm btn-icon btn-primary"
                      title={triggeringId === d.id
                        ? i18n.t.gitDeploy.triggering
                        : i18n.t.gitDeploy.trigger}
                      aria-label={triggeringId === d.id
                        ? i18n.t.gitDeploy.triggering
                        : i18n.t.gitDeploy.trigger}
                      onclick={() => triggerDeploy(d)}
                      disabled={triggeringId === d.id}
                    >
                      <ActionIcon name="deploy" />
                    </button>
                    <button
                      type="button"
                      class="btn btn-sm btn-icon btn-danger"
                      title={i18n.t.common.delete}
                      aria-label={i18n.t.common.delete}
                      onclick={() => (deleteTarget = d)}
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
</div>

<Modal
  open={createOpen}
  title={i18n.t.gitDeploy.add}
  onclose={() => (createOpen = false)}
>
  {#if formError}
    <div class="alert alert-danger" role="alert">{formError}</div>
  {/if}
  <form id="git-deploy-form" onsubmit={submitCreate}>
    <div class="form-group">
      <label class="form-label" for="gd-domain">{i18n.t.gitDeploy.domain}</label>
      {#if domains.length === 0}
        <p class="page-subtitle">{i18n.t.gitDeploy.noDomainsHint}</p>
      {:else}
        <select id="gd-domain" class="form-control" bind:value={fDomainId} required>
          <option value="" disabled>{i18n.t.gitDeploy.selectDomain}</option>
          {#each domains as dom (dom.id)}
            <option value={dom.id}>{dom.domain}</option>
          {/each}
        </select>
      {/if}
    </div>

    <div class="form-group">
      <label class="form-label" for="gd-repo">{i18n.t.gitDeploy.repoUrl}</label>
      <input
        id="gd-repo"
        type="text"
        class="form-control mono"
        bind:value={fRepoUrl}
        required
        placeholder={i18n.t.gitDeploy.repoUrlPlaceholder}
      />
    </div>

    <div class="form-group">
      <label class="form-label" for="gd-branch">{i18n.t.gitDeploy.branch}</label>
      <input
        id="gd-branch"
        type="text"
        class="form-control mono"
        bind:value={fBranch}
        placeholder={i18n.t.gitDeploy.branchPlaceholder}
      />
    </div>

    <div class="form-group">
      <label class="form-label" for="gd-build">{i18n.t.gitDeploy.buildCmd}</label>
      <input
        id="gd-build"
        type="text"
        class="form-control mono"
        bind:value={fBuildCmd}
        placeholder={i18n.t.gitDeploy.buildCmdPlaceholder}
      />
    </div>
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (createOpen = false)}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="submit"
      form="git-deploy-form"
      class="btn btn-primary"
      disabled={saving || domains.length === 0}
    >
      {saving ? i18n.t.common.loading : i18n.t.common.create}
    </button>
  {/snippet}
</Modal>

<Modal
  open={createdSecret !== null}
  title={i18n.t.gitDeploy.secretOnceTitle}
  onclose={() => (createdSecret = null)}
>
  <p>{i18n.t.gitDeploy.secretOnceDesc}</p>
  <div class="secret-box">
    <span>{createdSecret}</span>
    <button type="button" class="btn btn-sm" onclick={copySecret}>
      {copiedSecret ? i18n.t.common.copied : i18n.t.common.copy}
    </button>
  </div>

  {#snippet footer()}
    <button type="button" class="btn btn-primary" onclick={() => (createdSecret = null)}>
      {i18n.t.common.close}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.gitDeploy.deleteTitle}
  onclose={() => (deleteTarget = null)}
>
  <p>{fmt(i18n.t.gitDeploy.deleteBody, { repo: deleteTarget?.repo_url ?? '' })}</p>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (deleteTarget = null)}>
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

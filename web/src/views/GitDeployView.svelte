<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { Domain, GitDeploy, GitPreview, PaginatedResponse } from '../lib/types';

  let deploys = $state<GitDeploy[]>([]);
  let previews = $state<GitPreview[]>([]);
  let domains = $state<Domain[]>([]);
  let loading = $state(true);
  let loadingPreviews = $state(true);
  let triggeringId = $state<string | null>(null);
  let redeployingPreviewId = $state<string | null>(null);
  let deletingPreviewId = $state<string | null>(null);

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

  // Create Preview modal
  let previewModalOpen = $state(false);
  let fPreviewDeployId = $state('');
  let fPreviewBranch = $state('');
  let fPreviewPR = $state('');
  let fPreviewImage = $state('');
  let fPreviewPort = $state('3000');
  let previewFormError = $state('');
  let savingPreview = $state(false);

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

  async function loadPreviews() {
    loadingPreviews = true;
    try {
      const res = await api.get<{ items: GitPreview[] }>('/git-previews');
      previews = res.items ?? [];
    } catch {
      previews = [];
    } finally {
      loadingPreviews = false;
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
    loadPreviews();
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

  function previewStatusClass(status: string): string {
    if (status === 'running') return 'badge-success';
    if (status === 'deploying') return 'badge-warning';
    if (status === 'failed') return 'badge-danger';
    return 'badge-info';
  }

  function openPreviewModal(deployId: string) {
    fPreviewDeployId = deployId || deploys[0]?.id || '';
    fPreviewBranch = '';
    fPreviewPR = '';
    fPreviewImage = '';
    fPreviewPort = '3000';
    previewFormError = '';
    previewModalOpen = true;
  }

  async function submitCreatePreview(e: SubmitEvent) {
    e.preventDefault();
    if (!fPreviewDeployId) return;
    previewFormError = '';
    savingPreview = true;
    try {
      const prNum = Number(fPreviewPR);
      const portNum = Number(fPreviewPort);
      await api.post<GitPreview>(`/git-deploys/${fPreviewDeployId}/previews`, {
        branch: fPreviewBranch.trim(),
        pr_number: Number.isFinite(prNum) && prNum > 0 ? prNum : 0,
        image: fPreviewImage.trim(),
        container_port: Number.isFinite(portNum) && portNum > 0 ? portNum : 3000,
      });
      toast.success(i18n.t.gitDeploy.previewCreated);
      previewModalOpen = false;
      await loadPreviews();
    } catch (err) {
      if (err instanceof ApiError) {
        const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
        previewFormError = fieldMsg || err.detail || err.title;
      } else {
        previewFormError = i18n.t.common.errorGeneric;
      }
    } finally {
      savingPreview = false;
    }
  }

  async function redeployPreview(preview: GitPreview) {
    redeployingPreviewId = preview.id;
    try {
      await api.post(`/git-previews/${preview.id}/redeploy`);
      toast.success(i18n.t.gitDeploy.previewRedeployed);
      await loadPreviews();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      redeployingPreviewId = null;
    }
  }

  async function deletePreview(preview: GitPreview) {
    deletingPreviewId = preview.id;
    try {
      await api.delete(`/git-previews/${preview.id}`);
      toast.success(i18n.t.gitDeploy.previewDeleted);
      await loadPreviews();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      deletingPreviewId = null;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.gitDeploy.title}</h1>
    <p class="page-subtitle">{i18n.t.gitDeploy.subtitle}</p>
  </div>
  <div class="table-actions">
    <button
      type="button"
      class="btn"
      onclick={() => {
        loadDeploys();
        loadPreviews();
      }}
      disabled={loading || loadingPreviews}
    >
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
                      class="btn btn-sm btn-icon"
                      title={i18n.t.gitDeploy.newPreview}
                      aria-label={i18n.t.gitDeploy.newPreview}
                      onclick={() => openPreviewModal(d.id)}
                    >
                      <ActionIcon name="proxy" />
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

<div class="card">
  <div class="card-header-row">
    <h2 class="card-title">{i18n.t.gitDeploy.previewsTitle}</h2>
    {#if canWrite}
      <button
        type="button"
        class="btn btn-sm btn-primary"
        onclick={() => openPreviewModal(deploys[0]?.id ?? '')}
        disabled={deploys.length === 0}
      >
        + {i18n.t.gitDeploy.newPreview}
      </button>
    {/if}
  </div>
  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.gitDeploy.previewSubdomain}</th>
          <th>{i18n.t.gitDeploy.previewBranch}</th>
          <th>{i18n.t.gitDeploy.previewContainer}</th>
          <th>{i18n.t.common.status}</th>
          <th>{i18n.t.gitDeploy.lastDeployed}</th>
          {#if canWrite}
            <th>{i18n.t.common.actions}</th>
          {/if}
        </tr>
      </thead>
      <tbody>
        {#if loadingPreviews && previews.length === 0}
          <tr>
            <td colspan={canWrite ? 6 : 5} class="empty-state">{i18n.t.common.loading}</td>
          </tr>
        {:else if previews.length === 0}
          <tr>
            <td colspan={canWrite ? 6 : 5} class="empty-state">{i18n.t.gitDeploy.noPreviews}</td>
          </tr>
        {:else}
          {#each previews as p (p.id)}
            <tr>
              <td class="mono">
                <a href={`https://${p.subdomain}`} target="_blank" rel="noopener">
                  https://{p.subdomain}
                </a>
              </td>
              <td>
                <div class="table-actions">
                  {#if p.pr_number > 0}
                    <span class="badge badge-primary mono">PR #{p.pr_number}</span>
                  {/if}
                  <span class="badge badge-info mono">{p.branch}</span>
                </div>
              </td>
              <td class="mono">
                <div>{p.container_name || '—'}</div>
                <div class="page-subtitle">127.0.0.1:{p.host_port} -&gt; {p.container_port}</div>
              </td>
              <td>
                <span class={`badge ${previewStatusClass(p.status)}`}>{p.status}</span>
              </td>
              <td>
                {p.last_deployed_at
                  ? formatDateTime(p.last_deployed_at, localeCode)
                  : i18n.t.gitDeploy.never}
              </td>
              {#if canWrite}
                <td>
                  <div class="table-actions">
                    <button
                      type="button"
                      class="btn btn-sm btn-icon btn-primary"
                      title={i18n.t.gitDeploy.previewRedeploy}
                      aria-label={i18n.t.gitDeploy.previewRedeploy}
                      onclick={() => redeployPreview(p)}
                      disabled={redeployingPreviewId === p.id}
                    >
                      <ActionIcon name="restart" />
                    </button>
                    <button
                      type="button"
                      class="btn btn-sm btn-icon btn-danger"
                      title={i18n.t.common.delete}
                      aria-label={i18n.t.common.delete}
                      onclick={() => deletePreview(p)}
                      disabled={deletingPreviewId === p.id}
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

<Modal
  open={previewModalOpen}
  title={i18n.t.gitDeploy.newPreview}
  onclose={() => (previewModalOpen = false)}
>
  {#if previewFormError}
    <div class="alert alert-danger" role="alert">{previewFormError}</div>
  {/if}
  <form id="git-preview-form" onsubmit={submitCreatePreview}>
    <div class="form-group">
      <label class="form-label" for="gp-deploy">{i18n.t.gitDeploy.title}</label>
      <select id="gp-deploy" class="form-control" bind:value={fPreviewDeployId} required>
        {#each deploys as dep (dep.id)}
          <option value={dep.id}>
            {domainNameById.get(dep.domain_id) ?? dep.domain_id} — {dep.repo_url}
          </option>
        {/each}
      </select>
    </div>

    <div class="form-group">
      <label class="form-label" for="gp-branch">{i18n.t.gitDeploy.previewBranch}</label>
      <input
        id="gp-branch"
        type="text"
        class="form-control mono"
        bind:value={fPreviewBranch}
        required
        placeholder="feat/my-branch"
      />
    </div>

    <div class="form-group">
      <label class="form-label" for="gp-pr">{i18n.t.gitDeploy.previewPR}</label>
      <input
        id="gp-pr"
        type="number"
        min="0"
        class="form-control mono"
        bind:value={fPreviewPR}
        placeholder="42"
      />
    </div>

    <div class="form-group">
      <label class="form-label" for="gp-image">{i18n.t.gitDeploy.previewImage}</label>
      <input
        id="gp-image"
        type="text"
        class="form-control mono"
        bind:value={fPreviewImage}
        placeholder="node:20-alpine"
      />
    </div>

    <div class="form-group">
      <label class="form-label" for="gp-port">{i18n.t.gitDeploy.previewPort}</label>
      <input
        id="gp-port"
        type="number"
        min="1"
        max="65535"
        class="form-control mono"
        bind:value={fPreviewPort}
        required
      />
    </div>
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (previewModalOpen = false)}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="submit"
      form="git-preview-form"
      class="btn btn-primary"
      disabled={savingPreview || !fPreviewDeployId}
    >
      {savingPreview ? i18n.t.common.loading : i18n.t.common.create}
    </button>
  {/snippet}
</Modal>

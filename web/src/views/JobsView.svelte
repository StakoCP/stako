<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import Pagination from '../components/Pagination.svelte';
  import { api, ApiError } from '../lib/api';
  import { formatDateTime } from '../lib/format';
  import { i18n } from '../lib/i18n.svelte';
  import { router } from '../lib/router.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { Job, JobStatus, PaginatedResponse } from '../lib/types';

  interface Props {
    jobId?: string;
  }

  let { jobId }: Props = $props();

  let jobs = $state<Job[]>([]);
  let total = $state(0);
  let limit = 25;
  let offset = $state(0);
  let detailJob = $state<Job | null>(null);
  let loading = $state(true);
  let timer: number | null = null;

  function statusLabel(status: JobStatus): string {
    switch (status) {
      case 'queued':
        return i18n.t.jobs.statusQueued;
      case 'running':
        return i18n.t.jobs.statusRunning;
      case 'succeeded':
        return i18n.t.jobs.statusSucceeded;
      case 'failed':
        return i18n.t.jobs.statusFailed;
    }
  }

  function schedulePoll(active: boolean) {
    if (timer !== null) {
      window.clearTimeout(timer);
      timer = null;
    }
    if (active) {
      timer = window.setTimeout(() => {
        if (jobId) {
          loadDetail(true);
        } else {
          loadList(true);
        }
      }, 3000);
    }
  }

  async function loadList(silent = false) {
    if (!silent) loading = true;
    try {
      const res = await api.get<PaginatedResponse<Job>>(`/jobs?limit=${limit}&offset=${offset}`);
      jobs = res.items;
      total = res.total;
      const hasActive = jobs.some((j) => j.status === 'queued' || j.status === 'running');
      schedulePoll(hasActive);
    } catch (err) {
      if (!silent) {
        toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      }
    } finally {
      loading = false;
    }
  }

  async function loadDetail(silent = false) {
    if (!jobId) return;
    if (!silent) loading = true;
    try {
      detailJob = await api.get<Job>(`/jobs/${encodeURIComponent(jobId)}`);
      const isActive = detailJob.status === 'queued' || detailJob.status === 'running';
      schedulePoll(isActive);
    } catch (err) {
      if (!silent) {
        toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      }
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    const id = jobId;
    if (timer !== null) {
      window.clearTimeout(timer);
      timer = null;
    }
    if (id) {
      detailJob = null;
      loadDetail();
    } else {
      loadList();
    }
  });

  onDestroy(() => {
    if (timer !== null) window.clearTimeout(timer);
  });
</script>

{#if jobId}
  <div class="page-header">
    <div>
      <h1 class="page-title">{i18n.t.jobs.detailTitle}</h1>
      <p class="page-subtitle mono">{jobId}</p>
    </div>
    <div class="table-actions">
      <a href="#/jobs" class="btn btn-sm">← {i18n.t.common.back}</a>
      <button type="button" class="btn btn-sm" onclick={() => loadDetail()}>
        {i18n.t.common.refresh}
      </button>
    </div>
  </div>

  {#if loading && !detailJob}
    <p>{i18n.t.common.loading}</p>
  {:else if detailJob}
    <div class="card">
      <dl class="kv-list">
        <dt>{i18n.t.jobs.id}</dt>
        <dd class="mono">{detailJob.id}</dd>
        <dt>{i18n.t.jobs.type}</dt>
        <dd class="mono">{detailJob.type}</dd>
        <dt>{i18n.t.common.status}</dt>
        <dd>
          <span
            class="badge"
            class:badge-success={detailJob.status === 'succeeded'}
            class:badge-danger={detailJob.status === 'failed'}
            class:badge-info={detailJob.status === 'running'}
            class:badge-warning={detailJob.status === 'queued'}
          >
            {statusLabel(detailJob.status)}
          </span>
        </dd>
        <dt>{i18n.t.jobs.createdBy}</dt>
        <dd class="mono">{detailJob.created_by}</dd>
        <dt>{i18n.t.common.createdAt}</dt>
        <dd>{formatDateTime(detailJob.created_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</dd>
        <dt>{i18n.t.jobs.startedAt}</dt>
        <dd>{formatDateTime(detailJob.started_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</dd>
        <dt>{i18n.t.jobs.finishedAt}</dt>
        <dd>{formatDateTime(detailJob.finished_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</dd>
      </dl>
    </div>

    {#if detailJob.error}
      <div class="alert alert-danger" role="alert">
        <strong>{i18n.t.jobs.error}:</strong> {detailJob.error}
      </div>
    {/if}

    <div class="card">
      <h2 class="card-title">{i18n.t.jobs.logOutput}</h2>
      <pre class="log-box">{detailJob.log || i18n.t.jobs.noLog}</pre>
    </div>
  {/if}
{:else}
  <div class="page-header">
    <div>
      <h1 class="page-title">{i18n.t.jobs.title}</h1>
      <p class="page-subtitle">{i18n.t.jobs.subtitle} · {i18n.t.jobs.autoRefresh}</p>
    </div>
    <button type="button" class="btn btn-sm" onclick={() => loadList()} disabled={loading}>
      {i18n.t.common.refresh}
    </button>
  </div>

  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.jobs.id}</th>
          <th>{i18n.t.jobs.type}</th>
          <th>{i18n.t.common.status}</th>
          <th>{i18n.t.common.createdAt}</th>
          <th>{i18n.t.jobs.finishedAt}</th>
          <th>{i18n.t.common.actions}</th>
        </tr>
      </thead>
      <tbody>
        {#if loading && jobs.length === 0}
          <tr><td colspan="6" class="empty-state">{i18n.t.common.loading}</td></tr>
        {:else if jobs.length === 0}
          <tr><td colspan="6" class="empty-state">{i18n.t.common.noData}</td></tr>
        {:else}
          {#each jobs as job (job.id)}
            <tr>
              <td class="mono"><a href={`#/jobs/${job.id}`}>{job.id}</a></td>
              <td class="mono">{job.type}</td>
              <td>
                <span
                  class="badge"
                  class:badge-success={job.status === 'succeeded'}
                  class:badge-danger={job.status === 'failed'}
                  class:badge-info={job.status === 'running'}
                  class:badge-warning={job.status === 'queued'}
                >
                  {statusLabel(job.status)}
                </span>
              </td>
              <td>{formatDateTime(job.created_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</td>
              <td>{formatDateTime(job.finished_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</td>
              <td>
                <button
                  type="button"
                  class="btn btn-sm"
                  onclick={() => router.navigate(`/jobs/${job.id}`)}
                >
                  {i18n.t.users.viewJob}
                </button>
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
      loadList();
    }}
  />
{/if}

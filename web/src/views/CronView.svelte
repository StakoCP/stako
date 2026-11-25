<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { CronJob } from '../lib/types';

  let jobs = $state<CronJob[]>([]);
  let loading = $state(true);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let localeCode = $derived(i18n.locale === 'id' ? 'id-ID' : 'en-US');

  // Create modal
  let createOpen = $state(false);
  let fSchedule = $state('0 2 * * *');
  let fCommand = $state('');
  let formError = $state('');
  let saving = $state(false);

  // Delete modal
  let deleteTarget = $state<CronJob | null>(null);
  let deleting = $state(false);

  async function loadCron() {
    loading = true;
    try {
      const res = await api.get<{ items: CronJob[] }>('/cron');
      jobs = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    loadCron();
  });

  function openCreate() {
    fSchedule = '0 2 * * *';
    fCommand = '';
    formError = '';
    createOpen = true;
  }

  async function submitCreate(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    saving = true;
    try {
      await api.post<CronJob>('/cron', {
        schedule: fSchedule.trim(),
        command: fCommand.trim(),
      });
      toast.success(i18n.t.cron.createdSuccess);
      createOpen = false;
      await loadCron();
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
      await api.delete(`/cron/${deleteTarget.id}`);
      toast.success(i18n.t.cron.deletedSuccess);
      deleteTarget = null;
      await loadCron();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      deleteTarget = null;
    } finally {
      deleting = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.cron.title}</h1>
    <p class="page-subtitle">{i18n.t.cron.subtitle}</p>
  </div>
  {#if canWrite}
    <button type="button" class="btn btn-primary" onclick={openCreate}>
      + {i18n.t.cron.createCron}
    </button>
  {/if}
</div>

<div class="toolbar">
  <div></div>
  <button type="button" class="btn btn-sm" onclick={loadCron} disabled={loading}>
    {i18n.t.common.refresh}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.cron.schedule}</th>
        <th>{i18n.t.cron.command}</th>
        <th>{i18n.t.common.createdAt}</th>
        {#if canWrite}
          <th>{i18n.t.common.actions}</th>
        {/if}
      </tr>
    </thead>
    <tbody>
      {#if loading && jobs.length === 0}
        <tr>
          <td colspan={canWrite ? 4 : 3} class="empty-state">{i18n.t.common.loading}</td>
        </tr>
      {:else if jobs.length === 0}
        <tr>
          <td colspan={canWrite ? 4 : 3} class="empty-state">{i18n.t.cron.noJobs}</td>
        </tr>
      {:else}
        {#each jobs as job (job.id)}
          <tr>
            <td class="mono">{job.schedule}</td>
            <td class="mono">{job.command}</td>
            <td>{formatDateTime(job.created_at, localeCode)}</td>
            {#if canWrite}
              <td>
                <button
                  type="button"
                  class="btn btn-sm btn-danger"
                  onclick={() => (deleteTarget = job)}
                >
                  {i18n.t.common.delete}
                </button>
              </td>
            {/if}
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>

<Modal open={createOpen} title={i18n.t.cron.createCron} onclose={() => (createOpen = false)}>
  <form onsubmit={submitCreate}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}
    <div class="form-group">
      <label class="form-label" for="cron-schedule">{i18n.t.cron.schedule}</label>
      <input
        id="cron-schedule"
        class="form-control mono"
        type="text"
        bind:value={fSchedule}
        placeholder={i18n.t.cron.schedulePlaceholder}
        required
      />
      <div class="form-hint">{i18n.t.cron.presets}:</div>
      <div class="preset-row">
        <button type="button" class="btn btn-sm" onclick={() => (fSchedule = '* * * * *')}>
          * * * * * ({i18n.t.cron.presetEveryMin})
        </button>
        <button type="button" class="btn btn-sm" onclick={() => (fSchedule = '0 * * * *')}>
          0 * * * * ({i18n.t.cron.presetHourly})
        </button>
        <button type="button" class="btn btn-sm" onclick={() => (fSchedule = '0 2 * * *')}>
          0 2 * * * ({i18n.t.cron.presetDaily2am})
        </button>
        <button type="button" class="btn btn-sm" onclick={() => (fSchedule = '0 0 * * 0')}>
          0 0 * * 0 ({i18n.t.cron.presetWeekly})
        </button>
      </div>
    </div>
    <div class="form-group">
      <label class="form-label" for="cron-command">{i18n.t.cron.command}</label>
      <input
        id="cron-command"
        class="form-control mono"
        type="text"
        bind:value={fCommand}
        placeholder={i18n.t.cron.commandPlaceholder}
        required
      />
    </div>
    <div class="modal-footer">
      <button type="button" class="btn" onclick={() => (createOpen = false)}>
        {i18n.t.common.cancel}
      </button>
      <button type="submit" class="btn btn-primary" disabled={saving}>
        {saving ? i18n.t.common.loading : i18n.t.common.create}
      </button>
    </div>
  </form>
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.cron.deleteTitle}
  onclose={() => (deleteTarget = null)}
>
  <p>{fmt(i18n.t.cron.deleteBody, { command: deleteTarget?.command ?? '' })}</p>
  <div class="modal-footer">
    <button type="button" class="btn" onclick={() => (deleteTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button type="button" class="btn btn-danger" onclick={confirmDelete} disabled={deleting}>
      {deleting ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  </div>
</Modal>

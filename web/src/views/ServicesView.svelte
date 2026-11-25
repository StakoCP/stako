<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { PaginatedResponse, RabbitVhost, RedisInstance, User } from '../lib/types';

  let redisList = $state<RedisInstance[]>([]);
  let rabbitList = $state<RabbitVhost[]>([]);
  let users = $state<User[]>([]);
  let loadingRedis = $state(true);
  let loadingRabbit = $state(true);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let canSelectOwner = $derived(auth.user?.role === 'admin' || auth.user?.role === 'reseller');

  // Create Redis modal
  let redisFormOpen = $state(false);
  let rName = $state('');
  let rMemoryMb = $state(256);
  let rPersistent = $state(false);
  let rUserId = $state('');
  let rFormError = $state('');
  let rSaving = $state(false);

  // Delete Redis modal
  let deleteRedisTarget = $state<RedisInstance | null>(null);
  let deletingRedis = $state(false);

  // Create RabbitMQ modal
  let rabbitFormOpen = $state(false);
  let mqVhost = $state('');
  let mqUsername = $state('');
  let mqPassword = $state('');
  let mqUserId = $state('');
  let mqFormError = $state('');
  let mqSaving = $state(false);

  // Delete RabbitMQ modal
  let deleteRabbitTarget = $state<RabbitVhost | null>(null);
  let deletingRabbit = $state(false);

  // One-time connection string modal
  let createdConnString = $state<string | null>(null);
  let copiedConn = $state(false);

  function notifyJob(message: string, jobId?: string | null) {
    if (jobId) {
      toast.success(`${message} (${fmt(i18n.t.services.jobToast, { jobId })})`, {
        href: `#/jobs/${jobId}`,
        label: i18n.t.services.viewJob,
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

  async function loadRedis() {
    loadingRedis = true;
    try {
      const res = await api.get<{ items: RedisInstance[] }>('/services/redis');
      redisList = res.items;
    } catch (err) {
      toast.error(formatApiError(err));
    } finally {
      loadingRedis = false;
    }
  }

  async function loadRabbit() {
    loadingRabbit = true;
    try {
      const res = await api.get<{ items: RabbitVhost[] }>('/services/rabbitmq');
      rabbitList = res.items;
    } catch (err) {
      toast.error(formatApiError(err));
    } finally {
      loadingRabbit = false;
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
    loadRedis();
    loadRabbit();
    loadUsers();
  });

  function openCreateRedis() {
    rName = '';
    rMemoryMb = 256;
    rPersistent = false;
    rUserId = auth.user?.id ?? '';
    rFormError = '';
    redisFormOpen = true;
  }

  async function submitRedis(e: SubmitEvent) {
    e.preventDefault();
    rFormError = '';
    rSaving = true;
    try {
      const body: Record<string, unknown> = {
        name: rName.trim(),
        memory_mb: Number(rMemoryMb) || 256,
        persistent: rPersistent,
      };
      if (canSelectOwner && rUserId) body.user_id = rUserId;

      const res = await api.post<{
        instance: RedisInstance;
        connection_string: string;
        job_id: string;
      }>('/services/redis', body);

      notifyJob(i18n.t.services.redisCreated, res.job_id);
      redisFormOpen = false;
      copiedConn = false;
      createdConnString = res.connection_string;
      await loadRedis();
    } catch (err) {
      rFormError = formatApiError(err);
    } finally {
      rSaving = false;
    }
  }

  async function confirmDeleteRedis() {
    if (!deleteRedisTarget) return;
    deletingRedis = true;
    try {
      const res = await api.delete<{ job_id: string }>(`/services/redis/${deleteRedisTarget.id}`);
      notifyJob(i18n.t.services.redisDeleted, res?.job_id);
      deleteRedisTarget = null;
      await loadRedis();
    } catch (err) {
      toast.error(formatApiError(err));
      deleteRedisTarget = null;
    } finally {
      deletingRedis = false;
    }
  }

  function openCreateRabbit() {
    mqVhost = '';
    mqUsername = '';
    mqPassword = '';
    mqUserId = auth.user?.id ?? '';
    mqFormError = '';
    rabbitFormOpen = true;
  }

  async function submitRabbit(e: SubmitEvent) {
    e.preventDefault();
    mqFormError = '';
    mqSaving = true;
    try {
      const body: Record<string, unknown> = {
        vhost: mqVhost.trim(),
        username: mqUsername.trim(),
        password: mqPassword,
      };
      if (canSelectOwner && mqUserId) body.user_id = mqUserId;

      const res = await api.post<{
        vhost: RabbitVhost;
        connection_string: string;
        job_id: string;
      }>('/services/rabbitmq', body);

      notifyJob(i18n.t.services.rabbitCreated, res.job_id);
      rabbitFormOpen = false;
      copiedConn = false;
      createdConnString = res.connection_string;
      await loadRabbit();
    } catch (err) {
      mqFormError = formatApiError(err);
    } finally {
      mqSaving = false;
    }
  }

  async function confirmDeleteRabbit() {
    if (!deleteRabbitTarget) return;
    deletingRabbit = true;
    try {
      const res = await api.delete<{ job_id: string }>(`/services/rabbitmq/${deleteRabbitTarget.id}`);
      notifyJob(i18n.t.services.rabbitDeleted, res?.job_id);
      deleteRabbitTarget = null;
      await loadRabbit();
    } catch (err) {
      toast.error(formatApiError(err));
      deleteRabbitTarget = null;
    } finally {
      deletingRabbit = false;
    }
  }

  async function copyConnString() {
    if (!createdConnString) return;
    await navigator.clipboard.writeText(createdConnString);
    copiedConn = true;
    setTimeout(() => (copiedConn = false), 2000);
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.services.title}</h1>
    <p class="page-subtitle">{i18n.t.services.subtitle}</p>
  </div>
</div>

<!-- Section 1: Redis Instances -->
<div class="card">
  <div class="toolbar">
    <h2 class="card-title">{i18n.t.services.redisTitle}</h2>
    <div class="table-actions">
      <button type="button" class="btn btn-sm" onclick={loadRedis} disabled={loadingRedis}>
        {i18n.t.common.refresh}
      </button>
      {#if canWrite}
        <button type="button" class="btn btn-sm btn-primary" onclick={openCreateRedis}>
          + {i18n.t.services.createRedis}
        </button>
      {/if}
    </div>
  </div>

  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.services.redisName}</th>
          <th>{i18n.t.services.port}</th>
          <th>{i18n.t.services.memoryMb}</th>
          <th>{i18n.t.services.persistent}</th>
          <th>{i18n.t.common.status}</th>
          <th>{i18n.t.common.createdAt}</th>
          {#if canWrite}
            <th>{i18n.t.common.actions}</th>
          {/if}
        </tr>
      </thead>
      <tbody>
        {#if loadingRedis && redisList.length === 0}
          <tr><td colspan={canWrite ? 7 : 6} class="empty-state">{i18n.t.common.loading}</td></tr>
        {:else if redisList.length === 0}
          <tr><td colspan={canWrite ? 7 : 6} class="empty-state">{i18n.t.common.noData}</td></tr>
        {:else}
          {#each redisList as rd (rd.id)}
            <tr>
              <td class="mono"><strong>{rd.name}</strong></td>
              <td class="mono">{rd.port}</td>
              <td class="mono">{rd.memory_mb} MB</td>
              <td>
                <span class="badge {rd.persistent ? 'badge-info' : ''}">
                  {rd.persistent ? i18n.t.services.persistentYes : i18n.t.services.persistentNo}
                </span>
              </td>
              <td>
                <span class="badge {rd.status === 'running' ? 'badge-success' : 'badge-warning'}">
                  {rd.status}
                </span>
              </td>
              <td>{formatDateTime(rd.created_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</td>
              {#if canWrite}
                <td>
                  <div class="table-actions">
                    <button
                      type="button"
                      class="btn btn-sm btn-icon btn-danger"
                      title={i18n.t.common.delete}
                      aria-label={i18n.t.common.delete}
                      onclick={() => (deleteRedisTarget = rd)}
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

<!-- Section 2: RabbitMQ Vhosts -->
<div class="card">
  <div class="toolbar">
    <h2 class="card-title">{i18n.t.services.rabbitTitle}</h2>
    <div class="table-actions">
      <button type="button" class="btn btn-sm" onclick={loadRabbit} disabled={loadingRabbit}>
        {i18n.t.common.refresh}
      </button>
      {#if canWrite}
        <button type="button" class="btn btn-sm btn-primary" onclick={openCreateRabbit}>
          + {i18n.t.services.createRabbit}
        </button>
      {/if}
    </div>
  </div>

  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.services.vhost}</th>
          <th>{i18n.t.services.username}</th>
          <th>{i18n.t.common.status}</th>
          <th>{i18n.t.common.createdAt}</th>
          {#if canWrite}
            <th>{i18n.t.common.actions}</th>
          {/if}
        </tr>
      </thead>
      <tbody>
        {#if loadingRabbit && rabbitList.length === 0}
          <tr><td colspan={canWrite ? 5 : 4} class="empty-state">{i18n.t.common.loading}</td></tr>
        {:else if rabbitList.length === 0}
          <tr><td colspan={canWrite ? 5 : 4} class="empty-state">{i18n.t.common.noData}</td></tr>
        {:else}
          {#each rabbitList as mq (mq.id)}
            <tr>
              <td class="mono"><strong>{mq.vhost}</strong></td>
              <td class="mono">{mq.username}</td>
              <td>
                <span class="badge {mq.status === 'active' ? 'badge-success' : 'badge-warning'}">
                  {mq.status}
                </span>
              </td>
              <td>{formatDateTime(mq.created_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</td>
              {#if canWrite}
                <td>
                  <div class="table-actions">
                    <button
                      type="button"
                      class="btn btn-sm btn-icon btn-danger"
                      title={i18n.t.common.delete}
                      aria-label={i18n.t.common.delete}
                      onclick={() => (deleteRabbitTarget = mq)}
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

<!-- Create Redis Modal -->
<Modal open={redisFormOpen} title={i18n.t.services.createRedis} onclose={() => (redisFormOpen = false)}>
  <form id="redis-form" onsubmit={submitRedis}>
    {#if rFormError}
      <div class="alert alert-danger" role="alert">{rFormError}</div>
    {/if}

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="r-name">{i18n.t.services.redisName}</label>
        <input
          id="r-name"
          type="text"
          class="form-control mono"
          bind:value={rName}
          placeholder="cache-1"
          required
        />
      </div>
      <div class="form-group">
        <label class="form-label" for="r-mem">{i18n.t.services.memoryMb}</label>
        <input
          id="r-mem"
          type="number"
          class="form-control"
          bind:value={rMemoryMb}
          min="32"
          step="32"
          required
        />
      </div>
    </div>

    <div class="form-group">
      <label class="checkbox-row">
        <input type="checkbox" bind:checked={rPersistent} />
        <span>{i18n.t.services.persistent}</span>
      </label>
    </div>

    {#if canSelectOwner && users.length > 0}
      <div class="form-group">
        <label class="form-label" for="r-owner">{i18n.t.services.owner}</label>
        <select id="r-owner" class="form-control" bind:value={rUserId}>
          {#each users as u (u.id)}
            <option value={u.id}>{u.username} ({u.role})</option>
          {/each}
        </select>
      </div>
    {/if}
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (redisFormOpen = false)} disabled={rSaving}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="redis-form" class="btn btn-primary" disabled={rSaving}>
      {rSaving ? i18n.t.common.loading : i18n.t.common.create}
    </button>
  {/snippet}
</Modal>

<!-- Delete Redis Modal -->
<Modal
  open={deleteRedisTarget !== null}
  title={i18n.t.services.deleteRedisTitle}
  onclose={() => (deleteRedisTarget = null)}
>
  {#if deleteRedisTarget}
    <p>{fmt(i18n.t.services.deleteRedisBody, { name: deleteRedisTarget.name })}</p>
  {/if}
  {#snippet footer()}
    <button
      type="button"
      class="btn"
      onclick={() => (deleteRedisTarget = null)}
      disabled={deletingRedis}
    >
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-danger"
      onclick={confirmDeleteRedis}
      disabled={deletingRedis}
    >
      {deletingRedis ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  {/snippet}
</Modal>

<!-- Create RabbitMQ Vhost Modal -->
<Modal open={rabbitFormOpen} title={i18n.t.services.createRabbit} onclose={() => (rabbitFormOpen = false)}>
  <form id="rabbit-form" onsubmit={submitRabbit}>
    {#if mqFormError}
      <div class="alert alert-danger" role="alert">{mqFormError}</div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="mq-vhost">{i18n.t.services.vhost}</label>
      <input
        id="mq-vhost"
        type="text"
        class="form-control mono"
        bind:value={mqVhost}
        placeholder={i18n.t.services.vhostPlaceholder}
        required
      />
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="mq-user">{i18n.t.services.username}</label>
        <input
          id="mq-user"
          type="text"
          class="form-control mono"
          bind:value={mqUsername}
          required
        />
      </div>
      <div class="form-group">
        <label class="form-label" for="mq-pass">{i18n.t.services.password}</label>
        <input
          id="mq-pass"
          type="password"
          class="form-control"
          bind:value={mqPassword}
          required
        />
      </div>
    </div>

    {#if canSelectOwner && users.length > 0}
      <div class="form-group">
        <label class="form-label" for="mq-owner">{i18n.t.services.owner}</label>
        <select id="mq-owner" class="form-control" bind:value={mqUserId}>
          {#each users as u (u.id)}
            <option value={u.id}>{u.username} ({u.role})</option>
          {/each}
        </select>
      </div>
    {/if}
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (rabbitFormOpen = false)} disabled={mqSaving}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="rabbit-form" class="btn btn-primary" disabled={mqSaving}>
      {mqSaving ? i18n.t.common.loading : i18n.t.common.create}
    </button>
  {/snippet}
</Modal>

<!-- Delete RabbitMQ Vhost Modal -->
<Modal
  open={deleteRabbitTarget !== null}
  title={i18n.t.services.deleteRabbitTitle}
  onclose={() => (deleteRabbitTarget = null)}
>
  {#if deleteRabbitTarget}
    <p>{fmt(i18n.t.services.deleteRabbitBody, { vhost: deleteRabbitTarget.vhost })}</p>
  {/if}
  {#snippet footer()}
    <button
      type="button"
      class="btn"
      onclick={() => (deleteRabbitTarget = null)}
      disabled={deletingRabbit}
    >
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-danger"
      onclick={confirmDeleteRabbit}
      disabled={deletingRabbit}
    >
      {deletingRabbit ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  {/snippet}
</Modal>

<!-- One-time Connection String Modal -->
<Modal
  open={createdConnString !== null}
  title={i18n.t.services.connOnceTitle}
  onclose={() => (createdConnString = null)}
>
  <div class="alert alert-warning">{i18n.t.services.connOnceDesc}</div>
  {#if createdConnString}
    <div class="secret-box">
      <span>{createdConnString}</span>
      <button type="button" class="btn btn-sm" onclick={copyConnString}>
        {copiedConn ? i18n.t.common.copied : i18n.t.common.copy}
      </button>
    </div>
  {/if}
  {#snippet footer()}
    <button type="button" class="btn btn-primary" onclick={() => (createdConnString = null)}>
      {i18n.t.common.close}
    </button>
  {/snippet}
</Modal>

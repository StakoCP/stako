<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import Pagination from '../components/Pagination.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { router } from '../lib/router.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { Package, PaginatedResponse, User, UserRole, UserShell } from '../lib/types';

  let users = $state<User[]>([]);
  let packages = $state<Package[]>([]);
  let total = $state(0);
  let limit = 25;
  let offset = $state(0);
  let search = $state('');
  let loading = $state(true);
  let lastJobId = $state<string | null>(null);

  // Create / Edit modal
  let formOpen = $state(false);
  let editingUser = $state<User | null>(null);
  let fUsername = $state('');
  let fEmail = $state('');
  let fPassword = $state('');
  let fRole = $state<UserRole>('user');
  let fPackageId = $state('');
  let fShell = $state<UserShell>('nologin');
  let formError = $state('');
  let saving = $state(false);

  // Delete modal
  let deleteTarget = $state<User | null>(null);
  let deleting = $state(false);
  let impersonatingId = $state<string | null>(null);

  function canImpersonate(u: User): boolean {
    if (auth.impersonator || !auth.user || u.id === auth.user.id) return false;
    if (auth.user.role === 'admin') return u.role === 'reseller' || u.role === 'user';
    if (auth.user.role === 'reseller') return u.role === 'user';
    return false;
  }

  async function handleImpersonate(u: User) {
    impersonatingId = u.id;
    try {
      await auth.impersonate(u.id);
      toast.success(fmt(i18n.t.impersonation.started, { username: u.username }));
      router.navigate('/dashboard');
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      impersonatingId = null;
    }
  }

  let availableRoles = $derived<UserRole[]>(
    auth.user?.role === 'admin' ? ['admin', 'reseller', 'user', 'readonly'] : ['user', 'readonly']
  );

  let packageMap = $derived(new Map(packages.map((p) => [p.id, p.name])));

  async function loadUsers() {
    loading = true;
    try {
      const qs = new URLSearchParams({
        limit: String(limit),
        offset: String(offset),
      });
      if (search.trim()) qs.set('q', search.trim());
      const res = await api.get<PaginatedResponse<User>>(`/users?${qs.toString()}`);
      users = res.items;
      total = res.total;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
    }
  }

  async function loadPackages() {
    try {
      const res = await api.get<PaginatedResponse<Package>>('/packages?limit=200&offset=0');
      packages = res.items;
    } catch {
      // ignore if user has no package list access
    }
  }

  onMount(() => {
    loadUsers();
    loadPackages();
  });

  function handleSearch(e: SubmitEvent) {
    e.preventDefault();
    offset = 0;
    loadUsers();
  }

  function openCreate() {
    editingUser = null;
    fUsername = '';
    fEmail = '';
    fPassword = '';
    fRole = 'user';
    fPackageId = packages[0]?.id ?? '';
    fShell = 'nologin';
    formError = '';
    formOpen = true;
  }

  function openEdit(u: User) {
    editingUser = u;
    fUsername = u.username;
    fEmail = u.email;
    fPassword = '';
    fRole = u.role;
    fPackageId = u.package_id ?? '';
    fShell = u.shell;
    formError = '';
    formOpen = true;
  }

  async function submitForm(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    if ((!editingUser || fPassword.length > 0) && fPassword.length < 12) {
      formError = i18n.t.users.passwordTooShort;
      return;
    }
    saving = true;
    try {
      if (editingUser) {
        const body: Record<string, unknown> = {
          email: fEmail.trim(),
          role: fRole,
          package_id: fPackageId || null,
          shell: fShell,
        };
        if (fPassword) body.password = fPassword;
        await api.patch<User>(`/users/${editingUser.id}`, body);
        toast.success(i18n.t.users.updatedSuccess);
      } else {
        const body: Record<string, unknown> = {
          username: fUsername.trim(),
          email: fEmail.trim(),
          password: fPassword,
          role: fRole,
          shell: fShell,
        };
        if (fPackageId) body.package_id = fPackageId;
        const res = await api.post<{ user: User; job_id: string | null }>('/users', body);
        toast.success(i18n.t.users.createdSuccess);
        if (res.job_id) lastJobId = res.job_id;
      }
      formOpen = false;
      await loadUsers();
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

  async function toggleSuspend(u: User) {
    try {
      const endpoint = u.status === 'active' ? 'suspend' : 'unsuspend';
      await api.post<User>(`/users/${u.id}/${endpoint}`);
      toast.success(
        u.status === 'active' ? i18n.t.users.suspendedSuccess : i18n.t.users.unsuspendedSuccess
      );
      await loadUsers();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    try {
      const res = await api.delete<{ job_id: string | null }>(`/users/${deleteTarget.id}`);
      toast.success(i18n.t.users.deletedSuccess);
      if (res?.job_id) lastJobId = res.job_id;
      deleteTarget = null;
      await loadUsers();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      deleting = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.users.title}</h1>
    <p class="page-subtitle">{i18n.t.users.subtitle}</p>
  </div>
  <button type="button" class="btn btn-primary" onclick={openCreate}>
    + {i18n.t.users.createUser}
  </button>
</div>

{#if lastJobId}
  <div class="alert alert-info" role="status">
    {fmt(i18n.t.users.jobCreated, { jobId: lastJobId })}
    <a href={`#/jobs/${lastJobId}`}>{i18n.t.users.viewJob} →</a>
  </div>
{/if}

<div class="toolbar">
  <form class="toolbar-filters" onsubmit={handleSearch}>
    <input
      type="search"
      class="form-control"
      placeholder={i18n.t.common.search}
      bind:value={search}
      aria-label={i18n.t.common.search}
    />
    <button type="submit" class="btn">{i18n.t.common.search}</button>
  </form>
  <button type="button" class="btn btn-sm" onclick={loadUsers} disabled={loading}>
    {i18n.t.common.refresh}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.users.username}</th>
        <th>{i18n.t.users.email}</th>
        <th>{i18n.t.users.role}</th>
        <th>{i18n.t.users.package}</th>
        <th>{i18n.t.users.shell}</th>
        <th>{i18n.t.common.status}</th>
        <th>{i18n.t.users.lastLogin}</th>
        <th>{i18n.t.common.actions}</th>
      </tr>
    </thead>
    <tbody>
      {#if loading && users.length === 0}
        <tr><td colspan="8" class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if users.length === 0}
        <tr><td colspan="8" class="empty-state">{i18n.t.common.noData}</td></tr>
      {:else}
        {#each users as u (u.id)}
          <tr>
            <td class="mono"><strong>{u.username}</strong></td>
            <td>{u.email}</td>
            <td><span class="badge badge-primary">{i18n.t.roles[u.role]}</span></td>
            <td>{u.package_id ? (packageMap.get(u.package_id) ?? u.package_id) : '—'}</td>
            <td class="mono">{u.shell}</td>
            <td>
              <span
                class="badge"
                class:badge-success={u.status === 'active'}
                class:badge-danger={u.status === 'suspended'}
              >
                {u.status === 'active' ? i18n.t.users.active : i18n.t.users.suspended}
              </span>
            </td>
            <td>{formatDateTime(u.last_login_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</td>
            <td>
              <div class="table-actions">
                {#if canImpersonate(u)}
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.impersonation.loginAs}
                    aria-label={i18n.t.impersonation.loginAs}
                    onclick={() => handleImpersonate(u)}
                    disabled={impersonatingId === u.id}
                  >
                    <ActionIcon name="impersonate" />
                  </button>
                {/if}
                <button
                  type="button"
                  class="btn btn-sm btn-icon"
                  title={i18n.t.common.edit}
                  aria-label={i18n.t.common.edit}
                  onclick={() => openEdit(u)}
                >
                  <ActionIcon name="edit" />
                </button>
                <button
                  type="button"
                  class="btn btn-sm btn-icon"
                  title={u.status === 'active' ? i18n.t.users.suspend : i18n.t.users.unsuspend}
                  aria-label={u.status === 'active' ? i18n.t.users.suspend : i18n.t.users.unsuspend}
                  onclick={() => toggleSuspend(u)}
                >
                  <ActionIcon name={u.status === 'active' ? 'lock' : 'unlock'} />
                </button>
                {#if u.id !== auth.user?.id}
                  <button
                    type="button"
                    class="btn btn-sm btn-icon btn-danger"
                    title={i18n.t.common.delete}
                    aria-label={i18n.t.common.delete}
                    onclick={() => (deleteTarget = u)}
                  >
                    <ActionIcon name="delete" />
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

<Pagination
  {total}
  {limit}
  {offset}
  onchange={(newOffset) => {
    offset = newOffset;
    loadUsers();
  }}
/>

<Modal
  open={formOpen}
  title={editingUser ? i18n.t.users.editUser : i18n.t.users.createUser}
  onclose={() => (formOpen = false)}
>
  <form id="user-form" onsubmit={submitForm}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}

    {#if !editingUser}
      <div class="form-group">
        <label class="form-label" for="u-username">{i18n.t.users.username}</label>
        <input id="u-username" type="text" class="form-control" bind:value={fUsername} required />
      </div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="u-email">{i18n.t.users.email}</label>
      <input id="u-email" type="email" class="form-control" bind:value={fEmail} required />
    </div>

    <div class="form-group">
      <label class="form-label" for="u-password">
        {editingUser ? i18n.t.users.passwordNew : i18n.t.users.password}
      </label>
      <input
        id="u-password"
        type="password"
        class="form-control"
        bind:value={fPassword}
        minlength={editingUser ? undefined : 12}
        required={!editingUser}
        autocomplete="new-password"
      />
      <div class="form-hint">{i18n.t.users.passwordHint}</div>
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="u-role">{i18n.t.users.role}</label>
        <select id="u-role" class="form-control" bind:value={fRole}>
          {#each availableRoles as r (r)}
            <option value={r}>{i18n.t.roles[r]}</option>
          {/each}
        </select>
      </div>
      <div class="form-group">
        <label class="form-label" for="u-shell">{i18n.t.users.shell}</label>
        <select id="u-shell" class="form-control" bind:value={fShell}>
          <option value="nologin">nologin</option>
          <option value="sftp">sftp</option>
          <option value="rbash">rbash</option>
          <option value="bash">bash</option>
        </select>
      </div>
    </div>

    <div class="form-group">
      <label class="form-label" for="u-package">{i18n.t.users.package}</label>
      <select id="u-package" class="form-control" bind:value={fPackageId}>
        <option value="">{i18n.t.common.none}</option>
        {#each packages as p (p.id)}
          <option value={p.id}>{p.name}</option>
        {/each}
      </select>
    </div>
  </form>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (formOpen = false)} disabled={saving}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="user-form" class="btn btn-primary" disabled={saving}>
      {saving ? i18n.t.common.loading : i18n.t.common.save}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.users.deleteConfirmTitle}
  onclose={() => (deleteTarget = null)}
>
  {#if deleteTarget}
    <p>{fmt(i18n.t.users.deleteConfirmBody, { username: deleteTarget.username })}</p>
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

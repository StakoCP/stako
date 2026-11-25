<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatQuota } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { Package, PackageInput, PaginatedResponse } from '../lib/types';

  let packages = $state<Package[]>([]);
  let loading = $state(true);

  let formOpen = $state(false);
  let editingPkg = $state<Package | null>(null);
  let fName = $state('');
  let fDiskMb = $state(10240);
  let fBandwidthMb = $state(-1);
  let fDomains = $state(5);
  let fDatabases = $state(5);
  let fMailAccounts = $state(10);
  let fFtpAccounts = $state(2);
  let fContainers = $state(2);
  let fCpuPercent = $state(100);
  let fMemoryMb = $state(1024);
  let fShellAccess = $state(false);
  let fPhpVersions = $state('8.3');
  let formError = $state('');
  let saving = $state(false);

  let deleteTarget = $state<Package | null>(null);
  let deleting = $state(false);

  async function loadPackages() {
    loading = true;
    try {
      const res = await api.get<PaginatedResponse<Package>>('/packages?limit=200&offset=0');
      packages = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
    }
  }

  onMount(loadPackages);

  function canModify(pkg: Package): boolean {
    if (auth.user?.role === 'admin') return true;
    if (auth.user?.role === 'reseller') return pkg.owner_id === auth.user.id;
    return false;
  }

  function openCreate() {
    editingPkg = null;
    fName = '';
    fDiskMb = 10240;
    fBandwidthMb = -1;
    fDomains = 5;
    fDatabases = 5;
    fMailAccounts = 10;
    fFtpAccounts = 2;
    fContainers = 2;
    fCpuPercent = 100;
    fMemoryMb = 1024;
    fShellAccess = false;
    fPhpVersions = '8.3';
    formError = '';
    formOpen = true;
  }

  function openEdit(pkg: Package) {
    editingPkg = pkg;
    fName = pkg.name;
    fDiskMb = pkg.disk_mb;
    fBandwidthMb = pkg.bandwidth_mb;
    fDomains = pkg.domains;
    fDatabases = pkg.databases;
    fMailAccounts = pkg.mail_accounts;
    fFtpAccounts = pkg.ftp_accounts;
    fContainers = pkg.containers;
    fCpuPercent = pkg.cpu_percent;
    fMemoryMb = pkg.memory_mb;
    fShellAccess = pkg.shell_access;
    fPhpVersions = pkg.php_versions.join(', ');
    formError = '';
    formOpen = true;
  }

  async function submitForm(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    saving = true;
    const payload: PackageInput = {
      name: fName.trim(),
      disk_mb: Number(fDiskMb),
      bandwidth_mb: Number(fBandwidthMb),
      domains: Number(fDomains),
      databases: Number(fDatabases),
      mail_accounts: Number(fMailAccounts),
      ftp_accounts: Number(fFtpAccounts),
      containers: Number(fContainers),
      cpu_percent: Number(fCpuPercent),
      memory_mb: Number(fMemoryMb),
      shell_access: fShellAccess,
      php_versions: fPhpVersions
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean),
    };

    try {
      if (editingPkg) {
        await api.patch<Package>(`/packages/${editingPkg.id}`, payload);
        toast.success(i18n.t.packages.updatedSuccess);
      } else {
        await api.post<Package>('/packages', payload);
        toast.success(i18n.t.packages.createdSuccess);
      }
      formOpen = false;
      await loadPackages();
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
      await api.delete(`/packages/${deleteTarget.id}`);
      toast.success(i18n.t.packages.deletedSuccess);
      deleteTarget = null;
      await loadPackages();
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        toast.error(i18n.t.packages.inUseError);
      } else {
        toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      }
    } finally {
      deleting = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.packages.title}</h1>
    <p class="page-subtitle">{i18n.t.packages.subtitle}</p>
  </div>
  <button type="button" class="btn btn-primary" onclick={openCreate}>
    + {i18n.t.packages.createPackage}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.packages.name}</th>
        <th>{i18n.t.packages.diskMb}</th>
        <th>{i18n.t.packages.bandwidthMb}</th>
        <th>{i18n.t.packages.domains}</th>
        <th>{i18n.t.packages.databases}</th>
        <th>{i18n.t.packages.containers}</th>
        <th>{i18n.t.packages.memoryMb}</th>
        <th>PHP</th>
        <th>{i18n.t.common.actions}</th>
      </tr>
    </thead>
    <tbody>
      {#if loading && packages.length === 0}
        <tr><td colspan="9" class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if packages.length === 0}
        <tr><td colspan="9" class="empty-state">{i18n.t.common.noData}</td></tr>
      {:else}
        {#each packages as pkg (pkg.id)}
          <tr>
            <td><strong>{pkg.name}</strong></td>
            <td>{formatQuota(pkg.disk_mb, 'MB')}</td>
            <td>{formatQuota(pkg.bandwidth_mb, 'MB')}</td>
            <td>{formatQuota(pkg.domains)}</td>
            <td>{formatQuota(pkg.databases)}</td>
            <td>{formatQuota(pkg.containers)}</td>
            <td>{formatQuota(pkg.memory_mb, 'MB')}</td>
            <td class="mono">{pkg.php_versions.join(', ') || '—'}</td>
            <td>
              {#if canModify(pkg)}
                <div class="table-actions">
                  <button type="button" class="btn btn-sm" onclick={() => openEdit(pkg)}>
                    {i18n.t.common.edit}
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-danger"
                    onclick={() => (deleteTarget = pkg)}
                  >
                    {i18n.t.common.delete}
                  </button>
                </div>
              {:else}
                <span class="badge">Global</span>
              {/if}
            </td>
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>

<Modal
  open={formOpen}
  title={editingPkg ? i18n.t.packages.editPackage : i18n.t.packages.createPackage}
  size="lg"
  onclose={() => (formOpen = false)}
>
  <form id="pkg-form" onsubmit={submitForm}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="p-name">{i18n.t.packages.name}</label>
      <input id="p-name" type="text" class="form-control" bind:value={fName} required />
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="p-disk">{i18n.t.packages.diskMb}</label>
        <input id="p-disk" type="number" min="-1" class="form-control" bind:value={fDiskMb} required />
      </div>
      <div class="form-group">
        <label class="form-label" for="p-bw">{i18n.t.packages.bandwidthMb}</label>
        <input id="p-bw" type="number" min="-1" class="form-control" bind:value={fBandwidthMb} required />
      </div>
      <div class="form-group">
        <label class="form-label" for="p-domains">{i18n.t.packages.domains}</label>
        <input id="p-domains" type="number" min="-1" class="form-control" bind:value={fDomains} required />
      </div>
      <div class="form-group">
        <label class="form-label" for="p-db">{i18n.t.packages.databases}</label>
        <input id="p-db" type="number" min="-1" class="form-control" bind:value={fDatabases} required />
      </div>
      <div class="form-group">
        <label class="form-label" for="p-mail">{i18n.t.packages.mailAccounts}</label>
        <input id="p-mail" type="number" min="-1" class="form-control" bind:value={fMailAccounts} required />
      </div>
      <div class="form-group">
        <label class="form-label" for="p-ftp">{i18n.t.packages.ftpAccounts}</label>
        <input id="p-ftp" type="number" min="-1" class="form-control" bind:value={fFtpAccounts} required />
      </div>
      <div class="form-group">
        <label class="form-label" for="p-containers">{i18n.t.packages.containers}</label>
        <input id="p-containers" type="number" min="-1" class="form-control" bind:value={fContainers} required />
      </div>
      <div class="form-group">
        <label class="form-label" for="p-cpu">{i18n.t.packages.cpuPercent}</label>
        <input id="p-cpu" type="number" min="-1" class="form-control" bind:value={fCpuPercent} required />
      </div>
      <div class="form-group">
        <label class="form-label" for="p-mem">{i18n.t.packages.memoryMb}</label>
        <input id="p-mem" type="number" min="-1" class="form-control" bind:value={fMemoryMb} required />
      </div>
    </div>

    <p class="form-hint">{i18n.t.packages.unlimitedHint}</p>

    <div class="form-group">
      <label class="form-label" for="p-php">{i18n.t.packages.phpVersions}</label>
      <input
        id="p-php"
        type="text"
        class="form-control"
        bind:value={fPhpVersions}
        placeholder="8.2, 8.3"
      />
    </div>

    <div class="form-group">
      <label class="checkbox-row">
        <input type="checkbox" bind:checked={fShellAccess} />
        <span>{i18n.t.packages.shellAccess}</span>
      </label>
    </div>
  </form>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (formOpen = false)} disabled={saving}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="pkg-form" class="btn btn-primary" disabled={saving}>
      {saving ? i18n.t.common.loading : i18n.t.common.save}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.packages.deleteConfirmTitle}
  onclose={() => (deleteTarget = null)}
>
  {#if deleteTarget}
    <p>{fmt(i18n.t.packages.deleteConfirmBody, { name: deleteTarget.name })}</p>
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

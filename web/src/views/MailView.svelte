<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatDateTime, formatQuota } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type {
    MailDnsRecord,
    MailDomain,
    Mailbox,
    PaginatedResponse,
    User,
  } from '../lib/types';

  let domains = $state<MailDomain[]>([]);
  let users = $state<User[]>([]);
  let loadingDomains = $state(true);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let canSelectOwner = $derived(auth.user?.role === 'admin' || auth.user?.role === 'reseller');
  let localeCode = $derived(i18n.locale === 'id' ? 'id-ID' : 'en-US');

  // Selected domain -> Mailboxes panel
  let selectedDomain = $state<MailDomain | null>(null);
  let mailboxes = $state<Mailbox[]>([]);
  let loadingMailboxes = $state(false);

  // Add Mail Domain modal
  let addDomainOpen = $state(false);
  let fDomain = $state('');
  let fUserId = $state('');
  let domainFormError = $state('');
  let savingDomain = $state(false);

  // DNS Records modal (shown once after POST /mail/domains)
  let dnsRecordsModalOpen = $state(false);
  let createdDnsDomain = $state('');
  let createdDnsRecords = $state<MailDnsRecord[]>([]);
  let copiedDnsIndex = $state<number | null>(null);

  // Delete Mail Domain modal
  let deleteDomainTarget = $state<MailDomain | null>(null);
  let deletingDomain = $state(false);

  // Create Mailbox modal
  let addMailboxOpen = $state(false);
  let fLocalPart = $state('');
  let fPassword = $state('');
  let fQuotaMb = $state('');
  let mailboxFormError = $state('');
  let savingMailbox = $state(false);

  // Delete Mailbox modal
  let deleteMailboxTarget = $state<Mailbox | null>(null);
  let deletingMailbox = $state(false);

  function notifyJob(message: string, jobId?: string | null) {
    if (jobId) {
      toast.success(`${message} (${fmt(i18n.t.mail.jobToast, { jobId })})`, {
        href: `#/jobs/${jobId}`,
        label: i18n.t.mail.viewJob,
      });
    } else {
      toast.success(message);
    }
  }

  async function loadDomains() {
    loadingDomains = true;
    try {
      const res = await api.get<{ items: MailDomain[] }>('/mail/domains');
      domains = res.items;
      if (selectedDomain) {
        const stillExists = domains.find((d) => d.id === selectedDomain?.id) ?? null;
        selectedDomain = stillExists;
        if (!stillExists) {
          mailboxes = [];
        }
      }
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingDomains = false;
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

  async function selectDomain(domain: MailDomain) {
    selectedDomain = domain;
    await loadMailboxes(domain.id);
  }

  async function loadMailboxes(domainId: string) {
    loadingMailboxes = true;
    try {
      const res = await api.get<{ items: Mailbox[] }>(`/mail/domains/${domainId}/mailboxes`);
      mailboxes = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingMailboxes = false;
    }
  }

  onMount(() => {
    loadDomains();
    loadUsers();
  });

  function openAddDomain() {
    fDomain = '';
    fUserId = auth.user?.id ?? '';
    domainFormError = '';
    addDomainOpen = true;
  }

  async function submitDomain(e: SubmitEvent) {
    e.preventDefault();
    domainFormError = '';
    savingDomain = true;
    try {
      const body: Record<string, unknown> = {
        domain: fDomain.trim(),
      };
      if (canSelectOwner && fUserId) body.user_id = fUserId;

      const res = await api.post<{
        mail_domain: MailDomain;
        dns_records: MailDnsRecord[];
        job_id?: string;
      }>('/mail/domains', body);

      addDomainOpen = false;
      notifyJob(i18n.t.mail.domainCreated, res.job_id);
      createdDnsDomain = res.mail_domain.domain;
      createdDnsRecords = res.dns_records ?? [];
      copiedDnsIndex = null;
      if (createdDnsRecords.length > 0) {
        dnsRecordsModalOpen = true;
      }
      await loadDomains();
      await selectDomain(res.mail_domain);
    } catch (err) {
      if (err instanceof ApiError) {
        const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
        domainFormError = fieldMsg || err.detail || err.title;
      } else {
        domainFormError = i18n.t.common.errorGeneric;
      }
    } finally {
      savingDomain = false;
    }
  }

  async function copyDnsValue(value: string, idx: number) {
    try {
      await navigator.clipboard.writeText(value);
      copiedDnsIndex = idx;
      toast.success(i18n.t.common.copied);
    } catch {
      toast.error(i18n.t.common.errorGeneric);
    }
  }

  async function confirmDeleteDomain() {
    if (!deleteDomainTarget) return;
    deletingDomain = true;
    try {
      await api.delete(`/mail/domains/${deleteDomainTarget.id}`);
      toast.success(i18n.t.mail.domainDeleted);
      if (selectedDomain?.id === deleteDomainTarget.id) {
        selectedDomain = null;
        mailboxes = [];
      }
      deleteDomainTarget = null;
      await loadDomains();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      deleteDomainTarget = null;
    } finally {
      deletingDomain = false;
    }
  }

  function openAddMailbox() {
    fLocalPart = '';
    fPassword = '';
    fQuotaMb = '';
    mailboxFormError = '';
    addMailboxOpen = true;
  }

  async function submitMailbox(e: SubmitEvent) {
    e.preventDefault();
    if (!selectedDomain) return;
    mailboxFormError = '';
    savingMailbox = true;
    try {
      const body: Record<string, unknown> = {
        local_part: fLocalPart.trim(),
        password: fPassword,
      };
      if (fQuotaMb.trim() !== '') {
        const parsedQuota = Number(fQuotaMb.trim());
        if (Number.isFinite(parsedQuota)) body.quota_mb = parsedQuota;
      }

      await api.post<Mailbox>(`/mail/domains/${selectedDomain.id}/mailboxes`, body);
      toast.success(i18n.t.mail.mailboxCreated);
      addMailboxOpen = false;
      await loadMailboxes(selectedDomain.id);
    } catch (err) {
      if (err instanceof ApiError) {
        const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
        mailboxFormError = fieldMsg || err.detail || err.title;
      } else {
        mailboxFormError = i18n.t.common.errorGeneric;
      }
    } finally {
      savingMailbox = false;
    }
  }

  async function confirmDeleteMailbox() {
    if (!deleteMailboxTarget || !selectedDomain) return;
    deletingMailbox = true;
    try {
      await api.delete(`/mail/mailboxes/${deleteMailboxTarget.id}`);
      toast.success(i18n.t.mail.mailboxDeleted);
      deleteMailboxTarget = null;
      await loadMailboxes(selectedDomain.id);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      deleteMailboxTarget = null;
    } finally {
      deletingMailbox = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.mail.title}</h1>
    <p class="page-subtitle">{i18n.t.mail.subtitle}</p>
  </div>
  {#if canWrite}
    <button type="button" class="btn btn-primary" onclick={openAddDomain}>
      + {i18n.t.mail.addDomain}
    </button>
  {/if}
</div>

<div class="toolbar">
  <div></div>
  <button type="button" class="btn btn-sm" onclick={loadDomains} disabled={loadingDomains}>
    {i18n.t.common.refresh}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.mail.domain}</th>
        <th>{i18n.t.mail.dkimSelector}</th>
        <th>{i18n.t.common.createdAt}</th>
        <th>{i18n.t.common.actions}</th>
      </tr>
    </thead>
    <tbody>
      {#if loadingDomains && domains.length === 0}
        <tr><td colspan="4" class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if domains.length === 0}
        <tr><td colspan="4" class="empty-state">{i18n.t.common.noData}</td></tr>
      {:else}
        {#each domains as d (d.id)}
          <tr>
            <td>
              <button
                type="button"
                class="btn btn-ghost btn-sm mono"
                onclick={() => selectDomain(d)}
              >
                <strong>{d.domain}</strong>
              </button>
            </td>
            <td class="mono">{d.dkim_selector}</td>
            <td>{formatDateTime(d.created_at, localeCode)}</td>
            <td>
              <div class="table-actions">
                <button
                  type="button"
                  class="btn btn-sm btn-icon"
                  title={i18n.t.mail.manageMailboxes}
                  aria-label={i18n.t.mail.manageMailboxes}
                  onclick={() => selectDomain(d)}
                >
                  <ActionIcon name="config" />
                </button>
                {#if canWrite}
                  <button
                    type="button"
                    class="btn btn-sm btn-icon btn-danger"
                    title={i18n.t.common.delete}
                    aria-label={i18n.t.common.delete}
                    onclick={() => (deleteDomainTarget = d)}
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

{#if selectedDomain}
  <hr class="section-divider" />
  <div class="card">
    <div class="toolbar">
      <h2 class="card-title">{fmt(i18n.t.mail.mailboxesFor, { domain: selectedDomain.domain })}</h2>
      <div class="table-actions">
        <button
          type="button"
          class="btn btn-sm"
          onclick={() => selectedDomain && loadMailboxes(selectedDomain.id)}
          disabled={loadingMailboxes}
        >
          {i18n.t.common.refresh}
        </button>
        {#if canWrite}
          <button type="button" class="btn btn-sm btn-primary" onclick={openAddMailbox}>
            + {i18n.t.mail.createMailbox}
          </button>
        {/if}
      </div>
    </div>

    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{i18n.t.mail.email}</th>
            <th>{i18n.t.mail.quotaMb}</th>
            <th>{i18n.t.common.createdAt}</th>
            {#if canWrite}
              <th>{i18n.t.common.actions}</th>
            {/if}
          </tr>
        </thead>
        <tbody>
          {#if loadingMailboxes && mailboxes.length === 0}
            <tr><td colspan={canWrite ? 4 : 3} class="empty-state">{i18n.t.common.loading}</td></tr>
          {:else if mailboxes.length === 0}
            <tr><td colspan={canWrite ? 4 : 3} class="empty-state">{i18n.t.common.noData}</td></tr>
          {:else}
            {#each mailboxes as mb (mb.id)}
              <tr>
                <td class="mono"><strong>{mb.email}</strong></td>
                <td>{mb.quota_mb === 0 ? i18n.t.common.unlimited : formatQuota(mb.quota_mb, 'MB')}</td>
                <td>{formatDateTime(mb.created_at, localeCode)}</td>
                {#if canWrite}
                  <td>
                    <button
                      type="button"
                      class="btn btn-sm btn-icon btn-danger"
                      title={i18n.t.common.delete}
                      aria-label={i18n.t.common.delete}
                      onclick={() => (deleteMailboxTarget = mb)}
                    >
                      <ActionIcon name="delete" />
                    </button>
                  </td>
                {/if}
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
  </div>
{/if}

<Modal open={addDomainOpen} title={i18n.t.mail.addDomain} onclose={() => (addDomainOpen = false)}>
  <form id="mail-domain-form" onsubmit={submitDomain}>
    {#if domainFormError}
      <div class="alert alert-danger" role="alert">{domainFormError}</div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="mail-domain-input">{i18n.t.mail.domain}</label>
      <input
        id="mail-domain-input"
        type="text"
        class="form-control"
        bind:value={fDomain}
        required
        placeholder={i18n.t.mail.domainPlaceholder}
      />
    </div>

    {#if canSelectOwner && users.length > 0}
      <div class="form-group">
        <label class="form-label" for="mail-domain-owner">{i18n.t.mail.owner}</label>
        <select id="mail-domain-owner" class="form-control" bind:value={fUserId}>
          {#each users as u (u.id)}
            <option value={u.id}>{u.username} ({u.role})</option>
          {/each}
        </select>
      </div>
    {/if}
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (addDomainOpen = false)}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="mail-domain-form" class="btn btn-primary" disabled={savingDomain}>
      {savingDomain ? i18n.t.common.loading : i18n.t.common.create}
    </button>
  {/snippet}
</Modal>

<Modal
  open={dnsRecordsModalOpen}
  title={`${i18n.t.mail.dnsTitle} — ${createdDnsDomain}`}
  size="lg"
  onclose={() => (dnsRecordsModalOpen = false)}
>
  <p>{i18n.t.mail.dnsDesc}</p>
  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.mail.dnsType}</th>
          <th>{i18n.t.mail.dnsName}</th>
          <th>{i18n.t.mail.dnsValue}</th>
          <th>{i18n.t.common.actions}</th>
        </tr>
      </thead>
      <tbody>
        {#each createdDnsRecords as rec, idx (idx)}
          <tr>
            <td><span class="badge badge-info">{rec.type}</span></td>
            <td class="mono">{rec.name}</td>
            <td class="mono">{rec.value}</td>
            <td>
              <button
                type="button"
                class="btn btn-sm btn-icon"
                title={copiedDnsIndex === idx ? i18n.t.common.copied : i18n.t.common.copy}
                aria-label={copiedDnsIndex === idx ? i18n.t.common.copied : i18n.t.common.copy}
                onclick={() => copyDnsValue(rec.value, idx)}
              >
                <ActionIcon name="copy" />
              </button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>

  {#snippet footer()}
    <button type="button" class="btn btn-primary" onclick={() => (dnsRecordsModalOpen = false)}>
      {i18n.t.common.close}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteDomainTarget !== null}
  title={i18n.t.mail.deleteDomainTitle}
  onclose={() => (deleteDomainTarget = null)}
>
  <p>
    {fmt(i18n.t.mail.deleteDomainBody, { domain: deleteDomainTarget?.domain ?? '' })}
  </p>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (deleteDomainTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-danger"
      onclick={confirmDeleteDomain}
      disabled={deletingDomain}
    >
      {deletingDomain ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  {/snippet}
</Modal>

<Modal
  open={addMailboxOpen}
  title={i18n.t.mail.createMailbox}
  onclose={() => (addMailboxOpen = false)}
>
  <form id="mailbox-form" onsubmit={submitMailbox}>
    {#if mailboxFormError}
      <div class="alert alert-danger" role="alert">{mailboxFormError}</div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="mb-local">{i18n.t.mail.localPart}</label>
      <input
        id="mb-local"
        type="text"
        class="form-control"
        bind:value={fLocalPart}
        required
        placeholder={i18n.t.mail.localPartPlaceholder}
      />
      {#if selectedDomain}
        <div class="form-hint mono">{fLocalPart.trim() || 'user'}@{selectedDomain.domain}</div>
      {/if}
    </div>

    <div class="form-group">
      <label class="form-label" for="mb-password">{i18n.t.mail.password}</label>
      <input
        id="mb-password"
        type="password"
        class="form-control"
        bind:value={fPassword}
        required
        autocomplete="new-password"
      />
    </div>

    <div class="form-group">
      <label class="form-label" for="mb-quota">{i18n.t.mail.quotaMb}</label>
      <input
        id="mb-quota"
        type="number"
        min="0"
        class="form-control"
        bind:value={fQuotaMb}
        placeholder="1024"
      />
      <div class="form-hint">{i18n.t.mail.quotaHint}</div>
    </div>
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (addMailboxOpen = false)}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="mailbox-form" class="btn btn-primary" disabled={savingMailbox}>
      {savingMailbox ? i18n.t.common.loading : i18n.t.common.create}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteMailboxTarget !== null}
  title={i18n.t.mail.deleteMailboxTitle}
  onclose={() => (deleteMailboxTarget = null)}
>
  <p>
    {fmt(i18n.t.mail.deleteMailboxBody, { email: deleteMailboxTarget?.email ?? '' })}
  </p>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (deleteMailboxTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-danger"
      onclick={confirmDeleteMailbox}
      disabled={deletingMailbox}
    >
      {deletingMailbox ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  {/snippet}
</Modal>

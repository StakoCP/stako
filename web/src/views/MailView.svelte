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
    MailAlias,
    MailDnsRecord,
    MailDomain,
    MailQueueItem,
    Mailbox,
    PaginatedResponse,
    ServerSMTPRelay,
    User,
  } from '../lib/types';

  const RELAY_PRESETS: Record<string, { label: string; host: string; port: number; user?: string }> = {
    ses: { label: 'Amazon SES', host: 'email-smtp.us-east-1.amazonaws.com', port: 587 },
    mailgun: { label: 'Mailgun', host: 'smtp.mailgun.org', port: 587 },
    brevo: { label: 'Brevo (Sendinblue)', host: 'smtp-relay.brevo.com', port: 587 },
    sendgrid: { label: 'SendGrid', host: 'smtp.sendgrid.net', port: 587, user: 'apikey' },
    postmark: { label: 'Postmark', host: 'smtp.postmarkapp.com', port: 587 },
    custom: { label: 'Custom SMTP', host: '', port: 587 },
  };

  let domains = $state<MailDomain[]>([]);
  let users = $state<User[]>([]);
  let loadingDomains = $state(true);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let isAdmin = $derived(auth.user?.role === 'admin');
  let canSelectOwner = $derived(auth.user?.role === 'admin' || auth.user?.role === 'reseller');
  let localeCode = $derived(i18n.locale === 'id' ? 'id-ID' : 'en-US');

  // Selected domain -> Mailboxes & Aliases panel
  let selectedDomain = $state<MailDomain | null>(null);
  let domainTab = $state<'mailboxes' | 'aliases'>('mailboxes');
  let mailboxes = $state<Mailbox[]>([]);
  let loadingMailboxes = $state(false);
  let aliases = $state<MailAlias[]>([]);
  let loadingAliases = $state(false);

  // Add Alias modal
  let addAliasOpen = $state(false);
  let fAliasLocalPart = $state('');
  let fAliasDestination = $state('');
  let fAliasCatchAll = $state(false);
  let aliasFormError = $state('');
  let savingAlias = $state(false);

  // Delete Alias modal
  let deleteAliasTarget = $state<MailAlias | null>(null);
  let deletingAlias = $state(false);

  // Mail Queue (admin only)
  let queueItems = $state<MailQueueItem[]>([]);
  let loadingQueue = $state(false);
  let flushingQueue = $state(false);
  let deletingQueueId = $state<string | null>(null);

  // Add Mail Domain modal
  let addDomainOpen = $state(false);
  let fDomain = $state('');
  let fUserId = $state('');
  let domainFormError = $state('');
  let savingDomain = $state(false);

  // DNS Records modal (shown after POST /mail/domains or via DNS / DKIM button)
  let dnsRecordsModalOpen = $state(false);
  let createdDnsDomain = $state('');
  let createdDnsRecords = $state<MailDnsRecord[]>([]);
  let copiedDnsIndex = $state<number | null>(null);
  let loadingDns = $state(false);

  // Client Config (IMAP/SMTP) modal
  let clientConfigOpen = $state(false);
  let clientMailHost = $derived(
    selectedDomain
      ? `mail.${selectedDomain.domain}`
      : typeof window !== 'undefined'
        ? window.location.hostname
        : 'mail.example.com',
  );

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

  // Server SMTP Relay modal
  let serverRelayOpen = $state(false);
  let loadingServerRelay = $state(false);
  let savingServerRelay = $state(false);
  let testingServerRelay = $state(false);
  let serverRelayError = $state('');
  let srEnabled = $state(false);
  let srProvider = $state('ses');
  let srHost = $state('');
  let srPort = $state(587);
  let srUsername = $state('');
  let srPassword = $state('');
  let srTlsMode = $state('starttls');
  let srHasPassword = $state(false);

  // Domain SMTP Relay modal
  let domainRelayTarget = $state<MailDomain | null>(null);
  let savingDomainRelay = $state(false);
  let testingDomainRelay = $state(false);
  let domainRelayError = $state('');
  let drEnabled = $state(true);
  let drProvider = $state('ses');
  let drHost = $state('');
  let drPort = $state(587);
  let drUsername = $state('');
  let drPassword = $state('');

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
    await Promise.all([loadMailboxes(domain.id), loadAliases(domain.id)]);
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

  async function loadAliases(domainId: string) {
    loadingAliases = true;
    try {
      const res = await api.get<{ items: MailAlias[] }>(`/mail/domains/${domainId}/aliases`);
      aliases = res.items ?? [];
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingAliases = false;
    }
  }

  async function loadQueue() {
    if (!isAdmin) return;
    loadingQueue = true;
    try {
      const res = await api.get<{ items: MailQueueItem[] }>('/mail/queue');
      queueItems = res.items ?? [];
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingQueue = false;
    }
  }

  async function flushQueue() {
    flushingQueue = true;
    try {
      await api.post('/mail/queue/flush');
      toast.success(i18n.t.mail.queue_flushed);
      await loadQueue();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      flushingQueue = false;
    }
  }

  async function deleteQueueItem(queueId: string) {
    deletingQueueId = queueId;
    try {
      await api.delete(`/mail/queue/${encodeURIComponent(queueId)}`);
      toast.success(i18n.t.mail.queue_deleted);
      await loadQueue();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      deletingQueueId = null;
    }
  }

  onMount(() => {
    loadDomains();
    loadUsers();
    loadQueue();
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

  async function copyText(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(i18n.t.common.copied);
    } catch {
      toast.error(i18n.t.common.errorGeneric);
    }
  }

  async function openDnsRecords(domain: MailDomain) {
    loadingDns = true;
    try {
      const res = await api.get<{ records?: MailDnsRecord[]; dns_records?: MailDnsRecord[] }>(
        `/mail/domains/${domain.id}/dns`,
      );
      createdDnsDomain = domain.domain;
      createdDnsRecords = res.dns_records ?? res.records ?? [];
      copiedDnsIndex = null;
      dnsRecordsModalOpen = true;
    } catch (err) {
      if (err instanceof ApiError && err.status === 404 && domain.dkim_public_key) {
        const selector = domain.dkim_selector || 'default';
        createdDnsDomain = domain.domain;
        createdDnsRecords = [
          { type: 'TXT', name: domain.domain, value: 'v=spf1 mx a ~all' },
          {
            type: 'TXT',
            name: `${selector}._domainkey.${domain.domain}`,
            value: domain.dkim_public_key,
          },
          {
            type: 'TXT',
            name: `_dmarc.${domain.domain}`,
            value: `v=DMARC1; p=quarantine; rua=mailto:postmaster@${domain.domain}`,
          },
        ];
        copiedDnsIndex = null;
        dnsRecordsModalOpen = true;
      } else {
        toast.error(extractApiError(err));
      }
    } finally {
      loadingDns = false;
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
        aliases = [];
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

  function openAddAlias() {
    fAliasLocalPart = '';
    fAliasDestination = '';
    fAliasCatchAll = false;
    aliasFormError = '';
    addAliasOpen = true;
  }

  async function submitAlias(e: SubmitEvent) {
    e.preventDefault();
    if (!selectedDomain) return;
    aliasFormError = '';
    savingAlias = true;
    try {
      await api.post<MailAlias>(`/mail/domains/${selectedDomain.id}/aliases`, {
        local_part: fAliasCatchAll ? '*' : fAliasLocalPart.trim(),
        destination: fAliasDestination.trim(),
        is_catch_all: fAliasCatchAll,
      });
      toast.success(i18n.t.mail.alias_created);
      addAliasOpen = false;
      await loadAliases(selectedDomain.id);
    } catch (err) {
      aliasFormError = extractApiError(err);
    } finally {
      savingAlias = false;
    }
  }

  async function confirmDeleteAlias() {
    if (!deleteAliasTarget || !selectedDomain) return;
    deletingAlias = true;
    try {
      await api.delete(`/mail/aliases/${deleteAliasTarget.id}`);
      toast.success(i18n.t.mail.alias_deleted);
      deleteAliasTarget = null;
      await loadAliases(selectedDomain.id);
    } catch (err) {
      toast.error(extractApiError(err));
      deleteAliasTarget = null;
    } finally {
      deletingAlias = false;
    }
  }

  function extractApiError(err: unknown): string {
    if (err instanceof ApiError) {
      const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
      return fieldMsg || err.detail || err.title;
    }
    return i18n.t.common.errorGeneric;
  }

  function onServerProviderChange() {
    const preset = RELAY_PRESETS[srProvider];
    if (!preset) return;
    if (preset.host) srHost = preset.host;
    srPort = preset.port;
    if (preset.user !== undefined) srUsername = preset.user;
  }

  function onDomainProviderChange() {
    const preset = RELAY_PRESETS[drProvider];
    if (!preset) return;
    if (preset.host) drHost = preset.host;
    drPort = preset.port;
    if (preset.user !== undefined) drUsername = preset.user;
  }

  async function testRelayConnection(
    host: string,
    port: number,
    setTesting: (v: boolean) => void,
    setError: (msg: string) => void,
  ) {
    setError('');
    setTesting(true);
    try {
      await api.post('/mail/relay/test', { host: host.trim(), port: Number(port) || 587 });
      toast.success(i18n.t.mail.relayTestOk);
    } catch (err) {
      setError(extractApiError(err));
    } finally {
      setTesting(false);
    }
  }

  async function openServerRelay() {
    serverRelayError = '';
    srPassword = '';
    serverRelayOpen = true;
    loadingServerRelay = true;
    try {
      const cfg = await api.get<ServerSMTPRelay>('/mail/relay');
      srEnabled = cfg.enabled;
      srProvider = cfg.provider || 'ses';
      srHost = cfg.host || '';
      srPort = cfg.port || 587;
      srUsername = cfg.username || '';
      srTlsMode = cfg.tls_mode || 'starttls';
      srHasPassword = cfg.has_password;
      if (!srHost && RELAY_PRESETS[srProvider]?.host) {
        srHost = RELAY_PRESETS[srProvider].host;
      }
    } catch (err) {
      serverRelayError = extractApiError(err);
    } finally {
      loadingServerRelay = false;
    }
  }

  async function submitServerRelay(e: SubmitEvent) {
    e.preventDefault();
    serverRelayError = '';
    savingServerRelay = true;
    try {
      const res = await api.put<ServerSMTPRelay>('/mail/relay', {
        enabled: srEnabled,
        provider: srProvider,
        host: srHost.trim(),
        port: Number(srPort) || 587,
        username: srUsername.trim(),
        password: srPassword,
        tls_mode: srTlsMode,
      });
      srHasPassword = res.has_password;
      srPassword = '';
      toast.success(i18n.t.mail.relaySaved);
      serverRelayOpen = false;
    } catch (err) {
      serverRelayError = extractApiError(err);
    } finally {
      savingServerRelay = false;
    }
  }

  function openDomainRelay(domain: MailDomain) {
    domainRelayTarget = domain;
    domainRelayError = '';
    drEnabled = true;
    drProvider = domain.relay_provider || 'ses';
    drHost = domain.relay_host || RELAY_PRESETS[drProvider]?.host || '';
    drPort = domain.relay_port || RELAY_PRESETS[drProvider]?.port || 587;
    drUsername = domain.relay_user || '';
    drPassword = '';
  }

  async function submitDomainRelay(e: SubmitEvent) {
    e.preventDefault();
    if (!domainRelayTarget) return;
    domainRelayError = '';
    savingDomainRelay = true;
    try {
      await api.put(`/mail/domains/${domainRelayTarget.id}/relay`, {
        enabled: drEnabled,
        provider: drProvider,
        host: drHost.trim(),
        port: Number(drPort) || 587,
        username: drUsername.trim(),
        password: drPassword,
      });
      toast.success(i18n.t.mail.relaySaved);
      domainRelayTarget = null;
      await loadDomains();
    } catch (err) {
      domainRelayError = extractApiError(err);
    } finally {
      savingDomainRelay = false;
    }
  }

  async function clearDomainRelay() {
    if (!domainRelayTarget) return;
    domainRelayError = '';
    savingDomainRelay = true;
    try {
      await api.put(`/mail/domains/${domainRelayTarget.id}/relay`, { enabled: false });
      toast.success(i18n.t.mail.relaySaved);
      domainRelayTarget = null;
      await loadDomains();
    } catch (err) {
      domainRelayError = extractApiError(err);
    } finally {
      savingDomainRelay = false;
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
  <div class="table-actions">
    {#if auth.user?.role === 'admin'}
      <button type="button" class="btn btn-sm" onclick={openServerRelay}>
        {i18n.t.mail.serverRelayBtn}
      </button>
    {/if}
    <a class="btn btn-sm" href="/webmail/" target="_blank" rel="noopener noreferrer">
      {i18n.t.mail.webmailBtn}
    </a>
    <button type="button" class="btn btn-sm" onclick={() => (clientConfigOpen = true)}>
      {i18n.t.mail.clientConfigBtn}
    </button>
  </div>
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
              {#if d.relay_host}
                <div>
                  <span class="badge badge-info mono">{d.relay_host}:{d.relay_port || 587}</span>
                </div>
              {/if}
            </td>
            <td class="mono">{d.dkim_selector}</td>
            <td>{formatDateTime(d.created_at, localeCode)}</td>
            <td>
              <div class="table-actions">
                <button
                  type="button"
                  class="btn btn-sm btn-icon"
                  title={i18n.t.mail.viewDnsBtn}
                  aria-label={i18n.t.mail.viewDnsBtn}
                  onclick={() => openDnsRecords(d)}
                  disabled={loadingDns}
                >
                  <ActionIcon name="key" />
                </button>
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
                    class="btn btn-sm btn-icon"
                    title={i18n.t.mail.domainRelayTitle}
                    aria-label={i18n.t.mail.domainRelayTitle}
                    onclick={() => openDomainRelay(d)}
                  >
                    <ActionIcon name="deploy" />
                  </button>
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
      <h2 class="card-title">{selectedDomain.domain}</h2>
      <div class="table-actions">
        <button
          type="button"
          class="btn btn-sm"
          onclick={() => selectedDomain && openDnsRecords(selectedDomain)}
          disabled={loadingDns}
        >
          <ActionIcon name="key" />
          {i18n.t.mail.viewDnsBtn}
        </button>
        {#if domainTab === 'mailboxes'}
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
        {:else}
          <button
            type="button"
            class="btn btn-sm"
            onclick={() => selectedDomain && loadAliases(selectedDomain.id)}
            disabled={loadingAliases}
          >
            {i18n.t.common.refresh}
          </button>
          {#if canWrite}
            <button type="button" class="btn btn-sm btn-primary" onclick={openAddAlias}>
              + {i18n.t.mail.add_alias}
            </button>
          {/if}
        {/if}
      </div>
    </div>

    <div class="tabs" role="tablist" aria-label={selectedDomain.domain}>
      <button
        type="button"
        role="tab"
        aria-selected={domainTab === 'mailboxes'}
        class="tab-btn"
        class:active={domainTab === 'mailboxes'}
        onclick={() => (domainTab = 'mailboxes')}
      >
        {fmt(i18n.t.mail.mailboxesFor, { domain: selectedDomain.domain })}
      </button>
      <button
        type="button"
        role="tab"
        aria-selected={domainTab === 'aliases'}
        class="tab-btn"
        class:active={domainTab === 'aliases'}
        onclick={() => (domainTab = 'aliases')}
      >
        {i18n.t.mail.aliases}
      </button>
    </div>

    {#if domainTab === 'mailboxes'}
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
    {:else}
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>{i18n.t.mail.source}</th>
              <th>{i18n.t.mail.destination}</th>
              <th>{i18n.t.common.createdAt}</th>
              {#if canWrite}
                <th>{i18n.t.common.actions}</th>
              {/if}
            </tr>
          </thead>
          <tbody>
            {#if loadingAliases && aliases.length === 0}
              <tr><td colspan={canWrite ? 4 : 3} class="empty-state">{i18n.t.common.loading}</td></tr>
            {:else if aliases.length === 0}
              <tr><td colspan={canWrite ? 4 : 3} class="empty-state">{i18n.t.common.noData}</td></tr>
            {:else}
              {#each aliases as al (al.id)}
                <tr>
                  <td class="mono">
                    <strong>{al.source}</strong>
                    {#if al.is_catch_all}
                      <span class="badge badge-info">{i18n.t.mail.catch_all_badge}</span>
                    {/if}
                  </td>
                  <td class="mono">{al.destination}</td>
                  <td>{formatDateTime(al.created_at, localeCode)}</td>
                  {#if canWrite}
                    <td>
                      <button
                        type="button"
                        class="btn btn-sm btn-icon btn-danger"
                        title={i18n.t.common.delete}
                        aria-label={i18n.t.common.delete}
                        onclick={() => (deleteAliasTarget = al)}
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
    {/if}
  </div>
{/if}

{#if isAdmin}
  <hr class="section-divider" />
  <div class="card">
    <div class="toolbar">
      <h2 class="card-title">{i18n.t.mail.queue}</h2>
      <div class="table-actions">
        <button type="button" class="btn btn-sm" onclick={loadQueue} disabled={loadingQueue}>
          {i18n.t.common.refresh}
        </button>
        <button
          type="button"
          class="btn btn-sm btn-primary"
          onclick={flushQueue}
          disabled={flushingQueue || queueItems.length === 0}
        >
          {flushingQueue ? i18n.t.common.loading : i18n.t.mail.queue_flush}
        </button>
      </div>
    </div>

    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{i18n.t.mail.queue_id}</th>
            <th>{i18n.t.mail.queue_name}</th>
            <th>{i18n.t.mail.queue_sender}</th>
            <th>{i18n.t.mail.queue_recipients}</th>
            <th>{i18n.t.mail.queue_size}</th>
            <th>{i18n.t.mail.queue_arrival}</th>
            <th>{i18n.t.common.actions}</th>
          </tr>
        </thead>
        <tbody>
          {#if loadingQueue && queueItems.length === 0}
            <tr><td colspan="7" class="empty-state">{i18n.t.common.loading}</td></tr>
          {:else if queueItems.length === 0}
            <tr><td colspan="7" class="empty-state">{i18n.t.mail.queue_empty}</td></tr>
          {:else}
            {#each queueItems as q (q.queue_id)}
              <tr>
                <td class="mono"><strong>{q.queue_id}</strong></td>
                <td><span class="badge badge-info">{q.queue_name}</span></td>
                <td class="mono">{q.sender || '—'}</td>
                <td class="mono">
                  {#if q.recipients && q.recipients.length > 0}
                    {q.recipients.map((r) => (r.delay_reason ? `${r.address} (${r.delay_reason})` : r.address)).join(', ')}
                  {:else}
                    —
                  {/if}
                </td>
                <td class="mono">{q.message_size} B</td>
                <td>
                  {typeof q.arrival_time === 'number'
                    ? formatDateTime(new Date(q.arrival_time * 1000).toISOString(), localeCode)
                    : formatDateTime(String(q.arrival_time), localeCode)}
                </td>
                <td>
                  <button
                    type="button"
                    class="btn btn-sm btn-icon btn-danger"
                    title={i18n.t.common.delete}
                    aria-label={i18n.t.common.delete}
                    onclick={() => deleteQueueItem(q.queue_id)}
                    disabled={deletingQueueId === q.queue_id}
                  >
                    <ActionIcon name="delete" />
                  </button>
                </td>
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

<Modal
  open={serverRelayOpen}
  title={i18n.t.mail.serverRelayBtn}
  onclose={() => (serverRelayOpen = false)}
>
  <form id="server-relay-form" onsubmit={submitServerRelay}>
    {#if serverRelayError}
      <div class="alert alert-danger" role="alert">{serverRelayError}</div>
    {/if}

    <div class="form-group">
      <label class="checkbox-row">
        <input type="checkbox" bind:checked={srEnabled} disabled={loadingServerRelay} />
        <span>{i18n.t.mail.relayEnabled}</span>
      </label>
    </div>

    <div class="form-group">
      <label class="form-label" for="sr-provider">{i18n.t.mail.relayProvider}</label>
      <select
        id="sr-provider"
        class="form-control"
        bind:value={srProvider}
        onchange={onServerProviderChange}
        disabled={loadingServerRelay}
      >
        {#each Object.entries(RELAY_PRESETS) as [key, preset] (key)}
          <option value={key}>{preset.label}</option>
        {/each}
      </select>
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="sr-host">{i18n.t.mail.relayHost}</label>
        <input
          id="sr-host"
          type="text"
          class="form-control"
          bind:value={srHost}
          required={srEnabled}
          disabled={loadingServerRelay}
          placeholder="smtp.example.com"
        />
      </div>

      <div class="form-group">
        <label class="form-label" for="sr-port">{i18n.t.mail.relayPort}</label>
        <input
          id="sr-port"
          type="number"
          min="1"
          max="65535"
          class="form-control"
          bind:value={srPort}
          required={srEnabled}
          disabled={loadingServerRelay}
        />
      </div>
    </div>

    <div class="form-group">
      <label class="form-label" for="sr-tls">{i18n.t.mail.relayTls}</label>
      <select
        id="sr-tls"
        class="form-control"
        bind:value={srTlsMode}
        disabled={loadingServerRelay}
      >
        <option value="starttls">STARTTLS (587)</option>
        <option value="tls">TLS / SMTPS (465)</option>
        <option value="none">None</option>
      </select>
    </div>

    <div class="form-group">
      <label class="form-label" for="sr-user">{i18n.t.mail.relayUser}</label>
      <input
        id="sr-user"
        type="text"
        class="form-control"
        bind:value={srUsername}
        disabled={loadingServerRelay}
        autocomplete="off"
      />
    </div>

    <div class="form-group">
      <label class="form-label" for="sr-pass">{i18n.t.mail.relayPass}</label>
      <input
        id="sr-pass"
        type="password"
        class="form-control"
        bind:value={srPassword}
        disabled={loadingServerRelay}
        autocomplete="new-password"
      />
      {#if srHasPassword}
        <div class="form-hint">{i18n.t.mail.relayPassHint}</div>
      {/if}
    </div>
  </form>

  {#snippet footer()}
    <button
      type="button"
      class="btn"
      onclick={() =>
        testRelayConnection(
          srHost,
          srPort,
          (v) => (testingServerRelay = v),
          (msg) => (serverRelayError = msg),
        )}
      disabled={loadingServerRelay || testingServerRelay || !srHost.trim()}
    >
      {testingServerRelay ? i18n.t.common.loading : i18n.t.mail.relayTest}
    </button>
    <button type="button" class="btn" onclick={() => (serverRelayOpen = false)}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="submit"
      form="server-relay-form"
      class="btn btn-primary"
      disabled={loadingServerRelay || savingServerRelay}
    >
      {savingServerRelay ? i18n.t.common.loading : i18n.t.common.save}
    </button>
  {/snippet}
</Modal>

<Modal
  open={domainRelayTarget !== null}
  title={`${i18n.t.mail.domainRelayTitle}${domainRelayTarget ? ` — ${domainRelayTarget.domain}` : ''}`}
  onclose={() => (domainRelayTarget = null)}
>
  <form id="domain-relay-form" onsubmit={submitDomainRelay}>
    {#if domainRelayError}
      <div class="alert alert-danger" role="alert">{domainRelayError}</div>
    {/if}

    <div class="form-group">
      <label class="checkbox-row">
        <input type="checkbox" bind:checked={drEnabled} />
        <span>{i18n.t.mail.relayEnabled}</span>
      </label>
    </div>

    <div class="form-group">
      <label class="form-label" for="dr-provider">{i18n.t.mail.relayProvider}</label>
      <select
        id="dr-provider"
        class="form-control"
        bind:value={drProvider}
        onchange={onDomainProviderChange}
      >
        {#each Object.entries(RELAY_PRESETS) as [key, preset] (key)}
          <option value={key}>{preset.label}</option>
        {/each}
      </select>
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="dr-host">{i18n.t.mail.relayHost}</label>
        <input
          id="dr-host"
          type="text"
          class="form-control"
          bind:value={drHost}
          required={drEnabled}
          placeholder="smtp.example.com"
        />
      </div>

      <div class="form-group">
        <label class="form-label" for="dr-port">{i18n.t.mail.relayPort}</label>
        <input
          id="dr-port"
          type="number"
          min="1"
          max="65535"
          class="form-control"
          bind:value={drPort}
          required={drEnabled}
        />
      </div>
    </div>

    <div class="form-group">
      <label class="form-label" for="dr-user">{i18n.t.mail.relayUser}</label>
      <input
        id="dr-user"
        type="text"
        class="form-control"
        bind:value={drUsername}
        autocomplete="off"
      />
    </div>

    <div class="form-group">
      <label class="form-label" for="dr-pass">{i18n.t.mail.relayPass}</label>
      <input
        id="dr-pass"
        type="password"
        class="form-control"
        bind:value={drPassword}
        autocomplete="new-password"
      />
      {#if domainRelayTarget?.relay_host}
        <div class="form-hint">{i18n.t.mail.relayPassHint}</div>
      {/if}
    </div>
  </form>

  {#snippet footer()}
    {#if domainRelayTarget?.relay_host}
      <button
        type="button"
        class="btn btn-danger"
        onclick={clearDomainRelay}
        disabled={savingDomainRelay}
      >
        {i18n.t.mail.relayClear}
      </button>
    {/if}
    <button
      type="button"
      class="btn"
      onclick={() =>
        testRelayConnection(
          drHost,
          drPort,
          (v) => (testingDomainRelay = v),
          (msg) => (domainRelayError = msg),
        )}
      disabled={testingDomainRelay || !drHost.trim()}
    >
      {testingDomainRelay ? i18n.t.common.loading : i18n.t.mail.relayTest}
    </button>
    <button type="button" class="btn" onclick={() => (domainRelayTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="domain-relay-form" class="btn btn-primary" disabled={savingDomainRelay}>
      {savingDomainRelay ? i18n.t.common.loading : i18n.t.common.save}
    </button>
  {/snippet}
</Modal>

<Modal
  open={clientConfigOpen}
  title={i18n.t.mail.clientConfigTitle}
  size="lg"
  onclose={() => (clientConfigOpen = false)}
>
  <div class="grid-two">
    <div class="card">
      <h3 class="card-title">{i18n.t.mail.imapSection}</h3>
      <dl class="kv-list">
        <dt>{i18n.t.mail.serverHost}</dt>
        <dd>
          <span class="table-actions">
            <span class="mono">{clientMailHost}</span>
            <button
              type="button"
              class="btn btn-sm btn-icon"
              title={i18n.t.common.copy}
              aria-label={i18n.t.common.copy}
              onclick={() => copyText(clientMailHost)}
            >
              <ActionIcon name="copy" />
            </button>
          </span>
        </dd>
        <dt>{i18n.t.mail.relayPort}</dt>
        <dd>
          <span class="badge badge-info mono">993</span>
          <span>{i18n.t.mail.portSslTls}</span>
          /
          <span class="badge badge-info mono">143</span>
          <span>{i18n.t.mail.portStartTls}</span>
        </dd>
        <dt>{i18n.t.mail.relayUser}</dt>
        <dd>{i18n.t.mail.usernameHint}</dd>
      </dl>
    </div>

    <div class="card">
      <h3 class="card-title">{i18n.t.mail.smtpSection}</h3>
      <dl class="kv-list">
        <dt>{i18n.t.mail.serverHost}</dt>
        <dd>
          <span class="table-actions">
            <span class="mono">{clientMailHost}</span>
            <button
              type="button"
              class="btn btn-sm btn-icon"
              title={i18n.t.common.copy}
              aria-label={i18n.t.common.copy}
              onclick={() => copyText(clientMailHost)}
            >
              <ActionIcon name="copy" />
            </button>
          </span>
        </dd>
        <dt>{i18n.t.mail.relayPort}</dt>
        <dd>
          <span class="badge badge-info mono">465</span>
          <span>{i18n.t.mail.portSslTls}</span>
          /
          <span class="badge badge-info mono">587</span>
          <span>{i18n.t.mail.portStartTls}</span>
        </dd>
        <dt>{i18n.t.mail.relayUser}</dt>
        <dd>{i18n.t.mail.authRequired}</dd>
      </dl>
    </div>
  </div>

  {#snippet footer()}
    <button type="button" class="btn btn-primary" onclick={() => (clientConfigOpen = false)}>
      {i18n.t.common.close}
    </button>
  {/snippet}
</Modal>

<Modal
  open={addAliasOpen}
  title={i18n.t.mail.add_alias}
  onclose={() => (addAliasOpen = false)}
>
  <form id="alias-form" onsubmit={submitAlias}>
    {#if aliasFormError}
      <div class="alert alert-danger" role="alert">{aliasFormError}</div>
    {/if}

    <div class="form-group">
      <label class="checkbox-row">
        <input type="checkbox" bind:checked={fAliasCatchAll} />
        <span>{i18n.t.mail.catch_all}</span>
      </label>
    </div>

    {#if !fAliasCatchAll}
      <div class="form-group">
        <label class="form-label" for="alias-local">{i18n.t.mail.localPart}</label>
        <input
          id="alias-local"
          type="text"
          class="form-control"
          bind:value={fAliasLocalPart}
          required={!fAliasCatchAll}
          placeholder={i18n.t.mail.localPartPlaceholder}
        />
        {#if selectedDomain}
          <div class="form-hint mono">{fAliasLocalPart.trim() || 'alias'}@{selectedDomain.domain}</div>
        {/if}
      </div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="alias-dest">{i18n.t.mail.destination}</label>
      <input
        id="alias-dest"
        type="email"
        class="form-control"
        bind:value={fAliasDestination}
        required
        placeholder="admin@example.com"
      />
    </div>
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (addAliasOpen = false)}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="alias-form" class="btn btn-primary" disabled={savingAlias}>
      {savingAlias ? i18n.t.common.loading : i18n.t.common.create}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteAliasTarget !== null}
  title={i18n.t.common.delete}
  onclose={() => (deleteAliasTarget = null)}
>
  <p class="mono">
    {deleteAliasTarget ? `${deleteAliasTarget.source} -> ${deleteAliasTarget.destination}` : ''}
  </p>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (deleteAliasTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-danger"
      onclick={confirmDeleteAlias}
      disabled={deletingAlias}
    >
      {deletingAlias ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  {/snippet}
</Modal>

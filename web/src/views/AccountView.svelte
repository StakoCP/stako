<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import TwoFactorSetupPanel from '../components/TwoFactorSetupPanel.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatDateTime } from '../lib/format';
  import { i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { ApiKey, ApiKeyScope, AuditEntry, Session } from '../lib/types';

  type AccountTab = 'profile' | 'security' | 'sessions' | 'apikeys' | 'activity';

  interface AccountUsage {
    domains: number;
    databases: number;
    mail_domains: number;
    containers: number;
    disk_used_mb: number;
    disk_limit_mb: number;
    inode_used: number;
    inode_limit: number;
  }

  let activeTab = $state<AccountTab>('profile');

  // Profile
  let profileEmail = $state(auth.user?.email ?? '');
  let profileError = $state('');
  let profileSaving = $state(false);

  // Usage
  let usage = $state<AccountUsage | null>(null);
  let usageLoading = $state(true);

  // Recent activity (admin only)
  let activity = $state<AuditEntry[]>([]);
  let activityLoading = $state(false);
  let activityLoaded = $state(false);

  // Change password
  let currentPassword = $state('');
  let newPassword = $state('');
  let confirmPassword = $state('');
  let pwError = $state('');
  let pwSaving = $state(false);

  // 2FA
  let show2faSetup = $state(false);
  let disableCode = $state('');
  let disabling2fa = $state(false);

  // Sessions
  let sessions = $state<Session[]>([]);
  let sessionsLoading = $state(true);

  // API keys
  let apiKeys = $state<ApiKey[]>([]);
  let keysLoading = $state(true);
  let createKeyOpen = $state(false);
  let kName = $state('');
  let kScopeRead = $state(true);
  let kScopeWrite = $state(false);
  let kAllowedIps = $state('');
  let kExpiresDays = $state('90');
  let kError = $state('');
  let kSaving = $state(false);
  let createdToken = $state<string | null>(null);
  let copiedToken = $state(false);

  const localeCode = $derived(i18n.locale === 'id' ? 'id-ID' : 'en-US');

  $effect(() => {
    if (auth.user && !profileEmail) {
      profileEmail = auth.user.email;
    }
  });

  async function loadUsage() {
    usageLoading = true;
    try {
      usage = await api.get<AccountUsage>('/auth/me/usage');
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      usageLoading = false;
    }
  }

  async function loadSessions() {
    sessionsLoading = true;
    try {
      const res = await api.get<{ items: Session[] }>('/auth/sessions');
      sessions = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      sessionsLoading = false;
    }
  }

  async function loadApiKeys() {
    keysLoading = true;
    try {
      const res = await api.get<{ items: ApiKey[] }>('/api-keys');
      apiKeys = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      keysLoading = false;
    }
  }

  async function loadActivity() {
    if (auth.user?.role !== 'admin') return;
    activityLoading = true;
    try {
      const res = await api.get<{ items: AuditEntry[] }>(
        `/audit?actor=${encodeURIComponent(auth.user.username)}&limit=15`,
      );
      activity = res.items;
      activityLoaded = true;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      activityLoading = false;
    }
  }

  function selectTab(tab: AccountTab) {
    activeTab = tab;
    if (tab === 'activity' && !activityLoaded) {
      loadActivity();
    }
  }

  onMount(() => {
    loadUsage();
    loadSessions();
    loadApiKeys();
  });

  async function saveProfile(e: SubmitEvent) {
    e.preventDefault();
    profileError = '';
    profileSaving = true;
    try {
      await api.patch('/auth/profile', { email: profileEmail.trim() });
      await auth.refreshMe();
      profileEmail = auth.user?.email ?? profileEmail.trim();
      toast.success(i18n.t.account.profileSaved);
    } catch (err) {
      profileError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      profileSaving = false;
    }
  }

  async function changePassword(e: SubmitEvent) {
    e.preventDefault();
    pwError = '';
    if (newPassword.length < 12) {
      pwError = i18n.t.users.passwordTooShort;
      return;
    }
    if (newPassword !== confirmPassword) {
      pwError = i18n.t.account.passwordMismatch;
      return;
    }
    pwSaving = true;
    try {
      await api.post('/auth/password', {
        current_password: currentPassword,
        new_password: newPassword,
      });
      currentPassword = '';
      newPassword = '';
      confirmPassword = '';
      toast.success(i18n.t.account.passwordChanged);
      await loadSessions();
    } catch (err) {
      pwError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      pwSaving = false;
    }
  }

  async function disable2fa(e: SubmitEvent) {
    e.preventDefault();
    disabling2fa = true;
    try {
      await api.post('/auth/2fa/disable', { code: disableCode.trim() });
      disableCode = '';
      toast.success(i18n.t.account.twoFactorNowDisabled);
      await auth.refreshMe();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      disabling2fa = false;
    }
  }

  async function revokeSession(id: string) {
    try {
      await api.delete(`/auth/sessions/${encodeURIComponent(id)}`);
      toast.success(i18n.t.account.sessionRevoked);
      await loadSessions();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    }
  }

  async function revokeOtherSessions() {
    try {
      await api.post('/auth/sessions/revoke-others');
      toast.success(i18n.t.account.otherSessionsRevoked);
      await loadSessions();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    }
  }

  function openCreateKey() {
    kName = '';
    kScopeRead = true;
    kScopeWrite = false;
    kAllowedIps = '';
    kExpiresDays = '90';
    kError = '';
    createKeyOpen = true;
  }

  async function createApiKey(e: SubmitEvent) {
    e.preventDefault();
    kError = '';
    const scopes: ApiKeyScope[] = [];
    if (kScopeRead) scopes.push('read');
    if (kScopeWrite) scopes.push('write');
    if (scopes.length === 0) {
      kError = 'Select at least one scope.';
      return;
    }
    const body: Record<string, unknown> = {
      name: kName.trim(),
      scopes,
    };
    const ips = kAllowedIps
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
    if (ips.length > 0) body.allowed_ips = ips;
    const days = Number(kExpiresDays);
    if (kExpiresDays.trim() && Number.isFinite(days) && days > 0) {
      body.expires_in_days = days;
    }

    kSaving = true;
    try {
      const res = await api.post<{ api_key: ApiKey; token: string }>('/api-keys', body);
      createKeyOpen = false;
      createdToken = res.token;
      copiedToken = false;
      await loadApiKeys();
    } catch (err) {
      kError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      kSaving = false;
    }
  }

  async function deleteApiKey(id: string) {
    try {
      await api.delete(`/api-keys/${encodeURIComponent(id)}`);
      toast.success(i18n.t.account.apiKeyDeleted);
      await loadApiKeys();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    }
  }

  async function copyToken() {
    if (!createdToken) return;
    await navigator.clipboard.writeText(createdToken);
    copiedToken = true;
    setTimeout(() => (copiedToken = false), 2000);
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.account.title}</h1>
    <p class="page-subtitle">{i18n.t.account.subtitle}</p>
  </div>
</div>

<div class="tabs" role="tablist" aria-label={i18n.t.account.title}>
  <button
    type="button"
    role="tab"
    aria-selected={activeTab === 'profile'}
    class="tab-btn"
    class:active={activeTab === 'profile'}
    onclick={() => selectTab('profile')}
  >
    {i18n.t.account.tabProfile}
  </button>
  <button
    type="button"
    role="tab"
    aria-selected={activeTab === 'security'}
    class="tab-btn"
    class:active={activeTab === 'security'}
    onclick={() => selectTab('security')}
  >
    {i18n.t.account.tabSecurity}
  </button>
  <button
    type="button"
    role="tab"
    aria-selected={activeTab === 'sessions'}
    class="tab-btn"
    class:active={activeTab === 'sessions'}
    onclick={() => selectTab('sessions')}
  >
    {i18n.t.account.tabSessions}
  </button>
  <button
    type="button"
    role="tab"
    aria-selected={activeTab === 'apikeys'}
    class="tab-btn"
    class:active={activeTab === 'apikeys'}
    onclick={() => selectTab('apikeys')}
  >
    {i18n.t.account.tabApiKeys}
  </button>
  {#if auth.user?.role === 'admin'}
    <button
      type="button"
      role="tab"
      aria-selected={activeTab === 'activity'}
      class="tab-btn"
      class:active={activeTab === 'activity'}
      onclick={() => selectTab('activity')}
    >
      {i18n.t.account.tabActivity}
    </button>
  {/if}
</div>

{#if activeTab === 'profile'}
  <div class="grid-two">
    <div class="card">
      <h2 class="card-title">{i18n.t.account.profileTitle}</h2>
      {#if auth.user}
        <dl class="kv-list">
          <dt>{i18n.t.account.username}</dt>
          <dd class="mono">{auth.user.username}</dd>

          <dt>{i18n.t.account.role}</dt>
          <dd>
            <span
              class="badge"
              class:badge-primary={auth.user.role === 'admin' || auth.user.role === 'reseller'}
              class:badge-info={auth.user.role === 'user' || auth.user.role === 'readonly'}
            >
              {auth.user.role}
            </span>
          </dd>

          <dt>{i18n.t.account.status}</dt>
          <dd>
            <span
              class="badge"
              class:badge-success={auth.user.status === 'active'}
              class:badge-danger={auth.user.status !== 'active'}
            >
              {auth.user.status}
            </span>
          </dd>

          <dt>{i18n.t.account.twoFactorStatus}</dt>
          <dd>
            <span
              class="badge"
              class:badge-success={auth.user.totp_enabled}
              class:badge-warning={!auth.user.totp_enabled}
            >
              {auth.user.totp_enabled ? i18n.t.account.twoFactorEnabled : i18n.t.account.twoFactorDisabled}
            </span>
          </dd>

          <dt>{i18n.t.account.createdAt}</dt>
          <dd>{formatDateTime(auth.user.created_at, localeCode)}</dd>
        </dl>

        <form onsubmit={saveProfile}>
          {#if profileError}
            <div class="alert alert-danger" role="alert">{profileError}</div>
          {/if}
          <div class="form-group">
            <label class="form-label" for="profile-email">{i18n.t.account.email}</label>
            <input
              id="profile-email"
              type="email"
              class="form-control"
              bind:value={profileEmail}
              disabled={auth.user.role === 'readonly'}
              required
            />
          </div>
          {#if auth.user.role !== 'readonly'}
            <button type="submit" class="btn btn-primary" disabled={profileSaving}>
              {profileSaving ? i18n.t.common.loading : i18n.t.account.saveProfile}
            </button>
          {/if}
        </form>
      {:else}
        <p class="empty-state">{i18n.t.common.loading}</p>
      {/if}
    </div>

    <div class="card">
      <h2 class="card-title">{i18n.t.account.usageTitle}</h2>
      {#if usageLoading && !usage}
        <p class="empty-state">{i18n.t.common.loading}</p>
      {:else if usage}
        <dl class="kv-list">
          <dt>{i18n.t.account.domainsCount}</dt>
          <dd class="mono">{usage.domains}</dd>

          <dt>{i18n.t.account.databasesCount}</dt>
          <dd class="mono">{usage.databases}</dd>

          <dt>{i18n.t.account.mailDomainsCount}</dt>
          <dd class="mono">{usage.mail_domains}</dd>

          <dt>{i18n.t.account.containersCount}</dt>
          <dd class="mono">{usage.containers}</dd>

          <dt>{i18n.t.account.diskQuotaLabel}</dt>
          <dd class="mono">
            {usage.disk_used_mb} MB / {usage.disk_limit_mb > 0 ? `${usage.disk_limit_mb} MB` : '∞'}
          </dd>

          <dt>{i18n.t.account.inodeQuotaLabel}</dt>
          <dd class="mono">
            {usage.inode_used} / {usage.inode_limit > 0 ? usage.inode_limit : '∞'}
          </dd>
        </dl>
      {:else}
        <p class="empty-state">{i18n.t.common.noData}</p>
      {/if}
    </div>
  </div>
{:else if activeTab === 'security'}
  <div class="grid-two">
    <div class="card">
      <h2 class="card-title">{i18n.t.account.changePassword}</h2>
      <form onsubmit={changePassword}>
        {#if pwError}
          <div class="alert alert-danger" role="alert">{pwError}</div>
        {/if}
        <div class="form-group">
          <label class="form-label" for="pw-current">{i18n.t.account.currentPassword}</label>
          <input
            id="pw-current"
            type="password"
            class="form-control"
            bind:value={currentPassword}
            autocomplete="current-password"
            required
          />
        </div>
        <div class="form-group">
          <label class="form-label" for="pw-new">{i18n.t.account.newPassword}</label>
          <input
            id="pw-new"
            type="password"
            class="form-control"
            bind:value={newPassword}
            minlength={12}
            autocomplete="new-password"
            required
          />
          <div class="form-hint">{i18n.t.users.passwordHint}</div>
        </div>
        <div class="form-group">
          <label class="form-label" for="pw-confirm">{i18n.t.account.confirmPassword}</label>
          <input
            id="pw-confirm"
            type="password"
            class="form-control"
            bind:value={confirmPassword}
            minlength={12}
            autocomplete="new-password"
            required
          />
        </div>
        <button type="submit" class="btn btn-primary" disabled={pwSaving}>
          {pwSaving ? i18n.t.common.loading : i18n.t.account.changePassword}
        </button>
      </form>
    </div>

    <div class="card">
      <h2 class="card-title">{i18n.t.account.twoFactorTitle}</h2>
      {#if auth.user?.totp_enabled}
        <div class="alert alert-success">{i18n.t.account.twoFactorEnabled}</div>
        <form onsubmit={disable2fa}>
          <div class="form-group">
            <label class="form-label" for="disable-2fa-code">{i18n.t.account.enterCodeToDisable}</label>
            <input
              id="disable-2fa-code"
              type="text"
              class="form-control"
              bind:value={disableCode}
              inputmode="numeric"
              autocomplete="one-time-code"
              placeholder="123456"
              required
            />
          </div>
          <button type="submit" class="btn btn-danger" disabled={disabling2fa}>
            {disabling2fa ? i18n.t.common.loading : i18n.t.account.disable2fa}
          </button>
        </form>
      {:else if show2faSetup}
        <TwoFactorSetupPanel
          oncompleted={async () => {
            show2faSetup = false;
            await auth.refreshMe();
          }}
          oncancel={() => (show2faSetup = false)}
        />
      {:else}
        <p class="page-subtitle">{i18n.t.account.twoFactorDisabled}</p>
        <button type="button" class="btn btn-primary" onclick={() => (show2faSetup = true)}>
          {i18n.t.account.setup2fa}
        </button>
      {/if}
    </div>
  </div>
{:else if activeTab === 'sessions'}
  <div class="card">
    <div class="toolbar">
      <h2 class="card-title">{i18n.t.account.sessionsTitle}</h2>
      <button type="button" class="btn btn-sm" onclick={revokeOtherSessions}>
        {i18n.t.account.revokeOtherSessions}
      </button>
    </div>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{i18n.t.account.ipAddress}</th>
            <th>{i18n.t.account.userAgent}</th>
            <th>{i18n.t.account.lastSeen}</th>
            <th>{i18n.t.common.actions}</th>
          </tr>
        </thead>
        <tbody>
          {#if sessionsLoading && sessions.length === 0}
            <tr><td colspan="4" class="empty-state">{i18n.t.common.loading}</td></tr>
          {:else if sessions.length === 0}
            <tr><td colspan="4" class="empty-state">{i18n.t.common.noData}</td></tr>
          {:else}
            {#each sessions as s (s.id)}
              <tr>
                <td class="mono">
                  {s.ip}
                  {#if s.current}
                    <span class="badge badge-success">{i18n.t.account.currentSession}</span>
                  {/if}
                </td>
                <td>{s.user_agent}</td>
                <td>{formatDateTime(s.last_seen_at, localeCode)}</td>
                <td>
                  {#if !s.current}
                    <button
                      type="button"
                      class="btn btn-sm btn-icon btn-danger"
                      title={i18n.t.account.revokeSession}
                      aria-label={i18n.t.account.revokeSession}
                      onclick={() => revokeSession(s.id)}
                    >
                      <ActionIcon name="delete" />
                    </button>
                  {/if}
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
  </div>
{:else if activeTab === 'apikeys'}
  <div class="card">
    <div class="toolbar">
      <h2 class="card-title">{i18n.t.account.apiKeysTitle}</h2>
      <button type="button" class="btn btn-primary btn-sm" onclick={openCreateKey}>
        + {i18n.t.account.createApiKey}
      </button>
    </div>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{i18n.t.account.keyName}</th>
            <th>{i18n.t.account.prefix}</th>
            <th>{i18n.t.account.scopes}</th>
            <th>{i18n.t.account.expiresAt}</th>
            <th>{i18n.t.account.lastUsed}</th>
            <th>{i18n.t.common.actions}</th>
          </tr>
        </thead>
        <tbody>
          {#if keysLoading && apiKeys.length === 0}
            <tr><td colspan="6" class="empty-state">{i18n.t.common.loading}</td></tr>
          {:else if apiKeys.length === 0}
            <tr><td colspan="6" class="empty-state">{i18n.t.common.noData}</td></tr>
          {:else}
            {#each apiKeys as k (k.id)}
              <tr>
                <td><strong>{k.name}</strong></td>
                <td class="mono">{k.prefix}…</td>
                <td class="mono">{k.scopes.join(', ')}</td>
                <td>{formatDateTime(k.expires_at, localeCode)}</td>
                <td>{formatDateTime(k.last_used_at, localeCode)}</td>
                <td>
                  <button
                    type="button"
                    class="btn btn-sm btn-icon btn-danger"
                    title={i18n.t.common.delete}
                    aria-label={i18n.t.common.delete}
                    onclick={() => deleteApiKey(k.id)}
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
{:else if activeTab === 'activity' && auth.user?.role === 'admin'}
  <div class="card">
    <div class="toolbar">
      <h2 class="card-title">{i18n.t.account.activityTitle}</h2>
      <button type="button" class="btn btn-sm" onclick={loadActivity} disabled={activityLoading}>
        {i18n.t.common.refresh}
      </button>
    </div>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{i18n.t.audit.timestamp}</th>
            <th>{i18n.t.audit.action}</th>
            <th>{i18n.t.audit.object}</th>
            <th>{i18n.t.audit.result}</th>
            <th>{i18n.t.audit.ip}</th>
          </tr>
        </thead>
        <tbody>
          {#if activityLoading && activity.length === 0}
            <tr><td colspan="5" class="empty-state">{i18n.t.common.loading}</td></tr>
          {:else if activity.length === 0}
            <tr><td colspan="5" class="empty-state">{i18n.t.account.noActivity}</td></tr>
          {:else}
            {#each activity as entry (entry.id)}
              <tr>
                <td>{formatDateTime(entry.at, localeCode)}</td>
                <td class="mono">{entry.action}</td>
                <td class="mono">{entry.object_type}</td>
                <td>
                  <span
                    class="badge"
                    class:badge-success={entry.result === 'success'}
                    class:badge-danger={entry.result !== 'success'}
                  >
                    {entry.result}
                  </span>
                </td>
                <td class="mono">{entry.ip}</td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
  </div>
{/if}

<Modal
  open={createKeyOpen}
  title={i18n.t.account.createApiKey}
  onclose={() => (createKeyOpen = false)}
>
  <form id="apikey-form" onsubmit={createApiKey}>
    {#if kError}
      <div class="alert alert-danger" role="alert">{kError}</div>
    {/if}
    <div class="form-group">
      <label class="form-label" for="k-name">{i18n.t.account.keyName}</label>
      <input id="k-name" type="text" class="form-control" bind:value={kName} required />
    </div>
    <div class="form-group">
      <span class="form-label">{i18n.t.account.scopes}</span>
      <div class="checkbox-group">
        <label class="checkbox-row">
          <input type="checkbox" bind:checked={kScopeRead} />
          <span class="mono">read</span>
        </label>
        <label class="checkbox-row">
          <input type="checkbox" bind:checked={kScopeWrite} />
          <span class="mono">write</span>
        </label>
      </div>
    </div>
    <div class="form-group">
      <label class="form-label" for="k-ips">{i18n.t.account.allowedIps}</label>
      <input
        id="k-ips"
        type="text"
        class="form-control"
        bind:value={kAllowedIps}
        placeholder="203.0.113.0/24"
      />
    </div>
    <div class="form-group">
      <label class="form-label" for="k-exp">{i18n.t.account.expiresInDays}</label>
      <input id="k-exp" type="number" min="1" class="form-control" bind:value={kExpiresDays} />
    </div>
  </form>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (createKeyOpen = false)} disabled={kSaving}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="apikey-form" class="btn btn-primary" disabled={kSaving}>
      {kSaving ? i18n.t.common.loading : i18n.t.common.create}
    </button>
  {/snippet}
</Modal>

<Modal
  open={createdToken !== null}
  title={i18n.t.account.tokenOnceTitle}
  onclose={() => (createdToken = null)}
>
  <div class="alert alert-warning">{i18n.t.account.tokenOnceDesc}</div>
  {#if createdToken}
    <div class="secret-box">
      <span>{createdToken}</span>
      <button type="button" class="btn btn-sm" onclick={copyToken}>
        {copiedToken ? i18n.t.common.copied : i18n.t.common.copy}
      </button>
    </div>
  {/if}
  {#snippet footer()}
    <button type="button" class="btn btn-primary" onclick={() => (createdToken = null)}>
      {i18n.t.common.close}
    </button>
  {/snippet}
</Modal>

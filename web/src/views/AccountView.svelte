<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '../components/Modal.svelte';
  import TwoFactorSetupPanel from '../components/TwoFactorSetupPanel.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatDateTime } from '../lib/format';
  import { i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { ApiKey, ApiKeyScope, Session } from '../lib/types';

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

  onMount(() => {
    loadSessions();
    loadApiKeys();
  });

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
              <td>{formatDateTime(s.last_seen_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</td>
              <td>
                {#if !s.current}
                  <button
                    type="button"
                    class="btn btn-sm btn-danger"
                    onclick={() => revokeSession(s.id)}
                  >
                    {i18n.t.account.revokeSession}
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
              <td>{formatDateTime(k.expires_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</td>
              <td>{formatDateTime(k.last_used_at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</td>
              <td>
                <button
                  type="button"
                  class="btn btn-sm btn-danger"
                  onclick={() => deleteApiKey(k.id)}
                >
                  {i18n.t.common.delete}
                </button>
              </td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
</div>

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

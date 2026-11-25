<script lang="ts">
  import { ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { i18n } from '../lib/i18n.svelte';
  import { themeStore } from '../lib/theme.svelte';

  let username = $state('');
  let password = $state('');
  let totpCode = $state('');
  let needsTotp = $state(false);
  let loading = $state(false);
  let error = $state('');

  async function handleSubmit(e: SubmitEvent) {
    e.preventDefault();
    loading = true;
    error = '';
    try {
      await auth.login(username.trim(), password, needsTotp ? totpCode.trim() : undefined);
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.type === 'urn:stako:problem:totp-required') {
          needsTotp = true;
          totpCode = '';
          error = '';
        } else if (err.status === 429 || err.type === 'urn:stako:problem:rate-limited') {
          error = i18n.t.login.rateLimited;
        } else if (err.type === 'urn:stako:problem:invalid-credentials' || err.status === 401) {
          error = i18n.t.login.invalidCredentials;
        } else {
          error = i18n.t.common.errorGeneric;
        }
      } else {
        error = i18n.t.common.errorGeneric;
      }
    } finally {
      loading = false;
    }
  }

  function resetToPassword() {
    needsTotp = false;
    totpCode = '';
    error = '';
  }
</script>

<div class="auth-page">
  <div class="auth-card">
    <div class="auth-header">
      <div class="topbar-left">
        <span class="brand-mark">S</span>
        <strong>{i18n.t.appName}</strong>
      </div>
      <div class="topbar-right">
        <button type="button" class="btn btn-sm" onclick={() => i18n.toggleLocale()}>
          {i18n.locale.toUpperCase()}
        </button>
        <button
          type="button"
          class="btn btn-sm"
          onclick={() => themeStore.toggle()}
          aria-label="Theme"
        >
          {themeStore.current === 'dark' ? '☀' : '☾'}
        </button>
      </div>
    </div>

    <h1 class="page-title">{needsTotp ? i18n.t.login.totpTitle : i18n.t.login.title}</h1>
    <p class="page-subtitle">{needsTotp ? i18n.t.login.totpSubtitle : i18n.t.login.subtitle}</p>

    <form onsubmit={handleSubmit}>
      {#if error}
        <div class="alert alert-danger" role="alert">{error}</div>
      {/if}

      {#if !needsTotp}
        <div class="form-group">
          <label class="form-label" for="login-username">{i18n.t.login.username}</label>
          <input
            id="login-username"
            type="text"
            class="form-control"
            bind:value={username}
            autocomplete="username"
            required
          />
        </div>
        <div class="form-group">
          <label class="form-label" for="login-password">{i18n.t.login.password}</label>
          <input
            id="login-password"
            type="password"
            class="form-control"
            bind:value={password}
            autocomplete="current-password"
            required
          />
        </div>
        <button type="submit" class="btn btn-primary btn-block" disabled={loading}>
          {loading ? i18n.t.common.loading : i18n.t.login.submit}
        </button>
      {:else}
        <div class="form-group">
          <label class="form-label" for="login-totp">{i18n.t.login.totpCode}</label>
          <input
            id="login-totp"
            type="text"
            class="form-control"
            bind:value={totpCode}
            autocomplete="one-time-code"
            placeholder="123456"
            required
          />
        </div>
        <div class="table-actions">
          <button type="submit" class="btn btn-primary" disabled={loading}>
            {loading ? i18n.t.common.loading : i18n.t.login.verify}
          </button>
          <button type="button" class="btn" onclick={resetToPassword} disabled={loading}>
            {i18n.t.login.backToLogin}
          </button>
        </div>
      {/if}
    </form>
  </div>
</div>

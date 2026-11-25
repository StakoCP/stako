<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from './Modal.svelte';
  import { api, ApiError, registerReauthHandler } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { i18n } from '../lib/i18n.svelte';

  let open = $state(false);
  let password = $state('');
  let totpCode = $state('');
  let error = $state('');
  let submitting = $state(false);
  let resolver: ((ok: boolean) => void) | null = null;

  onMount(() => {
    registerReauthHandler(() => {
      return new Promise<boolean>((resolve) => {
        if (resolver) {
          resolver(false);
        }
        resolver = resolve;
        password = '';
        totpCode = '';
        error = '';
        open = true;
      });
    });
  });

  function closeCancel() {
    open = false;
    if (resolver) {
      resolver(false);
      resolver = null;
    }
  }

  async function handleSubmit(e: SubmitEvent) {
    e.preventDefault();
    if (!password) return;
    submitting = true;
    error = '';
    try {
      const body: Record<string, string> = { password };
      if (totpCode.trim()) {
        body.totp_code = totpCode.trim();
      }
      await api.post('/auth/reauth', body, { skipReauth: true, skipAuthRedirect: true });
      open = false;
      const res = resolver;
      resolver = null;
      res?.(true);
    } catch (err) {
      if (err instanceof ApiError && err.detail) {
        error = err.detail;
      } else {
        error = i18n.t.reauth.failed;
      }
    } finally {
      submitting = false;
    }
  }
</script>

<Modal open={open} title={i18n.t.reauth.title} onclose={closeCancel}>
  <p class="page-subtitle">{i18n.t.reauth.subtitle}</p>
  <form id="reauth-form" onsubmit={handleSubmit}>
    {#if error}
      <div class="alert alert-danger" role="alert">{error}</div>
    {/if}
    <div class="form-group">
      <label class="form-label" for="reauth-password">{i18n.t.reauth.password}</label>
      <input
        id="reauth-password"
        type="password"
        class="form-control"
        bind:value={password}
        autocomplete="current-password"
        required
      />
    </div>
    {#if auth.user?.totp_enabled}
      <div class="form-group">
        <label class="form-label" for="reauth-totp">{i18n.t.reauth.totpCode}</label>
        <input
          id="reauth-totp"
          type="text"
          class="form-control"
          bind:value={totpCode}
          inputmode="numeric"
          autocomplete="one-time-code"
          placeholder="123456"
        />
      </div>
    {/if}
  </form>
  {#snippet footer()}
    <button type="button" class="btn" onclick={closeCancel} disabled={submitting}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="reauth-form" class="btn btn-primary" disabled={submitting}>
      {submitting ? i18n.t.common.loading : i18n.t.reauth.submit}
    </button>
  {/snippet}
</Modal>

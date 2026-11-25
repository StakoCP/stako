<script lang="ts">
  import QRCode from 'qrcode';
  import { api, ApiError } from '../lib/api';
  import { i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';

  interface Props {
    oncompleted: () => void;
    oncancel?: () => void;
  }

  let { oncompleted, oncancel }: Props = $props();

  let step = $state<'init' | 'verify' | 'codes'>('init');
  let loading = $state(false);
  let error = $state('');
  let secret = $state('');
  let qrDataUrl = $state('');
  let code = $state('');
  let recoveryCodes = $state<string[]>([]);
  let copiedSecret = $state(false);
  let copiedCodes = $state(false);

  async function startSetup() {
    loading = true;
    error = '';
    try {
      const res = await api.post<{ secret: string; otpauth_url: string }>('/auth/2fa/setup');
      secret = res.secret;
      qrDataUrl = await QRCode.toDataURL(res.otpauth_url, {
        width: 192,
        margin: 1,
      });
      step = 'verify';
    } catch (err) {
      error = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      loading = false;
    }
  }

  async function enable2fa(e: SubmitEvent) {
    e.preventDefault();
    loading = true;
    error = '';
    try {
      const res = await api.post<{ recovery_codes: string[] }>('/auth/2fa/enable', {
        code: code.trim(),
      });
      recoveryCodes = res.recovery_codes;
      step = 'codes';
      toast.success(i18n.t.account.twoFactorNowEnabled);
    } catch (err) {
      error = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      loading = false;
    }
  }

  async function copyText(text: string, which: 'secret' | 'codes') {
    await navigator.clipboard.writeText(text);
    if (which === 'secret') {
      copiedSecret = true;
      setTimeout(() => (copiedSecret = false), 2000);
    } else {
      copiedCodes = true;
      setTimeout(() => (copiedCodes = false), 2000);
    }
  }

  function downloadCodes() {
    const blob = new Blob([recoveryCodes.join('\n') + '\n'], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'stako-recovery-codes.txt';
    a.click();
    URL.revokeObjectURL(url);
  }
</script>

{#if error}
  <div class="alert alert-danger" role="alert">{error}</div>
{/if}

{#if step === 'init'}
  <div class="table-actions">
    <button type="button" class="btn btn-primary" onclick={startSetup} disabled={loading}>
      {loading ? i18n.t.common.loading : i18n.t.account.setup2fa}
    </button>
    {#if oncancel}
      <button type="button" class="btn" onclick={oncancel}>{i18n.t.common.cancel}</button>
    {/if}
  </div>
{:else if step === 'verify'}
  <p class="page-subtitle">{i18n.t.account.scanQr}</p>
  {#if qrDataUrl}
    <div class="qr-container">
      <img src={qrDataUrl} alt="2FA QR Code" class="qr-image" />
    </div>
  {/if}
  <div class="form-group">
    <span class="form-label">{i18n.t.account.secretKey}</span>
    <div class="secret-box">
      <span>{secret}</span>
      <button type="button" class="btn btn-sm" onclick={() => copyText(secret, 'secret')}>
        {copiedSecret ? i18n.t.common.copied : i18n.t.common.copy}
      </button>
    </div>
  </div>
  <form onsubmit={enable2fa}>
    <div class="form-group">
      <label class="form-label" for="enable-2fa-code">{i18n.t.account.enterCodeToEnable}</label>
      <input
        id="enable-2fa-code"
        type="text"
        class="form-control"
        bind:value={code}
        inputmode="numeric"
        autocomplete="one-time-code"
        placeholder="123456"
        required
      />
    </div>
    <div class="table-actions">
      <button type="submit" class="btn btn-primary" disabled={loading}>
        {loading ? i18n.t.common.loading : i18n.t.account.setup2fa}
      </button>
      {#if oncancel}
        <button type="button" class="btn" onclick={oncancel}>{i18n.t.common.cancel}</button>
      {/if}
    </div>
  </form>
{:else if step === 'codes'}
  <div class="alert alert-warning">
    <strong>{i18n.t.account.recoveryCodesTitle}</strong>
    <p>{i18n.t.account.recoveryCodesDesc}</p>
  </div>
  <div class="code-grid">
    {#each recoveryCodes as rc (rc)}
      <div class="code-chip">{rc}</div>
    {/each}
  </div>
  <div class="table-actions">
    <button
      type="button"
      class="btn"
      onclick={() => copyText(recoveryCodes.join('\n'), 'codes')}
    >
      {copiedCodes ? i18n.t.common.copied : i18n.t.common.copy}
    </button>
    <button type="button" class="btn" onclick={downloadCodes}>
      {i18n.t.common.download}
    </button>
    <button type="button" class="btn btn-primary" onclick={oncompleted}>
      {i18n.t.account.doneSavedCodes}
    </button>
  </div>
{/if}

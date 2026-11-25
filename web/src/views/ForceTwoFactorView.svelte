<script lang="ts">
  import TwoFactorSetupPanel from '../components/TwoFactorSetupPanel.svelte';
  import { auth } from '../lib/auth.svelte';
  import { i18n } from '../lib/i18n.svelte';
  import { router } from '../lib/router.svelte';

  async function handleCompleted() {
    await auth.refreshMe();
    router.navigate('/dashboard');
  }
</script>

<div class="auth-page">
  <div class="auth-card auth-card-wide">
    <div class="auth-header">
      <div class="topbar-left">
        <span class="brand-mark">S</span>
        <strong>{i18n.t.appName}</strong>
      </div>
      <div class="topbar-right">
        <button type="button" class="btn btn-sm" onclick={() => i18n.toggleLocale()}>
          {i18n.locale.toUpperCase()}
        </button>
        <button type="button" class="btn btn-sm" onclick={() => auth.logout()}>
          {i18n.t.common.logout}
        </button>
      </div>
    </div>

    <h1 class="page-title">{i18n.t.force2fa.title}</h1>
    <p class="page-subtitle">{i18n.t.force2fa.subtitle}</p>

    <TwoFactorSetupPanel oncompleted={handleCompleted} />
  </div>
</div>

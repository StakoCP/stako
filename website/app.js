// Stako Static Website — Interactive install.sh Simulator
(function () {
  const PRESETS = {
    basic: [
      'openssh-server', 'nftables', 'fail2ban', 'nginx',
      'php84', 'mariadb', 'certbot'
    ],
    standard: [
      'openssh-server', 'nftables', 'fail2ban', 'nginx',
      'php84', 'mariadb', 'certbot',
      'apache2', 'postgresql', 'nodejs', 'docker',
      'redis', 'mail', 'ftp', 'backup'
    ],
    full: [
      'openssh-server', 'nftables', 'fail2ban', 'nginx',
      'php84', 'mariadb', 'certbot',
      'apache2', 'postgresql', 'nodejs', 'docker',
      'redis', 'mail', 'ftp', 'backup',
      'multiphp', 'rabbitmq', 'clamav', 'dns'
    ]
  };

  const EXTRA_FLAG_MAP = {
    apache2: '--with-apache',
    postgresql: '--with-postgres',
    nodejs: '--with-nodejs',
    docker: '--with-docker',
    redis: '--with-redis',
    mail: '--with-mail',
    ftp: '--with-ftp',
    backup: '--with-backup',
    multiphp: '--with-multiphp',
    rabbitmq: '--with-rabbitmq',
    clamav: '--with-clamav',
    dns: '--with-dns'
  };

  const pkgCheckboxes = Array.from(document.querySelectorAll('input[data-pkg]'));
  const presetBtns = Array.from(document.querySelectorAll('button[data-preset]'));
  const cmdOutput = document.getElementById('install-cmd-output');
  const planOutput = document.getElementById('install-plan-output');
  const copyBtn = document.getElementById('copy-install-cmd');
  const copyStatus = document.getElementById('copy-status');
  const verInput = document.getElementById('opt-version');
  const optUpdate = document.getElementById('opt-update');
  const optSkipUpgrade = document.getElementById('opt-skip-upgrade');
  const optDryRun = document.getElementById('opt-dry-run');

  if (!cmdOutput || pkgCheckboxes.length === 0) return;

  let currentPreset = 'standard';

  function setActivePresetButton(name) {
    currentPreset = name;
    presetBtns.forEach((btn) => {
      btn.classList.toggle('active', btn.dataset.preset === name);
    });
  }

  function applyPreset(name) {
    setActivePresetButton(name);
    const list = PRESETS[name] || [];
    pkgCheckboxes.forEach((cb) => {
      if (cb.disabled) {
        cb.checked = true;
      } else {
        cb.checked = list.includes(cb.dataset.pkg);
      }
    });
    renderCommand();
  }

  function detectPreset(selected) {
    const set = new Set(selected);
    for (const pName of ['basic', 'standard', 'full']) {
      const target = PRESETS[pName];
      if (target.length === set.size && target.every((k) => set.has(k))) {
        return pName;
      }
    }
    return 'custom';
  }

  function renderCommand() {
    const selected = pkgCheckboxes.filter((cb) => cb.checked).map((cb) => cb.dataset.pkg);
    const detected = detectPreset(selected);
    setActivePresetButton(detected);

    const args = [];
    if (detected === 'custom') {
      args.push('--preset basic');
      for (const pkg of selected) {
        if (EXTRA_FLAG_MAP[pkg]) {
          args.push(EXTRA_FLAG_MAP[pkg]);
        }
      }
    } else {
      args.push(`--preset ${detected}`);
    }

    const ver = (verInput && verInput.value.trim()) || '';
    if (ver && ver !== 'latest') {
      args.push(`--version ${ver}`);
    }
    if (optUpdate && optUpdate.checked) args.push('--update');
    if (optSkipUpgrade && optSkipUpgrade.checked) args.push('--skip-upgrade');
    if (optDryRun && optDryRun.checked) args.push('--dry-run');

    const base = 'curl -fsSL https://raw.githubusercontent.com/StakoCP/stako/main/installer/install.sh | sudo bash -s --';
    cmdOutput.textContent = `${base} ${args.join(' ')}`;

    if (planOutput) {
      const labels = pkgCheckboxes
        .filter((cb) => cb.checked)
        .map((cb) => cb.dataset.label || cb.dataset.pkg);
      planOutput.textContent =
        `Profile : ${detected.toUpperCase()}\n` +
        `Flags   : ${args.join(' ')}\n` +
        `Packages (${labels.length}):\n  - ` +
        labels.join('\n  - ');
    }
  }

  presetBtns.forEach((btn) => {
    btn.addEventListener('click', () => {
      const p = btn.dataset.preset;
      if (p && PRESETS[p]) applyPreset(p);
    });
  });

  pkgCheckboxes.forEach((cb) => cb.addEventListener('change', renderCommand));
  if (verInput) verInput.addEventListener('input', renderCommand);
  if (optUpdate) optUpdate.addEventListener('change', renderCommand);
  if (optSkipUpgrade) optSkipUpgrade.addEventListener('change', renderCommand);
  if (optDryRun) optDryRun.addEventListener('change', renderCommand);

  if (copyBtn) {
    copyBtn.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(cmdOutput.textContent || '');
        if (copyStatus) {
          copyStatus.textContent = 'Copied!';
          setTimeout(() => { copyStatus.textContent = ''; }, 2500);
        }
      } catch {
        // fallback
      }
    });
  }

  applyPreset(currentPreset);
})();

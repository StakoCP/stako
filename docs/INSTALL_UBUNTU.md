# Panduan Instalasi Stako di VPS Ubuntu (22.04 LTS / 24.04 LTS)

Panduan lengkap langkah demi langkah untuk memasang **Stako Control Panel** di VPS Ubuntu Server yang bersih, mulai dari persiapan server hingga verifikasi akhir dan operasional harian.

---

## 1. Prasyarat Server

- **Sistem Operasi:** Ubuntu Server **22.04 LTS** (Jammy) atau **24.04 LTS** (Noble), arsitektur `amd64` (`x86_64`) atau `arm64`.
- **Hak Akses:** Akses `root` (atau user dengan `sudo`) melalui SSH.
- **Spesifikasi Minimum:**
  - Preset **Basic:** 1 vCPU, 1 GB RAM, 15 GB disk.
  - Preset **Standard (disarankan):** 2 vCPU, 2–4 GB RAM, 25 GB disk.
  - Preset **Full:** 2+ vCPU, 4+ GB RAM, 40 GB disk.
- **Domain & DNS (disarankan):** Arahkan record `A` (dan `AAAA` jika memakai IPv6), misalnya `panel.domainanda.com`, ke IP publik VPS sebelum menerbitkan SSL.
- **Port Jaringan:** Pastikan firewall provider cloud (AWS Security Group, GCP VPC, Alibaba, dll.) membuka port berikut sesuai layanan yang dipasang:
  - `22/tcp` (SSH)
  - `80/tcp` & `443/tcp` (HTTP / HTTPS)
  - `8083/tcp` (Web UI & REST API Stako)
  - Opsional jika memakai layanan terkait: `21/tcp` (FTPS), `25/tcp`, `465/tcp`, `587/tcp`, `993/tcp` (Mail), `53/tcp+udp` (jika `--with-dns` diaktifkan).

---

## 2. Persiapan Awal VPS (Login & Hostname)

1. Login ke VPS melalui SSH:
   ```bash
   ssh root@IP_VPS_ANDA
   ```

2. Atur hostname FQDN server dan zona waktu:
   ```bash
   sudo hostnamectl set-hostname panel.domainanda.com
   sudo timedatectl set-timezone Asia/Jakarta
   ```

3. Pastikan sistem mutakhir:
   ```bash
   sudo apt-get update && sudo apt-get upgrade -y
   ```

---

## 3. Metode Instalasi

### Opsi A — Instalasi Otomatis dari GitHub Releases (Rekomendasi)

Installer cerdas (`installer/install.sh`) akan:
1. Menjalankan `apt-get update` dan `apt-get upgrade`.
2. Menambahkan repositori resmi (Ondrej PHP untuk **PHP 8.4 / 8.5**, NodeSource 22.x, Docker CE).
3. Memeriksa setiap dependensi backend — memasang jika belum ada, atau menghapus versi lama yang tidak memenuhi syarat lalu memasang versi yang sesuai.
4. Mengunduh binary `stako-core`, `stako-agent`, dan `stako` sesuai arsitektur (`amd64`/`arm64`) beserta `SHA256SUMS` lalu memverifikasi checksum-nya.
5. Menerapkan hardening baseline OS (`sysctl`, SSH `PermitRootLogin prohibit-password`, `nftables`, `fail2ban`, `auditd`, `unattended-upgrades`).
6. Memasang unit `systemd` dan menjalankan `stako-core bootstrap-admin`.

**Langkah:**

```bash
# 1. Unduh skrip installer
curl -fsSLO https://raw.githubusercontent.com/StakoCP/stako/main/installer/install.sh

# 2. (Opsional) Simulasi terlebih dahulu tanpa mengubah sistem
sudo bash install.sh --dry-run --preset standard

# 3. Jalankan instalasi (preset: basic | standard | full)
sudo bash install.sh --preset standard
```

> **Catatan DNS:** Secara default, PowerDNS **tidak** dipasang (mengikuti desain cloud/Cloudflare DNS). Jika ingin memasang DNS server bawaan, tambahkan flag `--with-dns`:
> ```bash
> sudo bash install.sh --preset standard --with-dns
> ```

---

### Opsi B — Instalasi dari Binary Lokal (`dist/`)

Jika Anda sudah melakukan *cross-compile* dari mesin pengembangan (`dist/stako-*-linux-amd64` atau `arm64`):

1. Unggah folder `dist/` dan `installer/install.sh` ke VPS (dari mesin lokal):
   ```bash
   scp -r dist installer/install.sh root@IP_VPS_ANDA:/root/stako-pkg/
   ```

2. Di VPS, verifikasi checksum lalu jalankan installer dengan `--from-dir`:
   ```bash
   cd /root/stako-pkg
   sha256sum -c dist/SHA256SUMS --ignore-missing
   sudo bash install.sh --preset standard --from-dir ./dist
   ```

---

## 4. Ringkasan Paket yang Dipasang per Preset

| Preset | Layanan & Dependensi yang Dipasang |
| --- | --- |
| **Basic** | `nginx`, **PHP 8.4** (`php8.4-fpm`, `cli`, `mysql`, `curl`, `mbstring`, `xml`, `zip`), `mariadb-server`, `nftables`, `fail2ban`, `auditd`, `unattended-upgrades`, `certbot`, `openssh-server` |
| **Standard** *(default)* | Semua **Basic** + `apache2`, `postgresql`, `nodejs` (22.x), `golang-go`, `python3` (`venv`, `pip`), `docker-ce` + `docker-compose-plugin` + `uidmap` + `dbus-user-session` (**Docker rootless**), `redis-server`, `postfix` + `dovecot` + `rspamd`, `proftpd-basic` (TLS-only), `restic` |
| **Full** | Semua **Standard** + `php8.3-fpm`, `php8.5-fpm`, `rabbitmq-server`, `clamav`, `spamassassin`, `goaccess`, `libnginx-mod-http-modsecurity` |

---

## 5. Verifikasi Setelah Instalasi

1. **Periksa status layanan systemd:**
   ```bash
   sudo systemctl status stako-agent stako-core nginx --no-pager
   ```

2. **Jalankan diagnosa `stako doctor`:**
   ```bash
   stako --insecure doctor
   ```
   Untuk memeriksa koneksi `stako-agent` dan info sistem secara penuh, buat API key dari panel lalu jalankan:
   ```bash
   stako --insecure --api-key "stk_..." doctor
   ```

3. **Periksa log instalasi (jika perlu audit):**
   ```bash
   sudo less /var/log/stako-install.log
   ```

4. **Periksa berkas konfigurasi pilihan preset:**
   ```bash
   cat /etc/stako/install.yml
   ```

---

## 6. Login Pertama & Hardening Awal Panel

1. Buka browser ke alamat panel:
   ```text
   https://IP_VPS_ANDA:8083
   ```
   *(Pada login pertama sebelum domain SSL diterbitkan, browser akan menampilkan peringatan sertifikat self-signed — lanjutkan untuk masuk.)*

2. Login menggunakan username `admin` dan password yang ditampilkan di akhir proses `stako-core bootstrap-admin`.
   Jika Anda lupa atau ingin membuat admin baru dari CLI:
   ```bash
   sudo stako-core bootstrap-admin --username admin2 --email admin@domainanda.com
   ```

3. **Aktifkan 2FA TOTP (Wajib):**
   - Setelah login pertama sebagai `admin` atau `reseller`, panel akan mengarahkan Anda ke halaman setup **2FA TOTP**.
   - Pindai QR code dengan aplikasi authenticator (Google Authenticator, Aegis, Bitwarden, dll.), masukkan kode 6 digit, lalu **simpan 8 recovery code** di tempat aman.

4. **Pasang Domain Panel & SSL Let's Encrypt:**
   - Buka menu **Domains** → tambahkan `panel.domainanda.com`, lalu terbitkan SSL Let's Encrypt atau arahkan reverse proxy.

5. **Jalankan Pemindaian Keamanan Awal:**
   - Buka menu **Security & Updates** → klik **Run Security & CVE Scan** untuk memverifikasi skor keamanan server dan status update paket.

---

## 7. Operasional Harian & Pemecahan Masalah

| Tugas | Perintah |
| --- | --- |
| Cek kesehatan panel | `stako --insecure doctor` |
| Lihat log `stako-core` | `sudo journalctl -u stako-core -n 100 --no-pager` |
| Lihat log `stako-agent` | `sudo journalctl -u stako-agent -n 100 --no-pager` |
| Restart layanan panel | `sudo systemctl restart stako-agent stako-core` |
| Aktifkan mode lockdown darurat (lewat API/UI) | Menu **Security & Updates** → **Emergency Lockdown Mode** |
| Lokasi database state & master key | `/var/lib/stako/stako.db` & `/etc/stako/keys/master.key` *(backup berkala!)* |

> **Penting:** Segera simpan cadangan `/etc/stako/keys/master.key` di tempat aman di luar server. Tanpa kunci ini, arsip backup terenkripsi, secret environment aplikasi, dan secret 2FA tidak dapat didekripsi saat pemulihan bencana.


---

## 8. Memperbarui (Update) Stako Tanpa Menghapus Instalasi

Untuk memperbarui Stako ke rilis terbaru **tanpa menghapus database, master key, sertifikat, maupun situs yang sudah ada**:

### Melalui Dashboard Web
1. Masuk sebagai `admin` → buka menu **Keamanan & Pembaruan** (`#/security`).
2. Pada kartu **Pembaruan Panel Stako**, klik **Periksa Pembaruan** lalu **Perbarui Stako (In-Place)**.
3. Panel otomatis membuat backup database/konfigurasi terlebih dahulu, mengunduh binary terbaru yang terverifikasi `SHA256SUMS`, lalu me-restart `stako-agent` dan `stako-core`.

### Melalui CLI / Terminal VPS
```bash
# Menggunakan skrip installer mode --update
curl -fsSLO https://raw.githubusercontent.com/StakoCP/stako/main/installer/install.sh
sudo bash install.sh --update

# Atau dari folder dist/ lokal:
sudo bash install.sh --update --from-dir ./dist
```

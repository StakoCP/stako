export type UserRole = 'admin' | 'reseller' | 'user' | 'readonly';
export type UserStatus = 'active' | 'suspended';
export type UserShell = 'nologin' | 'sftp' | 'rbash' | 'bash';

export interface User {
  id: string;
  username: string;
  email: string;
  role: UserRole;
  status: UserStatus;
  package_id: string | null;
  owner_id: string | null;
  totp_enabled: boolean;
  shell: UserShell;
  created_at: string;
  updated_at: string;
  last_login_at: string | null;
}

export interface Package {
  id: string;
  name: string;
  owner_id: string | null;
  disk_mb: number;
  bandwidth_mb: number;
  domains: number;
  databases: number;
  mail_accounts: number;
  ftp_accounts: number;
  containers: number;
  cpu_percent: number;
  memory_mb: number;
  shell_access: boolean;
  php_versions: string[];
  created_at: string;
  updated_at: string;
}

export type PackageInput = Omit<Package, 'id' | 'owner_id' | 'created_at' | 'updated_at'>;

export type ApiKeyScope = 'read' | 'write';

export interface ApiKey {
  id: string;
  name: string;
  prefix: string;
  scopes: ApiKeyScope[];
  allowed_ips: string[];
  expires_at: string | null;
  last_used_at: string | null;
  created_at: string;
}

export interface Session {
  id: string;
  ip: string;
  user_agent: string;
  created_at: string;
  last_seen_at: string;
  current: boolean;
}

export interface AuditEntry {
  id: number;
  at: string;
  actor_id: string;
  actor_username: string;
  acting_as: string | null;
  action: string;
  object_type: string;
  object_id: string;
  result: 'success' | 'failure';
  ip: string;
  user_agent: string;
  detail: Record<string, unknown>;
  hash: string;
}

export interface AuditVerifyResult {
  valid: boolean;
  checked: number;
  broken_at: number | null;
}

export type JobStatus = 'queued' | 'running' | 'succeeded' | 'failed';

export interface Job {
  id: string;
  type: string;
  status: JobStatus;
  created_by: string;
  payload: Record<string, unknown>;
  log: string;
  error: string | null;
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
}

export interface SystemInfo {
  hostname: string;
  os: string;
  kernel: string | null;
  version: string;
  uptime_seconds: number;
  cpu_cores: number;
  load_avg: [number, number, number] | null;
  memory_total_bytes: number | null;
  memory_available_bytes: number | null;
  agent: {
    connected: boolean;
    dry_run: boolean;
    version: string;
  };
  counts: {
    users: number;
    packages: number;
    jobs_running: number;
  };
}

export interface PaginatedResponse<T> {
  items: T[];
  total: number;
}

export interface SystemCapabilities {
  web: boolean;
  dns: boolean;
  docker: boolean;
  databases: boolean;
  redis_rabbitmq: boolean;
  ftp: boolean;
  mail: boolean;
  firewall: boolean;
}

export interface AuthMeResponse {
  user: User;
  impersonator?: User | null;
  csrf_token: string;
  two_factor_required: boolean;
  capabilities?: SystemCapabilities;
}

export interface LoginResponse {
  user: User;
  csrf_token: string;
}

export interface ImpersonateResponse {
  user: User;
  impersonator: User | null;
  csrf_token: string;
}

export type IPFamily = 'ipv4' | 'ipv6';
export type IPScope = 'shared' | 'dedicated';
export type IPVisibility = 'public' | 'internal';

export interface IPAddress {
  id: string;
  ip: string;
  family: IPFamily;
  scope: IPScope;
  visibility: IPVisibility;
  interface_name: string;
  owner_id: string | null;
  created_at: string;
}

export type DomainStack = 'nginx' | 'nginx_apache' | 'apache';
export type DomainTemplate = 'php' | 'wordpress' | 'laravel' | 'static' | 'spa' | 'proxy' | 'redirect';
export type DomainSSLMode = 'none' | 'letsencrypt' | 'custom';
export type DomainStatus = 'active' | 'suspended';

export interface Domain {
  id: string;
  user_id: string;
  domain: string;
  aliases: string[];
  stack: DomainStack;
  template: DomainTemplate;
  docroot: string;
  php_version: string;
  ssl_enabled: boolean;
  ssl_mode: DomainSSLMode;
  force_https: boolean;
  hsts: boolean;
  ipv4_id: string | null;
  ipv6_id: string | null;
  proxy_target: string;
  status: DomainStatus;
  created_at: string;
  updated_at: string;
}

export interface ProxyRule {
  id: string;
  domain_id: string;
  path: string;
  upstream: string;
  websocket: boolean;
  sse: boolean;
  grpc: boolean;
  created_at: string;
}

export type AppRuntime = 'node' | 'go' | 'python';
export type AppStatus = 'stopped' | 'running' | 'failed';

export interface App {
  id: string;
  user_id: string;
  domain_id: string | null;
  name: string;
  runtime: AppRuntime;
  runtime_version: string;
  work_dir: string;
  command: string;
  port: number;
  env_keys: string[];
  status: AppStatus;
  unit_name: string;
  created_at: string;
  updated_at: string;
}

export interface ValidationFieldError {
  field: string;
  message: string;
}

export interface ProblemDetails {
  type: string;
  title: string;
  status: number;
  detail?: string;
  errors?: ValidationFieldError[];
}

export type DatabaseEngine = 'mariadb' | 'postgres';

export interface Database {
  id: string;
  user_id: string;
  name: string;
  engine: DatabaseEngine;
  db_user: string;
  remote_hosts: string[];
  size_mb: number;
  created_at: string;
}

export interface PgBouncerConfig {
  enabled: boolean;
  listen_port: number;
  pool_mode: 'transaction' | 'session' | 'statement';
  max_client_conn: number;
  default_pool_size: number;
  reserve_pool_size: number;
}

export interface PGReplicationConfig {
  role: 'disabled' | 'primary' | 'standby';
  wal_level: 'replica' | 'logical';
  max_wal_senders: number;
  wal_keep_size_mb: number;
  replication_user: string;
  replication_pass?: string;
  has_replication_password?: boolean;
  standby_hosts: string[];
  primary_host: string;
  primary_port: number;
}

export interface PGPITRConfig {
  enabled: boolean;
  archive_dir: string;
  retention_days: number;
}

export interface PGAdvancedResponse {
  pgbouncer: PgBouncerConfig;
  replication: PGReplicationConfig;
  pitr: PGPITRConfig;
  status: {
    pgbouncer_active: boolean;
    in_recovery: boolean;
    replication_peers: string[];
  };
}

export type FtpProtocol = 'ftps' | 'sftp';

export interface FtpAccount {
  id: string;
  user_id: string;
  username: string;
  home_dir: string;
  quota_mb: number;
  protocol: FtpProtocol;
  created_at: string;
}

export type FirewallAction = 'allow' | 'deny';
export type FirewallProtocol = 'tcp' | 'udp' | 'any';

export interface FirewallRule {
  id: string;
  action: FirewallAction;
  protocol: FirewallProtocol;
  port: string;
  source_cidr: string;
  comment: string;
  created_at: string;
}

export interface BannedIP {
  ip: string;
  jail: string;
  reason: string;
  banned_at: string;
}

export type SecurityCategory = 'cve' | 'hardening' | 'ssh' | 'tls';
export type SecuritySeverity = 'critical' | 'high' | 'medium' | 'low';
export type SecurityFindingStatus = 'open' | 'resolved' | 'accepted';

export interface SecurityFinding {
  id: string;
  category: SecurityCategory;
  severity: SecuritySeverity;
  title: string;
  description: string;
  remediation: string;
  status: SecurityFindingStatus;
  component: string;
  detected_at: string;
  updated_at: string;
}

export interface SecurityFindingsResponse {
  score: number;
  items: SecurityFinding[];
  total: number;
}

export type PackageUpdateCategory = 'security' | 'regular' | 'kernel';

export interface PackageUpdate {
  package: string;
  current: string;
  candidate: string;
  category: PackageUpdateCategory;
}

export interface UpdatesResponse {
  reboot_required: boolean;
  items: PackageUpdate[];
}

export type ContainerStatus = 'running' | 'stopped';

export interface Container {
  id: string;
  user_id: string;
  domain_id: string | null;
  name: string;
  image: string;
  repo_url?: string;
  repo_branch?: string;
  local_path?: string;
  dockerfile?: string;
  ports: string[];
  volumes: string[];
  cpu_limit: string;
  memory_mb: number;
  status: ContainerStatus;
  created_at: string;
}

export type ComposeStackStatus = 'running' | 'stopped';

export interface ComposeStack {
  id: string;
  user_id: string;
  name: string;
  compose_yaml: string;
  status: ComposeStackStatus;
  created_at: string;
}

export type CatalogAppType = 'php' | 'docker';

export interface CatalogApp {
  slug: string;
  name: string;
  type: CatalogAppType;
  category: string;
  version: string;
  description: string;
}

export type AppInstallStatus = 'installed' | 'installing' | 'failed';

export interface AppInstall {
  id: string;
  user_id: string;
  domain_id: string;
  app_slug: string;
  status: AppInstallStatus;
  created_at: string;
}

export interface RedisInstance {
  id: string;
  user_id: string;
  name: string;
  port: number;
  memory_mb: number;
  persistent: boolean;
  status: string;
  created_at: string;
}

export interface RabbitVhost {
  id: string;
  user_id: string;
  vhost: string;
  username: string;
  status: string;
  created_at: string;
}

export interface MailDomain {
  id: string;
  user_id: string;
  domain: string;
  dkim_selector: string;
  dkim_public_key: string;
  relay_provider?: string;
  relay_host?: string;
  relay_port?: number;
  relay_user?: string;
  created_at: string;
}

export interface ServerSMTPRelay {
  enabled: boolean;
  provider: string;
  host: string;
  port: number;
  username: string;
  tls_mode: string;
  has_password: boolean;
}

export interface MailDnsRecord {
  type: string;
  name: string;
  value: string;
}

export interface Mailbox {
  id: string;
  mail_domain_id: string;
  email: string;
  quota_mb: number;
  created_at: string;
}

export interface MailAlias {
  id: string;
  mail_domain_id: string;
  source: string;
  destination: string;
  is_catch_all: boolean;
  created_at: string;
}

export interface MailQueueItem {
  queue_id: string;
  queue_name: string;
  arrival_time: number | string;
  message_size: number;
  sender: string;
  recipients?: { address: string; delay_reason?: string }[];
}

export type BackupKind = 'user_data' | 'system_config';
export type BackupTarget = 'local' | 's3' | 'sftp';
export type BackupStatus = 'completed' | 'running' | 'failed';

export interface Backup {
  id: string;
  user_id: string | null;
  kind: BackupKind;
  target: BackupTarget;
  size_bytes: number;
  checksum: string;
  status: BackupStatus;
  created_at: string;
}

export interface Webhook {
  id: string;
  url: string;
  events: string[];
  created_at: string;
}

export interface LockdownStatus {
  enabled: boolean;
  allowed_ips: string[];
}

export type DNSProvider = 'powerdns' | 'cloudflare';

export interface DNSZone {
  id: string;
  user_id?: string;
  domain: string;
  provider: DNSProvider;
  created_at?: string;
  updated_at?: string;
}

export interface DNSRecord {
  id: string;
  zone_id?: string;
  type: string;
  name: string;
  content: string;
  ttl: number;
  priority?: number | null;
  created_at?: string;
}

export interface CronJob {
  id: string;
  user_id?: string;
  schedule: string;
  command: string;
  created_at: string;
}

export interface FileEntry {
  name: string;
  is_dir: boolean;
  size: number;
  mode: string;
  octal_mode?: string;
  mod_time: string;
}

export interface FileListResponse {
  path: string;
  items: FileEntry[];
}

export interface FileReadResponse {
  path: string;
  content: string;
}

export interface ServiceStatus {
  name: string;
  status: string;
}

export interface MonitoringMetrics {
  cpu_percent: number;
  memory_percent: number;
  disk_percent: number;
  services: ServiceStatus[];
  timestamp: string;
}

export type AlertChannel = 'email' | 'telegram' | 'slack' | 'webhook';

export interface AlertRule {
  id: string;
  name: string;
  channel: AlertChannel;
  target: string;
  threshold: number;
  created_at?: string;
}

export interface TerminalExecResponse {
  output: string;
  exit_code: number;
}

export type WafEngine = 'coraza' | 'modsecurity';
export type WafMode = 'detection' | 'blocking';

export interface WafConfig {
  enabled: boolean;
  engine: WafEngine;
  mode: WafMode;
  paranoia_level: number;
}

export interface GitDeploy {
  id: string;
  user_id: string;
  domain_id: string;
  repo_url: string;
  branch: string;
  build_cmd: string;
  last_deployed_at: string | null;
  created_at: string;
}

export interface GitPreview {
  id: string;
  git_deploy_id: string;
  user_id: string;
  domain_id: string;
  subdomain: string;
  branch: string;
  pr_number: number;
  image: string;
  container_name: string;
  host_port: number;
  container_port: number;
  preview_domain_id: string;
  status: string;
  last_deployed_at: string;
  created_at: string;
}

export type ServerNodeRole = 'worker' | 'db' | 'mail';

export interface ServerNode {
  id: string;
  name: string;
  host: string;
  port: number;
  role: ServerNodeRole;
  status: string;
  created_at: string;
}

export type ImportSourcePanel = 'auto' | 'stako' | 'hestiacp' | 'cpanel' | 'vestacp';

export interface MigrationBundle {
  name: string;
  path: string;
  size_bytes: number;
  created_at: string;
}

export interface MigrationInspectResult {
  source_panel: string;
  archive_path: string;
  username: string;
  domains: string[];
  databases: string[];
  mail_domains: string[];
  mailboxes: string[];
  cron_jobs: string[];
  size_bytes: number;
  notes: string[];
}

export interface ServiceConfigMeta {
  service: string;
  label: string;
  path: string;
  test_cmd: string;
}

export interface ServiceConfigRevision {
  id: string;
  service: string;
  content: string;
  comment: string;
  created_by: string;
  created_at: string;
}

export interface ServiceConfigDetail {
  service: string;
  label: string;
  path: string;
  content: string;
  history: ServiceConfigRevision[];
}

export interface SystemUpdateInfo {
  current_version: string;
  latest_version: string;
  update_available: boolean;
  release_url?: string | null;
}

export type SystemServiceAction = 'start' | 'stop' | 'restart' | 'enable' | 'disable';

export interface SystemService {
  id: string;
  label: string;
  category: string;
  unit: string;
  apt_pkg: string;
  config_service?: string | null;
  protected: boolean;
  installed: boolean;
  version: string;
  active: 'active' | 'inactive' | 'failed' | 'not-installed' | string;
  enabled: boolean;
}

export interface CentralLogEntry {
  timestamp: string;
  service: string;
  source: string;
  level: string;
  message: string;
}

export interface CentralLogsResponse {
  entries: CentralLogEntry[];
  sources: string[];
  total: number;
}

export interface LogRotationConfig {
  frequency: 'daily' | 'weekly';
  rotate: number;
  maxsize: string;
  compress: boolean;
}

export interface LogShippingConfig {
  syslog_enabled: boolean;
  syslog_host: string;
  syslog_port: number;
  syslog_protocol: 'udp' | 'tcp' | 'tls';
  syslog_format: 'rfc5424' | 'rfc3164';
  loki_enabled: boolean;
  loki_url: string;
  loki_tenant_id: string;
  loki_username: string;
  loki_password?: string;
  has_loki_password?: boolean;
  loki_labels: string;
  loki_tls_skip_verify: boolean;
}

export interface UptimeMonitor {
  id: string;
  user_id: string;
  domain: string;
  url: string;
  interval_sec: number;
  expected_status: number;
  status: string;
  last_http_status: number;
  last_latency_ms: number;
  last_error: string;
  ssl_expires_at: string;
  ssl_days_left: number;
  ssl_issuer: string;
  domain_expires_at: string;
  domain_days_left: number;
  total_checks: number;
  up_checks: number;
  public_status: boolean;
  last_checked_at: string;
  created_at: string;
}

export interface PublicStatusMonitor {
  id: string;
  domain: string;
  url: string;
  status: string;
  last_http_status: number;
  last_latency_ms: number;
  ssl_expires_at: string;
  ssl_days_left: number;
  domain_expires_at: string;
  domain_days_left: number;
  uptime_pct: number;
  last_checked_at: string;
}

export interface PublicStatusResponse {
  overall_status: string;
  checked_at: string;
  monitors: PublicStatusMonitor[];
}

export interface WPSecurityIssue {
  id: string;
  severity: string;
  title: string;
  fixed: boolean;
}

export interface WPSite {
  id: string;
  user_id: string;
  domain_id: string;
  domain: string;
  path: string;
  wp_version: string;
  php_version: string;
  admin_url: string;
  is_staging: boolean;
  parent_id: string;
  updates_core: number;
  updates_plugins: number;
  updates_themes: number;
  security_score: number;
  security_issues: string;
  last_scanned_at: string;
  created_at: string;
}

export interface DomainCacheConfig {
  domain_id: string;
  user_id: string;
  microcache_enabled: boolean;
  microcache_ttl_sec: number;
  redis_object_cache: boolean;
  redis_instance_id: string;
  redis_port: number;
  cf_zone_id: string;
  has_cf_token: boolean;
  cf_under_attack: boolean;
  cf_dev_mode: boolean;
  updated_at: string;
}

export interface SmartDisk {
  device: string;
  model: string;
  size: string;
  health_status: string;
  temperature_c: number;
  power_on_hours: number;
}

export interface InodeMount {
  filesystem: string;
  mount: string;
  inodes_total: number;
  inodes_used: number;
  inodes_free: number;
  inodes_pct: number;
}

export interface MemoryPressure {
  mem_total_mb: number;
  mem_used_mb: number;
  mem_pct: number;
  swap_total_mb: number;
  swap_used_mb: number;
  swap_pct: number;
  psi_mem_avg10: number;
  psi_io_avg10: number;
}

export interface HardwareHealthResponse {
  disks: SmartDisk[];
  inodes: InodeMount[];
  pressure: MemoryPressure;
}

export interface UserDiskQuota {
  user_id: string;
  username: string;
  project_id: number;
  disk_limit_mb: number;
  inode_limit: number;
  disk_used_mb: number;
  inode_used: number;
  updated_at: string;
}

export interface PluginHook {
  event: string;
  url: string;
  secret?: string;
}

export interface PluginPage {
  slug: string;
  title: string;
  url: string;
  roles?: string[];
}

export interface PluginPageNav {
  plugin_id: string;
  plugin_name: string;
  slug: string;
  title: string;
  url: string;
}

export interface PluginManifest {
  id: string;
  name: string;
  version: string;
  author: string;
  description: string;
  homepage: string;
  enabled: boolean;
  hooks: PluginHook[];
  pages: PluginPage[];
  apps: CatalogApp[];
  created_at: string;
  updated_at: string;
}

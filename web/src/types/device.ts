export interface Device {
  id: string;
  name: string;
  hostname: string;
  os: 'linux' | 'windows' | 'darwin';
  arch: string;
  agent_version: string;
  status: 'online' | 'offline' | 'alert';
  last_seen: string;
  public_ip: string;
  private_ip: string;
  device_key?: string;
  tags?: string[];
  /**
   * The tenant this unit belongs to (2.3-0), `null` when no tenant owns it.
   * The console never infers a tenant from anything else: this field is the
   * definition, and the client filter follows it.
   */
  client_id?: string | null;
  reboot_pending?: boolean;
  /** Reported by the agent: can remote input land here? Never assume granted. */
  input_capability?: 'granted' | 'denied' | 'unavailable' | 'unknown';
  /** Reported by the agent: is screen capture producing frames? */
  capture_state?: 'ok' | 'error' | 'unknown';
  created_at: string;
  updated_at: string;
}

export interface Metrics {
  cpu: number;
  ram: number;
  ram_used: number;
  ram_total: number;
  disk_usage: number;
  disk_used: number;
  disk_total: number;
  net_in: number;
  net_out: number;
  uptime: number;
  processes: number;
}

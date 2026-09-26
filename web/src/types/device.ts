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

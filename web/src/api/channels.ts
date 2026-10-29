import client from './client';

/**
 * Notification channels (2.4-6): the ways this installation tells people what
 * happened. A channel is the v2.3 webhook generalized — an HTTP endpoint, a Slack
 * or Teams post, or a mailbox — with the routing (which tenants, which units,
 * which severity floor) stated on the row, and the same delivery ledger behind
 * every kind.
 */
export type ChannelKind = 'webhook' | 'slack' | 'teams' | 'email';
export type ChannelSeverity = '' | 'info' | 'warning' | 'critical';

export interface ChannelConfig {
  /** Mail addresses an email channel delivers to. */
  to?: string[];
  /** Sender address override, when the installation allows one. */
  from?: string;
}

export interface Channel {
  id: string;
  name: string;
  kind: ChannelKind;
  url: string;
  events: string;
  headers: string;
  enabled: boolean;
  config: ChannelConfig | string;
  scope_clients: string;
  min_severity: ChannelSeverity;
  tags: string;
  last_error: string;
  last_delivered_at: string | null;
  created_at: string;
}

export interface ChannelDelivery {
  id: string;
  webhook_id: string;
  event: string;
  payload: string;
  status: 'pending' | 'in_flight' | 'delivered' | 'failed';
  status_code: number;
  response_body: string;
  attempts: number;
  next_retry_at: string | null;
  delivered_at: string | null;
  created_at: string;
}

export interface ChannelForm {
  name: string;
  kind: ChannelKind;
  url: string;
  events: string[];
  headers?: Record<string, string>;
  enabled?: boolean;
  config?: ChannelConfig;
  scope_clients?: string[];
  min_severity?: ChannelSeverity;
  tags?: string[];
}

/**
 * The installation's mail transport, as the console may show it: where mail goes
 * and under which sender. Credentials are never part of it.
 */
export interface MailSummary {
  configured: boolean;
  host?: string;
  port?: number;
  sender?: string;
}

/** The server shows a channel's signing secret exactly once, on creation. */
export interface CreatedChannel {
  channel: Channel;
  secret?: string;
}

function asArray<T>(raw: ChannelConfig | string | T[] | string | undefined): T[] {
  if (Array.isArray(raw)) return raw as T[];
  if (typeof raw !== 'string') return [];
  try {
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

export function parseChannelConfig(raw: ChannelConfig | string): ChannelConfig {
  if (typeof raw === 'string') {
    try {
      return JSON.parse(raw) as ChannelConfig;
    } catch {
      return {};
    }
  }
  return raw || {};
}

export function parseChannelEvents(events: string): string[] {
  return asArray<string>(events);
}

export async function listChannels(): Promise<{ channels: Channel[]; mail: MailSummary }> {
  const res = await client.get('/v2/channels');
  return {
    channels: res.data.webhooks || [],
    mail: (res.data.mail as MailSummary) || { configured: false },
  };
}

export async function createChannel(form: ChannelForm): Promise<CreatedChannel> {
  const res = await client.post('/v2/channels', form);
  return { channel: res.data.webhook as Channel, secret: res.data.secret as string | undefined };
}

export async function updateChannel(id: string, form: Partial<ChannelForm>): Promise<Channel> {
  const res = await client.put(`/v2/channels/${id}`, form);
  return res.data.webhook;
}

export async function deleteChannel(id: string): Promise<void> {
  await client.delete(`/v2/channels/${id}`);
}

/** Ask the channel to deliver a test notification through its own routing. */
export async function testChannel(id: string): Promise<void> {
  await client.post(`/v2/channels/${id}/test`);
}

export async function listChannelDeliveries(id: string): Promise<ChannelDelivery[]> {
  const res = await client.get(`/v2/channels/${id}/deliveries`);
  return res.data.deliveries || [];
}

export async function retryChannelDelivery(channelId: string, deliveryId: string): Promise<void> {
  await client.post(`/v2/channels/${channelId}/deliveries/${deliveryId}/retry`);
}

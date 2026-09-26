import client from './client';

export interface Webhook {
  id: string;
  name: string;
  url: string;
  events: string;
  headers: string;
  enabled: boolean;
  last_error: string;
  last_delivered_at: string | null;
  created_at: string;
}

export interface WebhookDelivery {
  id: string;
  webhook_id: string;
  event: string;
  payload: string;
  status: string;
  status_code: number;
  response_body: string;
  attempts: number;
  next_retry_at: string | null;
  delivered_at: string | null;
  created_at: string;
}

export async function createWebhook(webhook: {
  name: string;
  url: string;
  events: string[];
  headers?: Record<string, string>;
  enabled?: boolean;
}): Promise<Webhook> {
  const res = await client.post('/v2/webhooks', webhook);
  return res.data.webhook;
}

export async function listWebhooks(): Promise<Webhook[]> {
  const res = await client.get('/v2/webhooks');
  return res.data.webhooks || [];
}

export async function getWebhook(id: string): Promise<Webhook> {
  const res = await client.get(`/v2/webhooks/${id}`);
  return res.data.webhook;
}

export async function updateWebhook(
  id: string,
  webhook: Partial<{ name: string; url: string; events: string[]; headers: Record<string, string>; enabled: boolean }>
): Promise<Webhook> {
  const res = await client.put(`/v2/webhooks/${id}`, webhook);
  return res.data.webhook;
}

export async function deleteWebhook(id: string): Promise<void> {
  await client.delete(`/v2/webhooks/${id}`);
}

export async function testWebhook(id: string): Promise<void> {
  await client.post(`/v2/webhooks/${id}/test`);
}

export async function listWebhookDeliveries(id: string): Promise<WebhookDelivery[]> {
  const res = await client.get(`/v2/webhooks/${id}/deliveries`);
  return res.data.deliveries || [];
}

export async function retryWebhookDelivery(webhookId: string, deliveryId: string): Promise<void> {
  await client.post(`/v2/webhooks/${webhookId}/deliveries/${deliveryId}/retry`);
}

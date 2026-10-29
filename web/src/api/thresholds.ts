import client from './client';

/**
 * Threshold rules (2.4-5): the warning/critical levels a metric is measured
 * against, per scope. Most specific wins, per metric, independently —
 * device → tag → client → global.
 */
export type ThresholdScope = 'device' | 'tag' | 'client' | 'global';
export type ThresholdMetric = 'cpu' | 'ram' | 'disk';

export interface ThresholdRule {
  id: string;
  scope_type: ThresholdScope;
  scope_value: string;
  metric: ThresholdMetric;
  warning: number;
  critical: number;
  dedup_window_seconds: number;
  enabled: boolean;
  /** The name of what the rule points at — a unit, a tenant, a tag. */
  scope_name?: string;
  /** False when the rule reaches units outside the caller's tenants. */
  editable?: boolean;
  created_at: string;
  updated_at: string;
}

export interface ThresholdDefault {
  metric: ThresholdMetric;
  warning: number;
  critical: number;
}

export interface ThresholdChainEntry {
  id: string;
  scope_type: ThresholdScope;
  scope_value: string;
  warning: number;
  critical: number;
  rank: number;
  source?: string;
  /** The name of what the rule points at, when the server can name it. */
  scope_name?: string;
}

export interface ThresholdMetricPreview {
  metric: ThresholdMetric;
  candidates: ThresholdChainEntry[];
  applies: ThresholdChainEntry;
}

export interface ThresholdPreview {
  device: { id: string; name: string; tags: string[]; client_id: string | null };
  metrics: ThresholdMetricPreview[];
}

export interface ThresholdSimulation {
  device_id: string;
  metric: ThresholdMetric;
  value: number;
  warning: number;
  critical: number;
  scope_type: ThresholdScope;
  scope_value: string;
  /** The unit's own name for a device rule, the tag for a tag rule. */
  scope_name?: string;
  source: string;
  would_fire: boolean;
  severity: string;
  threshold: number;
  message: string;
}

export interface ThresholdForm {
  scope_type: ThresholdScope;
  scope_value: string;
  metric: ThresholdMetric;
  warning: number;
  critical: number;
  dedup_window_seconds?: number;
  enabled?: boolean;
}

export const getThresholds = () =>
  client.get<{ rules: ThresholdRule[]; defaults: ThresholdDefault[] }>('/alerts/thresholds').then(
    (r) => ({ rules: r.data.rules ?? [], defaults: r.data.defaults ?? [] })
  );

export const createThreshold = (form: ThresholdForm) =>
  client.post<{ rule: ThresholdRule }>('/alerts/thresholds', form).then((r) => r.data.rule);

export const updateThreshold = (id: string, form: Partial<ThresholdForm>) =>
  client.put<{ rule: ThresholdRule }>(`/alerts/thresholds/${id}`, form).then((r) => r.data.rule);

export const deleteThreshold = (id: string) => client.delete(`/alerts/thresholds/${id}`);

/** "What applies to this unit, and in what order?" */
export const previewThresholds = (deviceId: string) =>
  client.get<ThresholdPreview>('/alerts/thresholds/resolve', { params: { device: deviceId } });

/** "What would this value do to this unit?" */
export const simulateThreshold = (deviceId: string, metric: ThresholdMetric, value: number) =>
  client.get<ThresholdSimulation>('/alerts/thresholds/simulate', {
    params: { device: deviceId, metric, value },
  });

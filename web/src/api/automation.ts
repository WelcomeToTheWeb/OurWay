import client from './client';

export interface Runbook {
  id: string;
  name: string;
  scope: string;
  scope_value: string;
  schedule: string;
  command: string;
  enabled: boolean;
  window_start: string;
  window_hours: number;
  timezone: string;
  last_run_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface RunbookRun {
  id: string;
  runbook_id: string;
  device_id: string;
  status: string;
  output: string;
  exit_code: number | null;
  started_at: string;
  finished_at: string | null;
  created_at: string;
}

export async function listRunbooks(): Promise<Runbook[]> {
  const res = await client.get('/v2/automation/runbooks');
  return res.data.runbooks || [];
}

export async function createRunbook(runbook: {
  name: string;
  scope: string;
  scope_value?: string;
  schedule: string;
  command: string;
  enabled?: boolean;
  window_start?: string;
  window_hours?: number;
  timezone?: string;
}): Promise<Runbook> {
  const res = await client.post('/v2/automation/runbooks', runbook);
  return res.data.runbook;
}

export async function updateRunbook(
  id: string,
  runbook: Partial<{
    name: string;
    scope: string;
    scope_value: string;
    schedule: string;
    command: string;
    enabled: boolean;
    window_start: string;
    window_hours: number;
    timezone: string;
  }>
): Promise<Runbook> {
  const res = await client.put(`/v2/automation/runbooks/${id}`, runbook);
  return res.data.runbook;
}

export async function deleteRunbook(id: string): Promise<void> {
  await client.delete(`/v2/automation/runbooks/${id}`);
}

export async function runRunbookNow(id: string): Promise<void> {
  await client.post(`/v2/automation/runbooks/${id}/run`);
}

export async function listRunbookRuns(id: string): Promise<RunbookRun[]> {
  const res = await client.get(`/v2/automation/runbooks/${id}/runs`);
  return res.data.runs || [];
}

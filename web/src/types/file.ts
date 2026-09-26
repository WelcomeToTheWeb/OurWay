export interface FileTransfer {
  id: string;
  device_id: string;
  filename: string;
  directory?: string;
  source_path?: string;
  destination?: string;
  size_bytes: number;
  status: 'pending' | 'transferring' | 'completed' | 'failed';
  direction: 'push' | 'pull';
  progress: number;
  error_message?: string;
  created_at: string;
  completed_at?: string;
}

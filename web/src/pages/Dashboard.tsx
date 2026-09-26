import { Server, CheckCircle, XCircle, Bell, Activity } from 'lucide-react';
import { useDevices } from '../hooks/useDevices';
import { DeviceCard } from '../components/DeviceCard';
import { useWebSocket } from '../hooks/useWebSocket';
import { useAuth } from '../auth/context';

export function Dashboard() {
  const { devices, loading, error } = useDevices();
  const { accessToken } = useAuth();
  const { connected } = useWebSocket(accessToken);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Activity className="h-8 w-8 animate-spin text-accent" />
      </div>
    );
  }

  if (error) {
    return <p className="text-status-error">Error: {error}</p>;
  }

  const online = devices.filter((d) => d.status === 'online').length;
  const offline = devices.filter((d) => d.status === 'offline').length;
  const alerts = devices.filter((d) => d.status === 'alert').length;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary">Dashboard</h1>
          <p className="text-sm text-text-secondary">Overview of your monitored devices</p>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={`h-2 w-2 rounded-full ${connected ? 'bg-status-online animate-pulse' : 'bg-status-offline'}`}
          />
          <span className="text-xs text-text-secondary">
            {connected ? 'Live' : 'Offline'}
          </span>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard icon={Server} label="Total Devices" value={devices.length} color="text-accent" />
        <StatCard icon={CheckCircle} label="Online" value={online} color="text-status-online" />
        <StatCard icon={XCircle} label="Offline" value={offline} color="text-status-offline" />
        <StatCard icon={Bell} label="Alerts" value={alerts} color="text-status-error" />
      </div>

      <div>
        <h2 className="text-sm font-medium text-text-muted uppercase tracking-wide mb-4">
          Devices
        </h2>
        {devices.length === 0 ? (
          <div className="flex flex-col items-center justify-center rounded-xl border border-bg-border bg-bg-card py-16 text-text-secondary">
            <Server className="h-10 w-10 text-text-muted" />
            <p className="mt-3 text-sm">No devices yet</p>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {devices.map((device) => (
              <DeviceCard key={device.id} device={device} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function StatCard({
  icon: Icon,
  label,
  value,
  color,
}: {
  icon: typeof Server;
  label: string;
  value: number;
  color: string;
}) {
  return (
    <div className="flex items-center gap-4 rounded-xl border border-bg-border bg-bg-card p-4">
      <div className={`flex h-10 w-10 items-center justify-center rounded-lg bg-bg ${color}`}>
        <Icon className="h-5 w-5" />
      </div>
      <div>
        <p className="text-2xl font-semibold text-text-primary">{value}</p>
        <p className="text-xs text-text-secondary">{label}</p>
      </div>
    </div>
  );
}

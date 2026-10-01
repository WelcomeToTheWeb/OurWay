import { useState } from 'react';
import { Search, Server, RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useDevices } from '../hooks/useDevices';
import { useWebSocket } from '../hooks/useWebSocket';
import { useAuth } from '../auth/context';
import { DeviceCard } from '../components/DeviceCard';
import { InstallerPanel } from '../components/InstallerPanel';
import { Grid } from 'react-window';

export function Devices() {
  const { t } = useTranslation();
  const { devices, loading, error, refresh } = useDevices();
  const { accessToken } = useAuth();
  // Realtime presence: WS status/heartbeat frames update the shared device
  // store, so the grid reflects online/offline changes without a refresh.
  useWebSocket(accessToken);
  const [search, setSearch] = useState('');

  const filtered = devices.filter((d) =>
    d.name.toLowerCase().includes(search.toLowerCase()) ||
    d.hostname.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary">{t('devices.title')}</h1>
          <p className="text-sm text-text-secondary">{t('devices.count', { count: devices.length })}</p>
        </div>
        <button
          onClick={refresh}
          className="flex items-center gap-2 rounded-lg bg-bg-secondary px-3 py-2 text-sm text-text-primary transition-colors hover:bg-bg"
        >
          <RefreshCw className="h-4 w-4" />
          {t('common.refresh')}
        </button>
      </div>

      <InstallerPanel />

      <div className="relative">
        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-text-secondary" />
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('devices.searchDevices')}
          className="w-full rounded-lg border border-bg-border bg-bg py-2 pl-9 pr-3 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
        />
      </div>

      {loading ? (
        <div className="flex h-64 items-center justify-center">
          <Server className="h-8 w-8 animate-spin text-accent" />
        </div>
      ) : error ? (
        <p className="text-status-error">{error}</p>
      ) : filtered.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-xl border border-bg-border bg-bg-card py-16 text-text-secondary">
          <Server className="h-10 w-10 text-text-muted" />
          <p className="mt-3 text-sm">{search ? t('devices.noMatch') : t('devices.none')}</p>
        </div>
      ) : filtered.length > 20 ? (
        <div className="rounded-xl border border-bg-border bg-bg-card">
          <Grid
            className="overflow-y-auto"
            style={{ height: 600, width: '100%' }}
            rowHeight={120}
            columnWidth={280}
            rowCount={Math.ceil(filtered.length / 4)}
            columnCount={4}
            overscanCount={10}
            cellProps={{}}
            cellComponent={(props) => {
              const index = props.rowIndex * 4 + props.columnIndex;
              const device = filtered[index];
              if (!device) return null;
              return (
                <div style={{ ...props.style, padding: '8px' }}>
                  <DeviceCard device={device} />
                </div>
              );
            }}
          />
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {filtered.map((device) => (
            <DeviceCard key={device.id} device={device} />
          ))}
        </div>
      )}
    </div>
  );
}

import { useSearchParams } from 'react-router-dom';
import { FleetPatches } from '../components/FleetPatches';
import { Patches } from './Patches';

const tabs = [
  { id: 'fleet', label: 'Fleet' },
  { id: 'device', label: 'By device' },
] as const;

/** Patch management: fleet-wide overview first, per-device detail second. */
export function PatchHub() {
  const [params, setParams] = useSearchParams();
  const tab = params.get('tab') === 'device' ? 'device' : 'fleet';

  return (
    <div className="space-y-6">
      {tab === 'fleet' && (
        <div>
          <h1 className="text-2xl font-semibold text-text-primary">Patch management</h1>
          <p className="text-sm text-text-secondary">Compliance and approvals across every device.</p>
        </div>
      )}
      <div role="tablist" aria-label="Patch views" className="flex gap-1 border-b border-bg-border">
        {tabs.map((t) => (
          <button
            key={t.id}
            role="tab"
            aria-selected={tab === t.id}
            onClick={() => setParams(t.id === 'fleet' ? {} : { tab: t.id })}
            className={`-mb-px border-b-2 px-4 py-2 text-sm font-medium transition-colors ${
              tab === t.id
                ? 'border-accent text-text-primary'
                : 'border-transparent text-text-secondary hover:text-text-primary'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>
      {tab === 'fleet' ? <FleetPatches /> : <Patches />}
    </div>
  );
}

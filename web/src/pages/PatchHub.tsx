import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { FleetPatches } from '../components/FleetPatches';
import { Patches } from './Patches';

const tabs = [
  { id: 'fleet', key: 'sheet.fleetSheet' },
  { id: 'device', key: 'sheet.byUnit' },
] as const;

/**
 * Patch management: the fleet sheet first (what is outstanding across every
 * client's units), the per-unit detail second.
 */
export function PatchHub() {
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();
  const tab = params.get('tab') === 'device' ? 'device' : 'fleet';

  return (
    <div className="space-y-4">
      {tab === 'fleet' && (
        <div>
          <h1 className="text-[26px]">{t('sheet.patchManagement')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">Compliance and approvals across every unit.</p>
        </div>
      )}
      <div role="tablist" aria-label="Patch views" className="flex border-b border-rule">
        {tabs.map((tabItem) => (
          <button
            key={tabItem.id}
            role="tab"
            aria-selected={tab === tabItem.id}
            onClick={() => setParams(tabItem.id === 'fleet' ? {} : { tab: tabItem.id })}
            className="-mb-px border-b-2 px-4 py-2 text-[13px] font-semibold transition-colors duration-400 ease-[var(--ease-press)]"
            style={{
              borderColor: tab === tabItem.id ? 'var(--service)' : 'transparent',
              color: tab === tabItem.id ? 'var(--text-primary)' : 'var(--text-secondary)',
            }}
          >
            {t(tabItem.key)}
          </button>
        ))}
      </div>
      {tab === 'fleet' ? <FleetPatches /> : <Patches />}
    </div>
  );
}

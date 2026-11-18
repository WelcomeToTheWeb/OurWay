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
          <h1 className="ow-display text-[22px]">{t('sheet.patchManagement')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">Compliance and approvals across every unit.</p>
        </div>
      )}
      {/* A tablist owes arrow-key travel and a panel that names its tab back;
          the URL still owns which view is open, so a deep link behaves. */}
      <div
        role="tablist"
        aria-label="Patch views"
        className="flex border-b border-rule"
        onKeyDown={(e) => {
          const i = tabs.findIndex((x) => x.id === tab);
          const next = e.key === 'ArrowRight' ? i + 1 : e.key === 'ArrowLeft' ? i - 1 : e.key === 'Home' ? 0 : e.key === 'End' ? tabs.length - 1 : -1;
          if (next < 0 || next >= tabs.length) return;
          e.preventDefault();
          const id = tabs[next].id;
          setParams(id === 'fleet' ? {} : { tab: id });
          document.getElementById(`patch-tab-${id}`)?.focus();
        }}
      >
        {tabs.map((tabItem) => (
          <button
            key={tabItem.id}
            id={`patch-tab-${tabItem.id}`}
            role="tab"
            aria-selected={tab === tabItem.id}
            aria-controls={`patch-panel-${tabItem.id}`}
            tabIndex={tab === tabItem.id ? 0 : -1}
            onClick={() => setParams(tabItem.id === 'fleet' ? {} : { tab: tabItem.id })}
            className="-mb-px border-b-2 px-4 py-2 text-[13px] font-semibold transition-colors duration-150 ease-press"
            style={{
              borderColor: tab === tabItem.id ? 'var(--service)' : 'transparent',
              color: tab === tabItem.id ? 'var(--text-primary)' : 'var(--text-secondary)',
            }}
          >
            {t(tabItem.key)}
          </button>
        ))}
      </div>
      <div
        role="tabpanel"
        id={`patch-panel-${tab}`}
        aria-labelledby={`patch-tab-${tab}`}
        tabIndex={0}
        className="outline-none focus-visible:ring-2 focus-visible:ring-[var(--focus)]"
      >
        {tab === 'fleet' ? <FleetPatches /> : <Patches />}
      </div>
    </div>
  );
}

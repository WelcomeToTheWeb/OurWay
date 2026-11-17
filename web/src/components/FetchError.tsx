import { useTranslation } from 'react-i18next';
import { AlertTriangle, RefreshCw } from 'lucide-react';

/**
 * A failed load, said out loud.
 *
 * Pages used to print the transport's own sentence — "Request failed with
 * status code 500" — as the whole of their failure state, which left an
 * operator guessing whether to retry, wait, or phone whoever owns the server,
 * and offered none of the three. The raw detail stays visible because it is
 * the line they read aloud to that person; the console states the failure
 * first and puts the retry under their hand.
 */
export function FetchError({ detail, onRetry }: { detail?: string; onRetry?: () => void }) {
  const { t } = useTranslation();
  return (
    <div
      role="alert"
      className="panel flex flex-wrap items-center gap-x-3 gap-y-2 border-[var(--error)] px-4 py-3"
    >
      <AlertTriangle className="h-4 w-4 shrink-0 text-[var(--error)]" aria-hidden="true" />
      <p className="text-[13px] font-semibold text-[var(--error)]">{t('common.loadFailed')}</p>
      {detail && (
        <p className="min-w-0 flex-1 truncate text-[12px] text-text-secondary" title={detail}>
          {detail}
        </p>
      )}
      {onRetry && (
        <button type="button" onClick={onRetry} className="btn btn-quiet ml-auto">
          <RefreshCw className="h-3.5 w-3.5" aria-hidden="true" />
          {t('common.retry')}
        </button>
      )}
    </div>
  );
}

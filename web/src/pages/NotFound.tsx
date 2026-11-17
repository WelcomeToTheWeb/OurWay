import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Compass } from 'lucide-react';

/**
 * The address the console does not have.
 *
 * Until now an unknown path rendered the shell around an empty `<main>`: rail,
 * shift bar, search box, and nothing to say. A technician following a stale
 * bookmark or a mistyped shortcut got a blank that looked like a crashed
 * report, which is the one thing a monitoring console must never imitate.
 */
export function NotFound() {
  const { t } = useTranslation();
  return (
    <div className="panel flex flex-col items-center gap-3 px-6 py-16 text-center">
      <Compass className="h-8 w-8 text-text-muted" aria-hidden="true" />
      <h1 className="ow-display text-[22px]">{t('common.notFoundTitle')}</h1>
      <p className="max-w-[46ch] text-[13px] text-text-secondary">{t('common.notFoundBody')}</p>
      <Link to="/" className="btn btn-quiet mt-1">
        {t('common.backHome')}
      </Link>
    </div>
  );
}

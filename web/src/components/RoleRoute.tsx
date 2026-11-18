import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { ShieldOff } from 'lucide-react';
import { useAuth } from '../auth/context';

/**
 * A role gate that speaks.
 *
 * This used to bounce the operator to the dashboard without a word, so the
 * experience of lacking a permission was a bookmark that appeared to be broken
 * — and the address they asked for vanished from the bar, which is the one
 * thing they could have handed to an administrator. The missing roles are named
 * here in their server spelling, because that is the name the grant uses.
 */
export function RoleRoute({
  roles,
  children,
}: {
  roles: string[];
  children: React.ReactNode;
}) {
  const { hasAnyRole } = useAuth();
  const { t } = useTranslation();
  if (!hasAnyRole(roles)) {
    return (
      <div role="alert" className="panel flex flex-col items-center gap-3 px-6 py-16 text-center">
        <ShieldOff className="h-8 w-8 text-text-muted" aria-hidden="true" />
        <h1 className="ow-display text-[22px]">{t('common.deniedTitle')}</h1>
        <p className="max-w-[52ch] text-[13px] text-text-secondary">
          {t('common.deniedBody', { roles: roles.join(', ') })}
        </p>
        <Link to="/" className="btn btn-quiet mt-1">
          {t('common.backHome')}
        </Link>
      </div>
    );
  }
  return <>{children}</>;
}

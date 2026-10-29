import type { TFunction } from 'i18next';
import type { ConfirmSheetRequest } from '../components/ConfirmSheet';

/**
 * Every destructive act in the console confirms through `ConfirmSheet`
 * (DESIGN.md §6 bans browser dialogs; `buglist.md` M19 tracked the ten call
 * sites that still used them).
 *
 * The copy lives in `en`/`de`/`fr` under `confirm.<action>.*` with the target
 * interpolated — not concatenated in English, which is what made the native
 * dialog unlocalisable. `scripts/i18n-parity.py` is what keeps the three
 * locales in lockstep, and a key that does not resolve renders its own key
 * path on screen: the tests below assert words, never a key path.
 */
export type ConfirmAction =
  | 'endSession'
  | 'deleteDevice'
  | 'deleteUser'
  | 'deleteWebhook'
  | 'deleteSsoBinding'
  | 'deletePolicy'
  | 'deleteClient'
  | 'purgeMetrics'
  | 'deleteAccount'
  | 'deleteApiKey'
  | 'revokeApiKey'
  // 2.3-4c: revoking a tenant's enrollment credential. It destroys nothing, and
  // it is exactly the kind of act the sheet exists for — the consequence is "an
  // agent will be refused", which an operator must read before pressing.
  | 'revokeToken'
  // 2.4-3: the two decisions about a pending SSO identity. Neither destroys
  // anything, and both grant an identity that did not exist a minute ago — exactly
  // the act an operator should read the consequence of before pressing.
  | 'approveIdentity'
  | 'mergeIdentity';

export function confirmRequest(
  t: TFunction,
  action: ConfirmAction,
  vars: Record<string, string> = {}
): ConfirmSheetRequest {
  return {
    title: t(`confirm.${action}.title`, vars),
    body: t(`confirm.${action}.body`, vars),
    confirmLabel: t(`confirm.${action}.action`),
  };
}

/**
 * A page's pending confirmation: the request to show and the act to run once
 * the operator presses the verb. Keeping them in one value means a sheet can
 * only ever confirm the thing that opened it.
 */
export type PendingConfirm = {
  request: ConfirmSheetRequest;
  run: () => void;
};

export function pendingConfirm(
  t: TFunction,
  action: ConfirmAction,
  run: () => void,
  vars: Record<string, string> = {}
): PendingConfirm {
  return { request: confirmRequest(t, action, vars), run };
}

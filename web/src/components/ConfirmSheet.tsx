import { useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';

/**
 * Confirmation is a state of the control, not a browser dialog (DESIGN.md §6).
 *
 * `window.confirm()` is banned: it is unthemeable, it cannot be localised (the
 * copy is a string concatenation written in English, so a German operator is
 * asked to destroy data in English), and automation dismisses it — which is why
 * the e2e suite had to accept a dialog to end a session at all.
 *
 * A sheet names the target in the title, states the consequence in the body,
 * and puts the verb — not "OK" — on the confirm button. Nothing is written
 * until the operator presses that button.
 */

export type ConfirmSheetRequest = {
  /** Names the target: `Delete unit "web-01"?` */
  title: string;
  /** What is destroyed and what survives. */
  body: string;
  /** The verb of the act: `Delete unit`, never `OK`. */
  confirmLabel: string;
};

type Props = {
  /** null renders nothing: the sheet is a state of the page, not a route. */
  request: ConfirmSheetRequest | null;
  /** The act is in flight: both controls rest, the sheet stays up. */
  busy?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
};

export function ConfirmSheet({ request, busy = false, onCancel, onConfirm }: Props) {
  const { t } = useTranslation();
  const panelRef = useRef<HTMLDivElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const restoreRef = useRef<Element | null>(null);
  const open = request !== null;

  // Focus discipline: in on open (onto Cancel — the sheet must never open with
  // the destructive control armed), trapped while open, back to the control
  // that asked on close.
  useEffect(() => {
    if (!open) return;
    restoreRef.current = document.activeElement;
    cancelRef.current?.focus();

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        onCancel();
        return;
      }
      if (event.key !== 'Tab' || !panelRef.current) return;
      const focusable = Array.from(
        panelRef.current.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')
      );
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const atStart = document.activeElement === first;
      const atEnd = document.activeElement === last;
      if (event.shiftKey && atStart) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && atEnd) {
        event.preventDefault();
        first.focus();
      }
    };

    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [open, onCancel]);

  useEffect(() => {
    if (open) return;
    const target = restoreRef.current;
    restoreRef.current = null;
    if (target instanceof HTMLElement) target.focus();
  }, [open]);

  if (!request) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-[var(--scrim)] p-4"
      onClick={() => !busy && onCancel()}
    >
      <div
        ref={panelRef}
        className="panel ow-rise w-full max-w-md p-4"
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-sheet-title"
        aria-describedby="confirm-sheet-body"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 id="confirm-sheet-title" className="panel-head mb-3 text-[var(--stamp)]">
          {request.title}
        </h2>
        <p id="confirm-sheet-body" className="mb-5 text-[13px] leading-relaxed text-text-primary">
          {request.body}
        </p>
        <div className="flex gap-3">
          <button
            ref={cancelRef}
            type="button"
            className="btn flex-1"
            disabled={busy}
            onClick={onCancel}
          >
            {t('confirm.cancel')}
          </button>
          <button
            type="button"
            className="btn flex-1 bg-[var(--stamp)] text-[var(--stamp-ink)] hover:opacity-90 disabled:opacity-50"
            disabled={busy}
            onClick={onConfirm}
          >
            {request.confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

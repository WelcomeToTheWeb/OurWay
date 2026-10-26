import { useEffect, useRef, useState } from 'react';

/**
 * Guide words — the lexicon discipline. A long list names, at its head, the
 * first and last entry currently in view, recomputed as the operator scrolls.
 * In a thousand-unit fleet that is the difference between "a list" and
 * "a list you know your place in".
 */
export function useGuideWords(count: number) {
  const ref = useRef<HTMLDivElement | null>(null);
  const [range, setRange] = useState<[number, number]>([0, Math.min(count - 1, 4)]);

  useEffect(() => {
    const el = ref.current;
    if (!el || count === 0) return;

    function measure() {
      const rows = Array.from(el!.querySelectorAll<HTMLElement>('[data-guide]'));
      if (rows.length === 0) return;
      const top = el!.scrollTop;
      const bottom = top + el!.clientHeight;
      let first = 0;
      let last = rows.length - 1;
      rows.forEach((r, i) => {
        if (r.offsetTop + r.offsetHeight <= top + 1) first = i + 1;
      });
      rows.forEach((r, i) => {
        if (r.offsetTop >= bottom - 1) {
          last = Math.min(last, i - 1);
        }
      });
      first = Math.min(first, rows.length - 1);
      last = Math.max(first, last);
      setRange((cur) => (cur[0] === first && cur[1] === last ? cur : [first, last]));
    }

    measure();
    // Fonts and rows settle after first paint; measure once more then, so
    // the head never prints a stale range on a freshly opened sheet.
    const raf = requestAnimationFrame(measure);
    el.addEventListener('scroll', measure, { passive: true });
    let ro: ResizeObserver | undefined;
    if (typeof ResizeObserver !== 'undefined') {
      ro = new ResizeObserver(measure);
      ro.observe(el);
    }
    const t = setTimeout(measure, 60);
    const t2 = setTimeout(measure, 400);
    return () => {
      cancelAnimationFrame(raf);
      el.removeEventListener('scroll', measure);
      ro?.disconnect();
      clearTimeout(t);
      clearTimeout(t2);
    };
  }, [count]);

  return { ref, first: range[0], last: range[1] };
}

/** The printed head: GUIDE WORDS · span · count. */
export function GuideWordsHead({
  first,
  last,
  total,
  unit = 'units',
  extra,
}: {
  first?: string;
  last?: string;
  total: number;
  unit?: string;
  extra?: React.ReactNode;
}) {
  return (
    <div className="flex items-baseline justify-between gap-4 border-b border-rule-strong bg-bg-card px-3 py-1.5">
      <span className="flex items-baseline gap-2 overflow-hidden">
        <span className="cap-sm cap shrink-0 text-text-muted">In view</span>
        <span className="cap cap-sm truncate text-text-primary">{first ?? '—'}</span>
        <span className="text-[11px] text-text-muted">→</span>
        <span className="cap cap-sm truncate text-text-primary">{last ?? '—'}</span>
      </span>
      <span className="flex items-baseline gap-3">
        {extra}
        <span className="ow-num text-[11px] text-text-secondary">
          {total} {unit}
        </span>
      </span>
    </div>
  );
}

import { useState } from 'react';
import { Tag, X, Plus } from 'lucide-react';
import { setDeviceTags } from '../api/tags';

/** Chip list with inline add/remove for one device's tags. */
export function TagEditor({
  deviceId,
  tags,
  onChange,
  readOnly = false,
}: {
  deviceId: string;
  tags: string[];
  onChange: (tags: string[]) => void;
  readOnly?: boolean;
}) {
  const [draft, setDraft] = useState('');
  const [error, setError] = useState<string | null>(null);

  async function save(next: string[]) {
    try {
      setError(null);
      const saved = await setDeviceTags(deviceId, next);
      onChange(saved);
    } catch {
      setError('Could not save tags');
    }
  }

  function add() {
    const t = draft.trim().toLowerCase();
    setDraft('');
    if (!t || tags.includes(t)) return;
    void save([...tags, t]);
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5" aria-label="Device tags">
      <Tag className="h-3.5 w-3.5 text-text-muted" aria-hidden="true" />
      {tags.length === 0 && readOnly && <span className="text-xs text-text-muted">No tags</span>}
      {tags.map((t) => (
        <span
          key={t}
          className="inline-flex items-center gap-1 rounded-full border border-bg-border bg-bg px-2 py-0.5 text-xs text-text-secondary"
        >
          {t}
          {!readOnly && (
            <button
              onClick={() => void save(tags.filter((x) => x !== t))}
              className="text-text-muted hover:text-status-error"
              aria-label={`Remove tag ${t}`}
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </span>
      ))}
      {!readOnly && (
        <span className="inline-flex items-center gap-1">
          <input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                add();
              }
            }}
            placeholder="add tag"
            maxLength={32}
            className="w-24 rounded-md border border-bg-border bg-bg px-2 py-0.5 text-xs text-text-primary placeholder:text-text-muted"
            aria-label="Add tag"
          />
          <button
            onClick={add}
            className="rounded-md p-1 text-text-muted hover:bg-bg hover:text-text-primary"
            aria-label="Add tag"
          >
            <Plus className="h-3.5 w-3.5" />
          </button>
        </span>
      )}
      {error && <span className="text-xs text-status-error">{error}</span>}
    </div>
  );
}

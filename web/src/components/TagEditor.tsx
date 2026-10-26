import { useState } from 'react';
import { Tag, X, Plus } from 'lucide-react';
import { setDeviceTags } from '../api/tags';

/** Tag list with inline add/remove for one unit — typed as decal captions. */
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
      {tags.length === 0 && readOnly && <span className="cap-sm cap text-text-muted">No tags</span>}
      {tags.map((t) => (
        <span
          key={t}
          className="cap-sm cap inline-flex items-center gap-1 border border-rule px-1.5 py-0.5 text-text-secondary"
        >
          {t}
          {!readOnly && (
            <button
              onClick={() => void save(tags.filter((x) => x !== t))}
              className="text-text-muted hover:text-[var(--error)]"
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
            className="input w-24 px-2 py-1 text-[11px]"
            aria-label="Add tag"
          />
          <button onClick={add} className="btn btn-quiet px-1.5 py-1" aria-label="Add tag">
            <Plus className="h-3.5 w-3.5" />
          </button>
        </span>
      )}
      {error && (
        <span role="alert" className="text-[11px] text-[var(--error)]">
          {error}
        </span>
      )}
    </div>
  );
}

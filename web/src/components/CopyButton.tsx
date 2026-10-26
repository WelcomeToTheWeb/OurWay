import { useState } from 'react';
import { Check, Copy } from 'lucide-react';

interface CopyButtonProps {
  text: string;
  className?: string;
}

/** Copies a measured value (device key, link, id) and stamps the result. */
export function CopyButton({ text, className = '' }: CopyButtonProps) {
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      const el = document.createElement('textarea');
      el.value = text;
      document.body.appendChild(el);
      el.select();
      document.execCommand('copy');
      document.body.removeChild(el);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }
  }

  return (
    <button
      type="button"
      onClick={handleCopy}
      aria-label={copied ? 'Copied' : 'Copy to clipboard'}
      className={`btn btn-quiet px-1.5 py-0.5 text-[11px] ${copied ? 'text-[var(--online)]' : ''} ${className}`}
    >
      {copied ? (
        <>
          <Check className="h-3 w-3" aria-hidden="true" />
          <span className="cap-sm cap">Copied</span>
        </>
      ) : (
        <Copy className="h-3 w-3" aria-hidden="true" />
      )}
    </button>
  );
}

import { useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';

type ShortcutHandler = (e: KeyboardEvent) => void;

interface Shortcut {
  key: string;
  ctrl?: boolean;
  alt?: boolean;
  shift?: boolean;
  handler: ShortcutHandler;
}

export function useKeyboardShortcuts(shortcuts: Shortcut[]) {
  const shortcutsRef = useRef(shortcuts);
  shortcutsRef.current = shortcuts;

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      // Ignore if user is typing in an input
      const target = e.target as HTMLElement;
      if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT') {
        return;
      }

      for (const shortcut of shortcutsRef.current) {
        const matches =
          e.key.toLowerCase() === shortcut.key.toLowerCase() &&
          (!shortcut.ctrl || e.ctrlKey) &&
          (!shortcut.alt || e.altKey) &&
          (!shortcut.shift || e.shiftKey);

        if (matches) {
          e.preventDefault();
          shortcut.handler(e);
          break;
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, []);
}

// Global shortcuts for the app
export function useGlobalShortcuts() {
  const navigate = useNavigate();

  // Handle chord shortcuts (g + key)
  const lastKeyRef = useRef<string | null>(null);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      const target = e.target as HTMLElement;
      if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT') {
        return;
      }

      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current);
      }

      if (lastKeyRef.current === 'g' || lastKeyRef.current === 'G') {
        switch (e.key.toLowerCase()) {
          case 'd':
            navigate('/devices');
            break;
          case 'a':
            navigate('/alerts');
            break;
          case 'f':
            navigate('/files');
            break;
          case 'p':
            navigate('/patches');
            break;
          case 'u':
            navigate('/users');
            break;
          case 's':
            navigate('/settings');
            break;
          case 'h':
            navigate('/');
            break;
        }
        lastKeyRef.current = null;
      } else {
        lastKeyRef.current = e.key;
        timeoutRef.current = setTimeout(() => {
          lastKeyRef.current = null;
        }, 500);
      }
    }

    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current);
      }
    };
  }, [navigate]);
}

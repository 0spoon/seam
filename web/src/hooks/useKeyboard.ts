import { useEffect } from 'react';

type KeyHandler = (e: KeyboardEvent) => void;

interface KeyBinding {
  key: string;
  ctrl?: boolean;
  meta?: boolean;
  shift?: boolean;
  handler: KeyHandler;
  /** If true, only trigger when no input/textarea is focused */
  global?: boolean;
}

export function useKeyboard(bindings: KeyBinding[]) {
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      const isInput =
        target.tagName === 'INPUT' ||
        target.tagName === 'TEXTAREA' ||
        target.isContentEditable;

      for (const binding of bindings) {
        if (binding.global && isInput) continue;

        const ctrlOrMeta = binding.ctrl || binding.meta;
        const matchesMod = ctrlOrMeta
          ? e.ctrlKey || e.metaKey
          : !e.ctrlKey && !e.metaKey;
        const matchesShift = binding.shift ? e.shiftKey : !e.shiftKey;

        if (e.key === binding.key && matchesMod && matchesShift) {
          e.preventDefault();
          binding.handler(e);
          return;
        }
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [bindings]);
}

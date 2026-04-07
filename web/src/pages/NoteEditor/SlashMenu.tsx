import { useEffect, useRef, useCallback, useState } from 'react';
import {
  Heading1,
  Heading2,
  Heading3,
  Bold,
  Italic,
  Code,
  List,
  ListChecks,
  Link2,
  Minus,
  Calendar,
  Clock,
  Quote,
  Table,
} from 'lucide-react';
import type { SlashCommand } from '../../lib/slashCommands';
import { slashCommands } from '../../lib/slashCommands';
import styles from './SlashMenu.module.css';

const iconMap: Record<string, React.ComponentType<{ size?: number }>> = {
  Heading1,
  Heading2,
  Heading3,
  Bold,
  Italic,
  Code,
  List,
  ListChecks,
  Link2,
  Minus,
  Calendar,
  Clock,
  Quote,
  Table,
};

interface SlashMenuProps {
  active: boolean;
  query: string;
  position: { top: number; left: number } | null;
  onSelect: (command: SlashCommand) => void;
  onDismiss: () => void;
}

// Simple fuzzy match: all query characters must appear in order in the target.
function fuzzyMatch(query: string, target: string): boolean {
  const q = query.toLowerCase();
  const t = target.toLowerCase();
  let qi = 0;
  for (let ti = 0; ti < t.length && qi < q.length; ti++) {
    if (t[ti] === q[qi]) {
      qi++;
    }
  }
  return qi === q.length;
}

// Returns indices of matched characters for highlighting.
function fuzzyMatchIndices(query: string, target: string): number[] {
  const q = query.toLowerCase();
  const t = target.toLowerCase();
  const indices: number[] = [];
  let qi = 0;
  for (let ti = 0; ti < t.length && qi < q.length; ti++) {
    if (t[ti] === q[qi]) {
      indices.push(ti);
      qi++;
    }
  }
  return qi === q.length ? indices : [];
}

function filterCommands(query: string): SlashCommand[] {
  if (!query) return slashCommands;

  return slashCommands.filter((cmd) => {
    if (fuzzyMatch(query, cmd.label)) return true;
    if (fuzzyMatch(query, cmd.id)) return true;
    if (cmd.keywords?.some((kw) => fuzzyMatch(query, kw))) return true;
    return false;
  });
}

function HighlightedLabel({ label, query }: { label: string; query: string }) {
  if (!query) return <>{label}</>;

  const indices = new Set(fuzzyMatchIndices(query, label));
  if (indices.size === 0) return <>{label}</>;

  return (
    <>
      {label.split('').map((char, i) =>
        indices.has(i) ? (
          <span key={i} className={styles.highlight}>
            {char}
          </span>
        ) : (
          <span key={i}>{char}</span>
        ),
      )}
    </>
  );
}

export function SlashMenu({
  active,
  query,
  position,
  onSelect,
  onDismiss,
}: SlashMenuProps) {
  const [selectedIndex, setSelectedIndex] = useState(0);
  const listRef = useRef<HTMLUListElement>(null);
  const filtered = filterCommands(query);

  // Reset selection when query changes. Selection index is reset when the
  // (external) query input changes.
  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    setSelectedIndex(0);
  }, [query]);
  /* eslint-enable react-hooks/set-state-in-effect */

  // Scroll the active item into view
  useEffect(() => {
    if (!listRef.current) return;
    const activeItem = listRef.current.children[selectedIndex] as HTMLElement | undefined;
    activeItem?.scrollIntoView({ block: 'nearest' });
  }, [selectedIndex]);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (!active) return;

      if (e.key === 'ArrowDown') {
        e.preventDefault();
        e.stopPropagation();
        setSelectedIndex((i) => (i + 1) % Math.max(1, filtered.length));
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        e.stopPropagation();
        setSelectedIndex((i) =>
          i <= 0 ? Math.max(0, filtered.length - 1) : i - 1,
        );
      } else if (e.key === 'Enter' || e.key === 'Tab') {
        e.preventDefault();
        e.stopPropagation();
        if (filtered.length > 0 && selectedIndex < filtered.length) {
          onSelect(filtered[selectedIndex]);
        }
      } else if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        onDismiss();
      }
    },
    [active, filtered, selectedIndex, onSelect, onDismiss],
  );

  useEffect(() => {
    if (!active) return;
    // Use capture phase to intercept keys before CodeMirror
    document.addEventListener('keydown', handleKeyDown, true);
    return () => {
      document.removeEventListener('keydown', handleKeyDown, true);
    };
  }, [active, handleKeyDown]);

  if (!active || !position) return null;

  return (
    <div
      className={styles.container}
      style={{ top: position.top + 4, left: position.left }}
      role="listbox"
      aria-label="Slash commands"
    >
      {filtered.length === 0 ? (
        <div className={styles.empty}>No matching commands</div>
      ) : (
        <ul ref={listRef} className={styles.list}>
          {filtered.map((cmd, i) => {
            const Icon = iconMap[cmd.icon];
            return (
              <li
                key={cmd.id}
                role="option"
                aria-selected={i === selectedIndex}
                className={`${styles.item} ${i === selectedIndex ? styles.itemActive : ''}`}
                onMouseEnter={() => setSelectedIndex(i)}
                onMouseDown={(e) => {
                  // Prevent focus loss from the editor
                  e.preventDefault();
                  onSelect(cmd);
                }}
              >
                <span className={styles.itemIcon}>
                  {Icon && <Icon size={16} />}
                </span>
                <span className={styles.itemContent}>
                  <span className={styles.itemLabel}>
                    <HighlightedLabel label={cmd.label} query={query} />
                  </span>
                  <span className={styles.itemDescription}>
                    {cmd.description}
                  </span>
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

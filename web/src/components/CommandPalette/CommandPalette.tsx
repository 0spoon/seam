import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import {
  Search,
  Clock,
  FileText,
  Folder,
  Hash,
  Plus,
  Inbox,
  Network,
  Calendar,
  MessageCircle,
  Settings,
  PanelLeftClose,
  PanelRight,
  PenLine,
  Columns2,
  Eye,
  Files,
  Trash2,
  Download,
  RefreshCw,
  type LucideIcon,
} from 'lucide-react';
import { useUIStore } from '../../stores/uiStore';
import { useProjectStore } from '../../stores/projectStore';
import { commandRegistry } from '../../lib/commandRegistry';
import { navigate } from '../../lib/navigation';
import { fuzzyMatch } from '../../lib/fuzzyMatch';
import { getRecentNotes } from '../../lib/recentNotes';
import { searchFTS } from '../../api/client';
import { timeAgo } from '../../lib/dates';
import type { FTSResult, TagCount } from '../../api/types';
import styles from './CommandPalette.module.css';

// Map lucide icon names to components.
const iconMap: Record<string, LucideIcon> = {
  Plus,
  Search,
  Inbox,
  Network,
  Calendar,
  MessageCircle,
  Settings,
  PanelLeftClose,
  PanelRight,
  PenLine,
  Columns2,
  Eye,
  Files,
  Trash2,
  Download,
  RefreshCw,
  Folder,
  Hash,
  FileText,
  Clock,
};

function getIcon(name?: string): LucideIcon {
  if (!name) return FileText;
  return iconMap[name] ?? FileText;
}

type PaletteMode = 'notes' | 'commands' | 'tags' | 'projects';

interface ResultItem {
  id: string;
  label: string;
  icon: LucideIcon;
  meta?: string;
  shortcut?: string;
  section: string;
  matchIndices?: number[];
  action: () => void;
}

// Render a label with matched characters highlighted.
function HighlightedLabel({ text, indices }: { text: string; indices?: number[] }) {
  if (!indices || indices.length === 0) {
    return <>{text}</>;
  }
  const indexSet = new Set(indices);
  const parts: React.ReactNode[] = [];
  for (let i = 0; i < text.length; i++) {
    if (indexSet.has(i)) {
      parts.push(
        <span key={i} className={styles.highlight}>
          {text[i]}
        </span>,
      );
    } else {
      parts.push(text[i]);
    }
  }
  return <>{parts}</>;
}

export function CommandPalette() {
  const isOpen = useUIStore((s) => s.commandPaletteOpen);
  const setOpen = useUIStore((s) => s.setCommandPaletteOpen);
  const tags = useUIStore((s) => s.tags);
  const projects = useProjectStore((s) => s.projects);

  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [ftsResults, setFtsResults] = useState<FTSResult[]>([]);
  const [ftsLoading, setFtsLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const paletteRef = useRef<HTMLDivElement>(null);
  const backdropRef = useRef<HTMLDivElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const abortRef = useRef<AbortController | null>(null);
  const resultsRef = useRef<HTMLDivElement>(null);

  // Register dynamic project commands.
  useEffect(() => {
    projects.forEach((p) => {
      commandRegistry.register({
        id: `project-${p.id}`,
        label: `Open project: ${p.name}`,
        category: 'project',
        icon: 'Folder',
        action: () => {
          setOpen(false);
          navigate(`/projects/${p.id}`);
        },
      });
    });
    return () => {
      projects.forEach((p) => commandRegistry.unregister(`project-${p.id}`));
    };
  }, [projects, setOpen]);

  // Determine mode from query prefix.
  const mode: PaletteMode = useMemo(() => {
    if (query.startsWith('>')) return 'commands';
    if (query.startsWith('#')) return 'tags';
    if (query.startsWith('@')) return 'projects';
    return 'notes';
  }, [query]);

  // The actual search string (strip mode prefix).
  const searchQuery = useMemo(() => {
    if (mode === 'commands') return query.slice(1).trim();
    if (mode === 'tags') return query.slice(1).trim();
    if (mode === 'projects') return query.slice(1).trim();
    return query.trim();
  }, [query, mode]);

  const modeLabel = useMemo(() => {
    switch (mode) {
      case 'commands':
        return 'Commands';
      case 'tags':
        return 'Tags';
      case 'projects':
        return 'Projects';
      default:
        return query.trim() ? 'Notes' : '';
    }
  }, [mode, query]);

  const placeholder = useMemo(() => {
    switch (mode) {
      case 'commands':
        return 'Search commands...';
      case 'tags':
        return 'Search tags...';
      case 'projects':
        return 'Search projects...';
      default:
        return 'Search notes, >commands, #tags, @projects...';
    }
  }, [mode]);

  // Debounced FTS search for note mode. The effect synchronizes local result
  // state with the external (debounced) FTS request lifecycle.
  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    if (mode !== 'notes' || !searchQuery) {
      setFtsResults([]);
      setFtsLoading(false);
      return;
    }

    setFtsLoading(true);

    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
    }
    if (abortRef.current) {
      abortRef.current.abort();
    }

    debounceRef.current = setTimeout(() => {
      const controller = new AbortController();
      abortRef.current = controller;
      searchFTS(searchQuery, 10, 0, controller.signal)
        .then(({ results }) => {
          if (!controller.signal.aborted) {
            setFtsResults(results);
            setFtsLoading(false);
          }
        })
        .catch(() => {
          if (!controller.signal.aborted) {
            setFtsResults([]);
            setFtsLoading(false);
          }
        });
    }, 150);

    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      if (abortRef.current) abortRef.current.abort();
    };
  }, [mode, searchQuery]);

  // Build the flat result list grouped by sections.
  const resultItems: ResultItem[] = useMemo(() => {
    const items: ResultItem[] = [];

    if (mode === 'commands') {
      // Filter commands from registry.
      const cmds = commandRegistry.getAll();
      const filtered = searchQuery
        ? cmds
            .map((cmd) => {
              const match = fuzzyMatch(searchQuery, cmd.label);
              return match ? { cmd, score: match.score, indices: match.matches } : null;
            })
            .filter(Boolean)
            .sort((a, b) => b!.score - a!.score)
            .map((r) => r!)
        : cmds.map((cmd) => ({ cmd, score: 0, indices: [] as number[] }));

      for (const { cmd, indices } of filtered) {
        items.push({
          id: cmd.id,
          label: cmd.label,
          icon: getIcon(cmd.icon),
          shortcut: cmd.shortcut,
          section: 'COMMANDS',
          matchIndices: indices,
          action: cmd.action,
        });
      }
      return items;
    }

    if (mode === 'tags') {
      const filtered = filterTags(tags, searchQuery);
      for (const { tag, indices } of filtered) {
        items.push({
          id: `tag-${tag.name}`,
          label: `#${tag.name}`,
          icon: Hash,
          meta: `${tag.count} note${tag.count === 1 ? '' : 's'}`,
          section: 'TAGS',
          matchIndices: indices.map((i) => i + 1), // offset by 1 for the # prefix
          action: () => {
            setOpen(false);
            navigate(`/?tag=${encodeURIComponent(tag.name)}`);
          },
        });
      }
      return items;
    }

    if (mode === 'projects') {
      const filtered = filterProjects(projects, searchQuery);
      for (const { project, indices } of filtered) {
        items.push({
          id: `project-nav-${project.id}`,
          label: project.name,
          icon: Folder,
          section: 'PROJECTS',
          matchIndices: indices,
          action: () => {
            setOpen(false);
            navigate(`/projects/${project.id}`);
          },
        });
      }
      return items;
    }

    // Notes mode: show recent notes when query is empty, FTS results when typing.
    if (!searchQuery) {
      const recent = getRecentNotes();
      for (const note of recent) {
        items.push({
          id: `recent-${note.id}`,
          label: note.title,
          icon: Clock,
          meta: timeAgo(new Date(note.openedAt).toISOString()),
          section: 'RECENT',
          action: () => {
            setOpen(false);
            navigate(`/notes/${note.id}`);
          },
        });
      }
      return items;
    }

    // FTS results
    for (const result of ftsResults) {
      items.push({
        id: `note-${result.note_id}`,
        label: result.title,
        icon: FileText,
        meta: result.snippet ? result.snippet.replace(/<\/?b>/g, '').slice(0, 60) : undefined,
        section: 'NOTES',
        action: () => {
          setOpen(false);
          navigate(`/notes/${result.note_id}`);
        },
      });
    }

    return items;
  }, [mode, searchQuery, ftsResults, tags, projects, setOpen]);

  // Reset selection when results change.
  useEffect(() => {
    setSelectedIndex(0);
  }, [resultItems.length, query]);

  // Open/close handling: reset internal state when the palette opens, syncing
  // with the external open/close lifecycle.
  useEffect(() => {
    if (isOpen) {
      previousFocusRef.current = document.activeElement as HTMLElement | null;
      setQuery('');
      setSelectedIndex(0);
      setFtsResults([]);
      setTimeout(() => inputRef.current?.focus(), 50);
    }
  }, [isOpen]);
  /* eslint-enable react-hooks/set-state-in-effect */

  // Scroll selected item into view.
  useEffect(() => {
    if (!resultsRef.current) return;
    const items = resultsRef.current.querySelectorAll(`button[role="option"]`);
    const target = items[selectedIndex] as HTMLElement | undefined;
    if (target && typeof target.scrollIntoView === 'function') {
      target.scrollIntoView({ block: 'nearest' });
    }
  }, [selectedIndex]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault();
          setSelectedIndex((i) => Math.min(i + 1, resultItems.length - 1));
          break;
        case 'ArrowUp':
          e.preventDefault();
          setSelectedIndex((i) => Math.max(i - 1, 0));
          break;
        case 'Enter':
          e.preventDefault();
          if (resultItems[selectedIndex]) {
            resultItems[selectedIndex].action();
          }
          break;
        case 'Escape':
          e.preventDefault();
          setOpen(false);
          break;
        case 'Tab': {
          // Focus trap: keep Tab cycling within the palette.
          const container = paletteRef.current;
          if (!container) break;
          const focusable = container.querySelectorAll<HTMLElement>(
            'input:not([disabled]), button:not([disabled]), [tabindex]:not([tabindex="-1"])',
          );
          if (focusable.length === 0) break;
          const first = focusable[0];
          const last = focusable[focusable.length - 1];
          if (e.shiftKey && document.activeElement === first) {
            e.preventDefault();
            last.focus();
          } else if (!e.shiftKey && document.activeElement === last) {
            e.preventDefault();
            first.focus();
          }
          break;
        }
      }
    },
    [resultItems, selectedIndex, setOpen],
  );

  const handleBackdropClick = useCallback(
    (e: React.MouseEvent) => {
      if (e.target === backdropRef.current) {
        setOpen(false);
      }
    },
    [setOpen],
  );

  // Group items by section for rendering.
  const sections = useMemo(() => {
    const grouped: { section: string; items: ResultItem[] }[] = [];
    let currentSection = '';
    for (const item of resultItems) {
      if (item.section !== currentSection) {
        currentSection = item.section;
        grouped.push({ section: currentSection, items: [] });
      }
      grouped[grouped.length - 1].items.push(item);
    }
    return grouped;
  }, [resultItems]);

  // Pre-compute global index map for each item ID.
  const globalIndexMap = useMemo(() => {
    const map = new Map<string, number>();
    let idx = 0;
    for (const section of sections) {
      for (const item of section.items) {
        map.set(item.id, idx++);
      }
    }
    return map;
  }, [sections]);

  return (
    <AnimatePresence
      onExitComplete={() => {
        if (previousFocusRef.current) {
          previousFocusRef.current.focus();
          previousFocusRef.current = null;
        }
      }}
    >
      {isOpen && (
        <motion.div
          ref={backdropRef}
          className={styles.backdrop}
          onClick={handleBackdropClick}
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.25, ease: [0.4, 0, 1, 1] }}
        >
          <motion.div
            ref={paletteRef}
            className={styles.palette}
            onKeyDown={handleKeyDown}
            role="dialog"
            aria-modal="true"
            aria-label="Command palette"
            initial={{ opacity: 0, scale: 0.98 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.98 }}
            transition={{ duration: 0.25, ease: [0.16, 1, 0.3, 1] }}
          >
            <div className={styles.inputRow}>
              {modeLabel && <span className={styles.modeLabel}>{modeLabel}</span>}
              <input
                ref={inputRef}
                type="text"
                className={styles.input}
                placeholder={placeholder}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                aria-label="Command palette search"
                aria-keyshortcuts="Meta+K Control+K"
              />
            </div>
            <div className={styles.results} role="listbox" ref={resultsRef}>
              {sections.map((section) => (
                <div key={section.section}>
                  <div className={styles.sectionHeader}>{section.section}</div>
                  {section.items.map((item) => {
                    const itemIndex = globalIndexMap.get(item.id) ?? 0;
                    const Icon = item.icon;
                    return (
                      <button
                        key={item.id}
                        className={`${styles.item} ${itemIndex === selectedIndex ? styles.selected : ''}`}
                        onClick={item.action}
                        role="option"
                        aria-selected={itemIndex === selectedIndex}
                        onMouseEnter={() => setSelectedIndex(itemIndex)}
                      >
                        <span className={styles.icon}>
                          <Icon size={16} />
                        </span>
                        <span className={styles.label}>
                          <HighlightedLabel text={item.label} indices={item.matchIndices} />
                        </span>
                        {item.meta && <span className={styles.meta}>{item.meta}</span>}
                        {item.shortcut && <span className={styles.shortcut}>{item.shortcut}</span>}
                      </button>
                    );
                  })}
                </div>
              ))}
              {resultItems.length === 0 && !ftsLoading && (
                <div className={styles.empty}>
                  {mode === 'notes' && !searchQuery ? 'No recent notes' : 'No results found'}
                </div>
              )}
              {ftsLoading && mode === 'notes' && searchQuery && (
                <div className={styles.empty}>Searching...</div>
              )}
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

// Helper: filter tags by fuzzy match.
function filterTags(tags: TagCount[], query: string): { tag: TagCount; indices: number[] }[] {
  if (!query) {
    return tags.map((tag) => ({ tag, indices: [] }));
  }
  const results: { tag: TagCount; indices: number[]; score: number }[] = [];
  for (const tag of tags) {
    const match = fuzzyMatch(query, tag.name);
    if (match) {
      results.push({ tag, indices: match.matches, score: match.score });
    }
  }
  results.sort((a, b) => b.score - a.score);
  return results;
}

// Helper: filter projects by fuzzy match.
function filterProjects(
  projects: { id: string; name: string }[],
  query: string,
): { project: { id: string; name: string }; indices: number[] }[] {
  if (!query) {
    return projects.map((project) => ({ project, indices: [] }));
  }
  const results: {
    project: { id: string; name: string };
    indices: number[];
    score: number;
  }[] = [];
  for (const project of projects) {
    const match = fuzzyMatch(query, project.name);
    if (match) {
      results.push({ project, indices: match.matches, score: match.score });
    }
  }
  results.sort((a, b) => b.score - a.score);
  return results;
}

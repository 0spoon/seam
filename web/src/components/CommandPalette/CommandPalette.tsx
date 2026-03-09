import { useState, useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Plus,
  Search,
  Folder,
  Network,
  Calendar,
  PanelLeftClose,
  MessageCircle,
} from 'lucide-react';
import { useUIStore } from '../../stores/uiStore';
import { useProjectStore } from '../../stores/projectStore';
import styles from './CommandPalette.module.css';

interface CommandItem {
  id: string;
  label: string;
  icon: React.ReactNode;
  secondary?: string;
  shortcut?: string;
  action: () => void;
}

export function CommandPalette() {
  const isOpen = useUIStore((s) => s.commandPaletteOpen);
  const setOpen = useUIStore((s) => s.setCommandPaletteOpen);
  const setCaptureModalOpen = useUIStore((s) => s.setCaptureModalOpen);
  const toggleSidebar = useUIStore((s) => s.toggleSidebar);
  const projects = useProjectStore((s) => s.projects);
  const navigate = useNavigate();

  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const paletteRef = useRef<HTMLDivElement>(null);
  const backdropRef = useRef<HTMLDivElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);

  const baseCommands: CommandItem[] = [
    {
      id: 'new-note',
      label: 'New note',
      icon: <Plus size={16} />,
      shortcut: 'Cmd+N',
      action: () => {
        setOpen(false);
        setCaptureModalOpen(true);
      },
    },
    {
      id: 'search',
      label: 'Search notes',
      icon: <Search size={16} />,
      shortcut: '/',
      action: () => {
        setOpen(false);
        navigate('/search');
      },
    },
    {
      id: 'graph',
      label: 'Graph view',
      icon: <Network size={16} />,
      action: () => {
        setOpen(false);
        navigate('/graph');
      },
    },
    {
      id: 'timeline',
      label: 'Timeline',
      icon: <Calendar size={16} />,
      action: () => {
        setOpen(false);
        navigate('/timeline');
      },
    },
    {
      id: 'ask-seam',
      label: 'Ask Seam',
      icon: <MessageCircle size={16} />,
      action: () => {
        setOpen(false);
        navigate('/ask');
      },
    },
    {
      id: 'toggle-sidebar',
      label: 'Toggle sidebar',
      icon: <PanelLeftClose size={16} />,
      shortcut: 'Cmd+\\',
      action: () => {
        setOpen(false);
        toggleSidebar();
      },
    },
  ];

  const projectCommands: CommandItem[] = projects.map((p) => ({
    id: `project-${p.id}`,
    label: `Open project: ${p.name}`,
    icon: <Folder size={16} />,
    action: () => {
      setOpen(false);
      navigate(`/projects/${p.id}`);
    },
  }));

  const allCommands = [...baseCommands, ...projectCommands];

  const filtered = query
    ? allCommands.filter((cmd) =>
        cmd.label.toLowerCase().includes(query.toLowerCase()),
      )
    : allCommands;

  useEffect(() => {
    if (isOpen) {
      // Save the element that had focus before opening so we can restore it.
      previousFocusRef.current = document.activeElement as HTMLElement | null;
      setQuery('');
      setSelectedIndex(0);
      setTimeout(() => inputRef.current?.focus(), 50);
    } else if (previousFocusRef.current) {
      // Return focus to the previously focused element on close.
      previousFocusRef.current.focus();
      previousFocusRef.current = null;
    }
  }, [isOpen]);

  useEffect(() => {
    setSelectedIndex(0);
  }, [query]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        setSelectedIndex((i) => Math.min(i + 1, filtered.length - 1));
        break;
      case 'ArrowUp':
        e.preventDefault();
        setSelectedIndex((i) => Math.max(i - 1, 0));
        break;
      case 'Enter':
        e.preventDefault();
        if (filtered[selectedIndex]) {
          filtered[selectedIndex].action();
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
  };

  const handleBackdropClick = (e: React.MouseEvent) => {
    if (e.target === backdropRef.current) {
      setOpen(false);
    }
  };

  if (!isOpen) return null;

  return (
    <div
      ref={backdropRef}
      className={styles.backdrop}
      onClick={handleBackdropClick}
    >
      <div
        ref={paletteRef}
        className={styles.palette}
        onKeyDown={handleKeyDown}
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
      >
        <input
          ref={inputRef}
          type="text"
          className={styles.input}
          placeholder="Type a command..."
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          aria-label="Command palette search"
        />
        <div className={styles.results} role="listbox">
          {filtered.map((cmd, index) => (
            <button
              key={cmd.id}
              className={`${styles.item} ${index === selectedIndex ? styles.selected : ''}`}
              onClick={cmd.action}
              role="option"
              aria-selected={index === selectedIndex}
              onMouseEnter={() => setSelectedIndex(index)}
            >
              <span className={styles.icon}>{cmd.icon}</span>
              <span className={styles.label}>{cmd.label}</span>
              {cmd.shortcut && (
                <span className={styles.shortcut}>{cmd.shortcut}</span>
              )}
            </button>
          ))}
          {filtered.length === 0 && (
            <div className={styles.empty}>No matching commands</div>
          )}
        </div>
      </div>
    </div>
  );
}

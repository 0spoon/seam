// Central command registry for the command palette.
// Commands register themselves here and the palette reads from this registry.

import { navigate } from './navigation';
import { useUIStore } from '../stores/uiStore';
import { useToastStore } from '../components/Toast/ToastContainer';
import { fuzzyFilter } from './fuzzyMatch';

export interface Command {
  id: string;
  label: string;
  category: 'navigation' | 'editor' | 'ai' | 'view' | 'project' | 'tag';
  shortcut?: string; // display string like "Cmd+N"
  icon?: string; // lucide icon name
  action: () => void;
  when?: () => boolean; // contextual visibility predicate
}

class CommandRegistry {
  private commands: Map<string, Command> = new Map();

  register(cmd: Command): void {
    this.commands.set(cmd.id, cmd);
  }

  unregister(id: string): void {
    this.commands.delete(id);
  }

  getAll(): Command[] {
    return Array.from(this.commands.values()).filter(
      (cmd) => !cmd.when || cmd.when(),
    );
  }

  getFiltered(query: string, mode: 'command' | 'all'): Command[] {
    const available = this.getAll();
    if (!query) return available;

    return fuzzyFilter(query, available, (cmd) => cmd.label).map((item) => ({
      ...item,
      // Strip the fuzzyFilter additions for clean Command type
    }));
  }
}

export const commandRegistry = new CommandRegistry();

// Register default commands. Actions use the navigation helper or store
// methods so they work outside React component scope.

function registerDefaults() {
  // -- Navigation commands --
  commandRegistry.register({
    id: 'new-note',
    label: 'New note',
    category: 'navigation',
    shortcut: 'Cmd+N',
    icon: 'Plus',
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      useUIStore.getState().setCaptureModalOpen(true);
    },
  });

  commandRegistry.register({
    id: 'search',
    label: 'Search notes',
    category: 'navigation',
    shortcut: '/',
    icon: 'Search',
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      navigate('/search');
    },
  });

  commandRegistry.register({
    id: 'inbox',
    label: 'Go to Inbox',
    category: 'navigation',
    icon: 'Inbox',
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      navigate('/');
    },
  });

  commandRegistry.register({
    id: 'graph',
    label: 'Graph view',
    category: 'navigation',
    icon: 'Network',
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      navigate('/graph');
    },
  });

  commandRegistry.register({
    id: 'timeline',
    label: 'Timeline',
    category: 'navigation',
    icon: 'Calendar',
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      navigate('/timeline');
    },
  });

  commandRegistry.register({
    id: 'ask-seam',
    label: 'Ask Seam',
    category: 'navigation',
    icon: 'MessageCircle',
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      navigate('/ask');
    },
  });

  commandRegistry.register({
    id: 'settings',
    label: 'Settings',
    category: 'navigation',
    icon: 'Settings',
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      navigate('/settings');
    },
  });

  // -- View commands --
  commandRegistry.register({
    id: 'toggle-sidebar',
    label: 'Toggle sidebar',
    category: 'view',
    shortcut: 'Cmd+\\',
    icon: 'PanelLeftClose',
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      useUIStore.getState().toggleSidebar();
    },
  });

  commandRegistry.register({
    id: 'toggle-right-panel',
    label: 'Toggle right panel',
    category: 'view',
    icon: 'PanelRight',
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      useUIStore.getState().toggleRightPanel();
    },
  });

  commandRegistry.register({
    id: 'view-editor',
    label: 'Editor only view',
    category: 'view',
    icon: 'PenLine',
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      useUIStore.getState().setEditorViewMode('editor');
    },
  });

  commandRegistry.register({
    id: 'view-split',
    label: 'Split view',
    category: 'view',
    icon: 'Columns2',
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      useUIStore.getState().setEditorViewMode('split');
    },
  });

  commandRegistry.register({
    id: 'view-preview',
    label: 'Preview only view',
    category: 'view',
    icon: 'Eye',
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      useUIStore.getState().setEditorViewMode('preview');
    },
  });

  // -- Editor commands (contextual: only visible on note pages) --
  const isOnNotePage = () => window.location.pathname.startsWith('/notes/');

  commandRegistry.register({
    id: 'duplicate-note',
    label: 'Duplicate current note',
    category: 'editor',
    icon: 'Files',
    when: isOnNotePage,
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      // Dispatch a custom event that NoteEditorPage can listen to,
      // since we cannot call component callbacks from here directly.
      window.dispatchEvent(new CustomEvent('seam:command', { detail: 'duplicate-note' }));
    },
  });

  commandRegistry.register({
    id: 'delete-note',
    label: 'Delete current note',
    category: 'editor',
    icon: 'Trash2',
    when: isOnNotePage,
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      window.dispatchEvent(new CustomEvent('seam:command', { detail: 'delete-note' }));
    },
  });

  commandRegistry.register({
    id: 'export-markdown',
    label: 'Export as Markdown',
    category: 'editor',
    icon: 'Download',
    when: isOnNotePage,
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      window.dispatchEvent(new CustomEvent('seam:command', { detail: 'export-markdown' }));
    },
  });

  // -- AI commands --
  commandRegistry.register({
    id: 'reindex-embeddings',
    label: 'Reindex embeddings',
    category: 'ai',
    icon: 'RefreshCw',
    action: () => {
      useUIStore.getState().setCommandPaletteOpen(false);
      fetch('/api/ai/embeddings/reindex', { method: 'POST' })
        .then(() => {
          useToastStore.getState().addToast('Reindex started', 'success');
        })
        .catch(() => {
          useToastStore.getState().addToast('Failed to start reindex', 'error');
        });
    },
  });
}

registerDefaults();

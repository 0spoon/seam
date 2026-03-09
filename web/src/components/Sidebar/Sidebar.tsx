import { useNavigate, useLocation } from 'react-router-dom';
import {
  Search,
  Inbox,
  Tag,
  Settings,
  Plus,
  PanelLeftClose,
  PanelLeftOpen,
  MessageCircle,
  Network,
  Calendar,
  Check,
  MoreHorizontal,
  Pencil,
  Trash2,
  FilePlus,
} from 'lucide-react';
import { useUIStore } from '../../stores/uiStore';
import { useProjectStore } from '../../stores/projectStore';
import { useSettingsStore } from '../../stores/settingsStore';
import { useAuthStore } from '../../stores/authStore';
import { useKeyboard } from '../../hooks/useKeyboard';
import { useNoteWebSocket } from '../../hooks/useWebSocket';
import { useNoteStore } from '../../stores/noteStore';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { getProjectColor } from '../../lib/tagColor';
import { searchFTS } from '../../api/client';
import type { FTSResult } from '../../api/types';
import { sanitizeHtml } from '../../lib/sanitize';
import styles from './Sidebar.module.css';
import { useState, useRef, useCallback, useEffect, useMemo } from 'react';
import { useToastStore } from '../../components/Toast/ToastContainer';

export function Sidebar() {
  const navigate = useNavigate();
  const location = useLocation();
  const collapsed = useUIStore((s) => s.sidebarCollapsed);
  const sidebarOpen = useUIStore((s) => s.sidebarOpen);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const toggleSidebar = useUIStore((s) => s.toggleSidebar);
  const tags = useUIStore((s) => s.tags);
  const setCaptureModalOpen = useUIStore((s) => s.setCaptureModalOpen);
  const projects = useProjectStore((s) => s.projects);
  const user = useAuthStore((s) => s.user);
  const addToast = useToastStore((s) => s.addToast);
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<FTSResult[]>([]);
  const [showDropdown, setShowDropdown] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(-1);
  const searchRef = useRef<HTMLInputElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const createProject = useProjectStore((s) => s.createProject);
  const settingsProjectsExpanded = useSettingsStore((s) => s.settings.sidebar_projects_expanded);
  const settingsTagsExpanded = useSettingsStore((s) => s.settings.sidebar_tags_expanded);
  const updateSetting = useSettingsStore((s) => s.updateSetting);
  const projectsExpanded = settingsProjectsExpanded !== 'false';
  const tagsExpanded = settingsTagsExpanded !== 'false';
  const setProjectsExpanded = useCallback((expanded: boolean) => {
    updateSetting('sidebar_projects_expanded', String(expanded));
  }, [updateSetting]);
  const setTagsExpanded = useCallback((expanded: boolean) => {
    updateSetting('sidebar_tags_expanded', String(expanded));
  }, [updateSetting]);
  const [showNewProject, setShowNewProject] = useState(false);
  const [newProjectName, setNewProjectName] = useState('');
  const newProjectRef = useRef<HTMLInputElement>(null);
  const createNoteInProject = useNoteStore((s) => s.createNote);
  const updateProject = useProjectStore((s) => s.updateProject);
  const deleteProject = useProjectStore((s) => s.deleteProject);
  const [contextMenuProjectId, setContextMenuProjectId] = useState<string | null>(null);
  const [renamingProjectId, setRenamingProjectId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [deleteConfirm, setDeleteConfirm] = useState<{ open: boolean; projectId: string; projectName: string }>({ open: false, projectId: '', projectName: '' });
  const [deleteCascade, setDeleteCascade] = useState<'inbox' | 'delete'>('inbox');
  const contextMenuRef = useRef<HTMLDivElement>(null);

  // Subscribe to WS note.changed events so the note list stays fresh.
  useNoteWebSocket();

  const focusSearch = useCallback(() => {
    searchRef.current?.focus();
  }, []);

  const keyBindings = useMemo(() => [
    { key: '/', global: true, handler: focusSearch },
  ], [focusSearch]);

  useKeyboard(keyBindings);

  // Debounced inline search as user types.
  useEffect(() => {
    if (!searchQuery.trim()) {
      setSearchResults([]);
      setShowDropdown(false);
      return;
    }

    if (searchTimerRef.current) {
      clearTimeout(searchTimerRef.current);
    }

    searchTimerRef.current = setTimeout(async () => {
      try {
        const { results } = await searchFTS(searchQuery.trim(), 5, 0);
        setSearchResults(results);
        setShowDropdown(results.length > 0);
        setSelectedIndex(-1);
      } catch {
        setSearchResults([]);
        setShowDropdown(false);
      }
    }, 200);

    return () => {
      if (searchTimerRef.current) {
        clearTimeout(searchTimerRef.current);
      }
    };
  }, [searchQuery]);

  // Close dropdown when clicking outside.
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (
        dropdownRef.current &&
        !dropdownRef.current.contains(e.target as Node) &&
        searchRef.current &&
        !searchRef.current.contains(e.target as Node)
      ) {
        setShowDropdown(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const handleSearchSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (searchQuery.trim()) {
      setShowDropdown(false);
      navTo(`/search?q=${encodeURIComponent(searchQuery.trim())}`);
    }
  };

  const handleSearchKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      setSearchQuery('');
      setShowDropdown(false);
      searchRef.current?.blur();
    } else if (e.key === 'ArrowDown' && showDropdown) {
      e.preventDefault();
      setSelectedIndex((prev) => Math.min(prev + 1, searchResults.length - 1));
    } else if (e.key === 'ArrowUp' && showDropdown) {
      e.preventDefault();
      setSelectedIndex((prev) => Math.max(prev - 1, -1));
    } else if (e.key === 'Enter' && showDropdown && selectedIndex >= 0) {
      e.preventDefault();
      const result = searchResults[selectedIndex];
      if (result) {
        setShowDropdown(false);
        setSearchQuery('');
        navTo(`/notes/${result.note_id}`);
      }
    }
  };

  // Close mobile sidebar overlay after navigating.
  const navTo = useCallback((path: string) => {
    navigate(path);
    setSidebarOpen(false);
  }, [navigate, setSidebarOpen]);

  const handleCreateProject = useCallback(async () => {
    const name = newProjectName.trim();
    if (!name) return;
    try {
      const project = await createProject({ name });
      setShowNewProject(false);
      setNewProjectName('');
      navTo(`/projects/${project.id}`);
    } catch {
      addToast('Failed to create project', 'error');
    }
  }, [newProjectName, createProject, navTo, addToast]);

  useEffect(() => {
    if (showNewProject) {
      setTimeout(() => newProjectRef.current?.focus(), 50);
    }
  }, [showNewProject]);

  // Close context menu on outside click
  useEffect(() => {
    if (!contextMenuProjectId) return;
    const handleClick = (e: MouseEvent) => {
      if (contextMenuRef.current && !contextMenuRef.current.contains(e.target as Node)) {
        setContextMenuProjectId(null);
      }
    };
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setContextMenuProjectId(null);
    };
    document.addEventListener('mousedown', handleClick);
    document.addEventListener('keydown', handleEscape);
    return () => {
      document.removeEventListener('mousedown', handleClick);
      document.removeEventListener('keydown', handleEscape);
    };
  }, [contextMenuProjectId]);

  const handleRenameProject = useCallback(async (projectId: string, newName: string) => {
    const trimmed = newName.trim();
    if (!trimmed) {
      setRenamingProjectId(null);
      return;
    }
    try {
      await updateProject(projectId, { name: trimmed });
      setRenamingProjectId(null);
    } catch {
      // Error toast handled by store
    }
  }, [updateProject]);

  const handleDeleteProject = useCallback(async () => {
    if (!deleteConfirm.projectId) return;
    try {
      await deleteProject(deleteConfirm.projectId, deleteCascade);
      setDeleteConfirm({ open: false, projectId: '', projectName: '' });
      // If we were viewing this project, navigate away
      if (location.pathname === `/projects/${deleteConfirm.projectId}`) {
        navTo('/');
      }
    } catch {
      // Error toast handled by store
    }
  }, [deleteConfirm, deleteCascade, deleteProject, navTo, location.pathname]);

  const handleNewNoteInProject = useCallback(async (projectId: string) => {
    setContextMenuProjectId(null);
    try {
      const note = await createNoteInProject({ title: 'Untitled', body: '', project_id: projectId });
      navTo(`/notes/${note.id}`);
    } catch {
      addToast('Failed to create note', 'error');
    }
  }, [createNoteInProject, navTo, addToast]);

  const isActive = (path: string) => location.pathname === path;
  const isProjectActive = (id: string) =>
    location.pathname === `/projects/${id}`;

  const handleSettings = () => {
    navTo('/settings');
  };

  return (
    <>
    <nav
      className={`${styles.sidebar} ${collapsed ? styles.collapsed : ''} ${sidebarOpen ? styles.mobileOpen : ''}`}
      role="navigation"
      aria-label="Main navigation"
    >
      <div className={styles.top}>
        {/* Wordmark */}
        <button
          className={styles.wordmark}
          onClick={() => navTo('/')}
          title="Go to Inbox"
        >
          {collapsed ? 'S' : 'Seam'}
        </button>

        {/* Search */}
        {collapsed ? (
          <button
            className={styles.iconButton}
            onClick={() => navTo('/search')}
            title="Search notes"
            aria-label="Search notes"
          >
            <Search size={16} />
          </button>
        ) : (
          <form onSubmit={handleSearchSubmit} className={styles.searchForm}>
            <Search size={14} className={styles.searchIcon} />
            <input
              ref={searchRef}
              type="text"
              className={styles.searchInput}
              placeholder="Search notes..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              onKeyDown={handleSearchKeyDown}
              onFocus={() => searchResults.length > 0 && setShowDropdown(true)}
              aria-label="Search notes (press / to focus)"
              role="combobox"
              aria-expanded={showDropdown}
              aria-controls="sidebar-search-listbox"
              aria-activedescendant={
                showDropdown && selectedIndex >= 0
                  ? `search-result-${searchResults[selectedIndex]?.note_id}`
                  : undefined
              }
            />
            {!searchQuery && (
              <kbd className={styles.searchHint}>/</kbd>
            )}
            {showDropdown && (
              <div
                ref={dropdownRef}
                className={styles.searchDropdown}
                role="listbox"
                id="sidebar-search-listbox"
                aria-live="polite"
              >
                {searchResults.map((result, index) => (
                  <button
                    key={result.note_id}
                    id={`search-result-${result.note_id}`}
                    className={`${styles.searchResult} ${index === selectedIndex ? styles.searchResultSelected : ''}`}
                    onClick={() => {
                      setShowDropdown(false);
                      setSearchQuery('');
                      navTo(`/notes/${result.note_id}`);
                    }}
                    onMouseEnter={() => setSelectedIndex(index)}
                    role="option"
                    aria-selected={index === selectedIndex}
                  >
                    <span className={styles.searchResultTitle}>{result.title}</span>
                    <span
                      className={styles.searchResultSnippet}
                      dangerouslySetInnerHTML={{ __html: sanitizeHtml(result.snippet) }}
                    />
                  </button>
                ))}
              </div>
            )}
          </form>
        )}

        {/* Inbox */}
        <button
          className={`${styles.navItem} ${isActive('/') ? styles.active : ''}`}
          onClick={() => navTo('/')}
          title="Inbox"
        >
          <Inbox size={16} />
          <span className={styles.fadeLabel}>Inbox</span>
        </button>

        {/* Ask Seam */}
        <button
          className={`${styles.navItem} ${isActive('/ask') ? styles.active : ''}`}
          onClick={() => navTo('/ask')}
          title="Ask Seam"
        >
          <MessageCircle size={16} />
          <span className={styles.fadeLabel}>Ask Seam</span>
        </button>

        {/* Graph */}
        <button
          className={`${styles.navItem} ${isActive('/graph') ? styles.active : ''}`}
          onClick={() => navTo('/graph')}
          title="Knowledge Graph"
        >
          <Network size={16} />
          <span className={styles.fadeLabel}>Graph</span>
        </button>

        {/* Timeline */}
        <button
          className={`${styles.navItem} ${isActive('/timeline') ? styles.active : ''}`}
          onClick={() => navTo('/timeline')}
          title="Timeline"
        >
          <Calendar size={16} />
          <span className={styles.fadeLabel}>Timeline</span>
        </button>

        {/* Projects */}
        <div className={styles.section}>
          {!collapsed && (
            <div className={styles.sectionHeaderRow}>
              <button
                className={styles.sectionHeader}
                onClick={() => setProjectsExpanded(!projectsExpanded)}
              >
                Projects
              </button>
              <button
                className={styles.sectionAction}
                onClick={() => setShowNewProject(!showNewProject)}
                title="Create project"
                aria-label="Create project"
              >
                <Plus size={12} />
              </button>
            </div>
          )}
          {!collapsed && showNewProject && (
            <div className={styles.newProjectRow}>
              <input
                ref={newProjectRef}
                className={styles.newProjectInput}
                placeholder="Project name"
                value={newProjectName}
                onChange={(e) => setNewProjectName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') handleCreateProject();
                  if (e.key === 'Escape') {
                    setShowNewProject(false);
                    setNewProjectName('');
                  }
                }}
              />
              <button
                className={styles.newProjectConfirm}
                onClick={handleCreateProject}
                disabled={!newProjectName.trim()}
                aria-label="Confirm"
              >
                <Check size={12} />
              </button>
            </div>
          )}
          {(collapsed || projectsExpanded) &&
            projects.map((project, index) => (
              <div key={project.id} className={styles.projectRow} ref={contextMenuProjectId === project.id ? contextMenuRef : undefined}>
                {renamingProjectId === project.id ? (
                  <input
                    className={styles.renameInput}
                    value={renameValue}
                    onChange={(e) => setRenameValue(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') handleRenameProject(project.id, renameValue);
                      if (e.key === 'Escape') setRenamingProjectId(null);
                    }}
                    onBlur={() => handleRenameProject(project.id, renameValue)}
                    autoFocus
                  />
                ) : (
                  <button
                    className={`${styles.navItem} ${isProjectActive(project.id) ? styles.active : ''}`}
                    onClick={() => navTo(`/projects/${project.id}`)}
                    onContextMenu={(e) => {
                      if (collapsed) return;
                      e.preventDefault();
                      setContextMenuProjectId(project.id);
                    }}
                    title={project.name}
                    style={{
                      flex: 1,
                      ...(isProjectActive(project.id) ? { borderLeftColor: getProjectColor(index) } : undefined),
                    }}
                  >
                    <span
                      className={styles.projectDot}
                      style={{ backgroundColor: getProjectColor(index) }}
                    />
                    {!collapsed && (
                      <span className={styles.navLabel}>{project.name}</span>
                    )}
                  </button>
                )}
                {!collapsed && !renamingProjectId && (
                  <button
                    className={styles.projectMenuTrigger}
                    onClick={(e) => {
                      e.stopPropagation();
                      setContextMenuProjectId(contextMenuProjectId === project.id ? null : project.id);
                    }}
                    aria-label={`${project.name} options`}
                  >
                    <MoreHorizontal size={12} />
                  </button>
                )}
                {contextMenuProjectId === project.id && (
                  <div className={styles.contextMenu}>
                    <button
                      className={styles.contextMenuItem}
                      onClick={() => {
                        setContextMenuProjectId(null);
                        setRenamingProjectId(project.id);
                        setRenameValue(project.name);
                      }}
                    >
                      <Pencil size={12} />
                      Rename
                    </button>
                    <button
                      className={styles.contextMenuItem}
                      onClick={() => handleNewNoteInProject(project.id)}
                    >
                      <FilePlus size={12} />
                      New note
                    </button>
                    <div className={styles.contextMenuDivider} />
                    <button
                      className={styles.contextMenuItemDanger}
                      onClick={() => {
                        setContextMenuProjectId(null);
                        setDeleteConfirm({ open: true, projectId: project.id, projectName: project.name });
                        setDeleteCascade('inbox');
                      }}
                    >
                      <Trash2 size={12} />
                      Delete
                    </button>
                  </div>
                )}
              </div>
            ))}
        </div>

        {/* Tags */}
        {!collapsed && (
          <div className={styles.section}>
            <button
              className={styles.sectionHeader}
              onClick={() => setTagsExpanded(!tagsExpanded)}
            >
              Tags
            </button>
            {tagsExpanded &&
              tags.slice(0, 10).map((tag) => (
                <button
                  key={tag.name}
                  className={styles.navItem}
                  onClick={() => navTo(`/?tag=${encodeURIComponent(tag.name)}`)}
                  title={`${tag.name} (${tag.count})`}
                >
                  <Tag size={14} />
                  <span className={styles.navLabel}>#{tag.name}</span>
                  <span className={styles.count}>{tag.count}</span>
                </button>
              ))}
          </div>
        )}
      </div>

      <div className={styles.bottom}>
        <div className={styles.divider} />

        {/* User row */}
        <div className={styles.userRow}>
          <div className={styles.avatar}>
            {user?.username?.charAt(0).toUpperCase() ?? '?'}
          </div>
          {!collapsed && (
            <>
              <span className={styles.username}>{user?.username}</span>
              <button
                className={styles.iconButton}
                onClick={handleSettings}
                title="Settings"
                aria-label="Settings"
              >
                <Settings size={14} />
              </button>
            </>
          )}
        </div>

        {/* Capture button */}
        <button
          className={styles.captureButton}
          onClick={() => setCaptureModalOpen(true)}
          title="Quick capture"
          aria-label="Quick capture"
        >
          {collapsed ? <Plus size={16} /> : 'Capture'}
        </button>

        {/* Collapse toggle */}
        <button
          className={styles.collapseToggle}
          onClick={toggleSidebar}
          title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          {collapsed ? (
            <PanelLeftOpen size={16} />
          ) : (
            <PanelLeftClose size={16} />
          )}
        </button>
      </div>
    </nav>

    <ConfirmModal
      open={deleteConfirm.open}
      title={`Delete "${deleteConfirm.projectName}"`}
      message={
        deleteCascade === 'inbox'
          ? 'Notes in this project will be moved to Inbox.'
          : 'This project and all its notes will be permanently deleted.'
      }
      confirmLabel={deleteCascade === 'inbox' ? 'Keep notes' : 'Delete everything'}
      destructive={deleteCascade === 'delete'}
      onConfirm={handleDeleteProject}
      onCancel={() => setDeleteConfirm({ open: false, projectId: '', projectName: '' })}
    >
      <div style={{ display: 'flex', gap: 'var(--space-2)', marginBottom: 'var(--space-3)' }}>
        <button
          onClick={() => setDeleteCascade('inbox')}
          style={{
            padding: '4px 10px',
            borderRadius: 'var(--radius-sm)',
            fontSize: 'var(--font-size-xs)',
            fontFamily: 'var(--font-ui)',
            border: `1px solid ${deleteCascade === 'inbox' ? 'var(--accent-primary)' : 'var(--border-default)'}`,
            color: deleteCascade === 'inbox' ? 'var(--accent-primary)' : 'var(--text-secondary)',
            background: deleteCascade === 'inbox' ? 'var(--accent-muted)' : 'transparent',
          }}
        >
          Keep notes
        </button>
        <button
          onClick={() => setDeleteCascade('delete')}
          style={{
            padding: '4px 10px',
            borderRadius: 'var(--radius-sm)',
            fontSize: 'var(--font-size-xs)',
            fontFamily: 'var(--font-ui)',
            border: `1px solid ${deleteCascade === 'delete' ? 'var(--status-error)' : 'var(--border-default)'}`,
            color: deleteCascade === 'delete' ? 'var(--status-error)' : 'var(--text-secondary)',
            background: deleteCascade === 'delete' ? 'rgba(196,107,107,0.1)' : 'transparent',
          }}
        >
          Delete everything
        </button>
      </div>
    </ConfirmModal>
    </>
  );
}



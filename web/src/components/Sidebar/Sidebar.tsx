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
} from 'lucide-react';
import { useUIStore } from '../../stores/uiStore';
import { useProjectStore } from '../../stores/projectStore';
import { useAuthStore } from '../../stores/authStore';
import { useKeyboard } from '../../hooks/useKeyboard';
import { getProjectColor } from '../../lib/tagColor';
import { searchFTS } from '../../api/client';
import type { FTSResult } from '../../api/types';
import styles from './Sidebar.module.css';
import { useState, useRef, useCallback, useEffect } from 'react';

export function Sidebar() {
  const navigate = useNavigate();
  const location = useLocation();
  const collapsed = useUIStore((s) => s.sidebarCollapsed);
  const toggleSidebar = useUIStore((s) => s.toggleSidebar);
  const tags = useUIStore((s) => s.tags);
  const setCaptureModalOpen = useUIStore((s) => s.setCaptureModalOpen);
  const projects = useProjectStore((s) => s.projects);
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<FTSResult[]>([]);
  const [showDropdown, setShowDropdown] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(-1);
  const searchRef = useRef<HTMLInputElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const [projectsExpanded, setProjectsExpanded] = useState(true);
  const [tagsExpanded, setTagsExpanded] = useState(true);

  const focusSearch = useCallback(() => {
    searchRef.current?.focus();
  }, []);

  useKeyboard([
    { key: '/', global: true, handler: focusSearch },
  ]);

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
      navigate(`/search?q=${encodeURIComponent(searchQuery.trim())}`);
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
        navigate(`/notes/${result.note_id}`);
      }
    }
  };

  const isActive = (path: string) => location.pathname === path;
  const isProjectActive = (id: string) =>
    location.pathname === `/projects/${id}`;

  const handleLogout = async () => {
    await logout();
    navigate('/login');
  };

  return (
    <nav
      className={`${styles.sidebar} ${collapsed ? styles.collapsed : ''}`}
      role="navigation"
      aria-label="Main navigation"
    >
      <div className={styles.top}>
        {/* Wordmark */}
        <button
          className={styles.wordmark}
          onClick={() => navigate('/')}
          title="Go to Inbox"
        >
          {collapsed ? 'S' : 'Seam'}
        </button>

        {/* Search */}
        {collapsed ? (
          <button
            className={styles.iconButton}
            onClick={focusSearch}
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
              aria-label="Search notes"
            />
            {showDropdown && (
              <div ref={dropdownRef} className={styles.searchDropdown}>
                {searchResults.map((result, index) => (
                  <button
                    key={result.note_id}
                    className={`${styles.searchResult} ${index === selectedIndex ? styles.searchResultSelected : ''}`}
                    onClick={() => {
                      setShowDropdown(false);
                      setSearchQuery('');
                      navigate(`/notes/${result.note_id}`);
                    }}
                    onMouseEnter={() => setSelectedIndex(index)}
                  >
                    <span className={styles.searchResultTitle}>{result.title}</span>
                    <span
                      className={styles.searchResultSnippet}
                      dangerouslySetInnerHTML={{ __html: result.snippet }}
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
          onClick={() => navigate('/')}
          title="Inbox"
        >
          <Inbox size={16} />
          {!collapsed && <span>Inbox</span>}
        </button>

        {/* Ask Seam */}
        <button
          className={`${styles.navItem} ${isActive('/ask') ? styles.active : ''}`}
          onClick={() => navigate('/ask')}
          title="Ask Seam"
        >
          <MessageCircle size={16} />
          {!collapsed && <span>Ask Seam</span>}
        </button>

        {/* Projects */}
        <div className={styles.section}>
          {!collapsed && (
            <button
              className={styles.sectionHeader}
              onClick={() => setProjectsExpanded(!projectsExpanded)}
            >
              Projects
            </button>
          )}
          {(collapsed || projectsExpanded) &&
            projects.map((project, index) => (
              <button
                key={project.id}
                className={`${styles.navItem} ${isProjectActive(project.id) ? styles.active : ''}`}
                onClick={() => navigate(`/projects/${project.id}`)}
                title={project.name}
                style={
                  isProjectActive(project.id)
                    ? { borderLeftColor: getProjectColor(index) }
                    : undefined
                }
              >
                <span
                  className={styles.projectDot}
                  style={{ backgroundColor: getProjectColor(index) }}
                />
                {!collapsed && (
                  <span className={styles.navLabel}>{project.name}</span>
                )}
              </button>
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
                  onClick={() => navigate(`/?tag=${encodeURIComponent(tag.name)}`)}
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
                onClick={handleLogout}
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
  );
}



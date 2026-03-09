import { useEffect, useState, useCallback, useRef, useMemo } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import CodeMirror, { type ReactCodeMirrorRef } from '@uiw/react-codemirror';
import { markdown } from '@codemirror/lang-markdown';
import { AnimatePresence, motion } from 'motion/react';
import { formatDistanceToNow } from 'date-fns';
  import {
  Bold,
  Italic,
  Heading,
  Link,
  Link2,
  Code,
  List,
  ListChecks,
  PenLine,
  Columns2,
  Eye,
  PanelRight,
  Trash2,
  MoreHorizontal,
  Check,
  X,
  Sparkles,
  Copy,
  ExternalLink,
  Files,
  Download,
  FolderInput,
} from 'lucide-react';
import { useNoteStore } from '../../stores/noteStore';
import { useProjectStore } from '../../stores/projectStore';
import { useUIStore } from '../../stores/uiStore';
import { useToastStore } from '../../components/Toast/ToastContainer';
import { useWebSocket } from '../../hooks/useWebSocket';
import { getRelatedNotes, aiAssist, getTwoHopBacklinks } from '../../api/client';
import { renderMarkdown } from '../../lib/markdown';
import { sanitizeHtml } from '../../lib/sanitize';
import { timeAgo, formatDateTime } from '../../lib/dates';
import { saveDraft, getDraft, clearDraft } from '../../lib/drafts';
import { getTagColor } from '../../lib/tagColor';
import { seamEditorTheme } from './editorTheme';
import {
  wikilinkDecorationPlugin,
  wikilinkDecorationTheme,
  wikilinkAutocomplete,
} from './wikilinkExtension';
import { EditorSkeleton } from '../../components/Skeleton/Skeleton';
import { ConfirmModal } from '../../components/ConfirmModal/ConfirmModal';
import type { AIAssistReq, LinkSuggestion, RelatedNote, TwoHopBacklink, WSMessage, TagCount } from '../../api/types';
import styles from './NoteEditorPage.module.css';

export function NoteEditorPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const currentNote = useNoteStore((s) => s.currentNote);
  const backlinks = useNoteStore((s) => s.backlinks);
  const fetchNote = useNoteStore((s) => s.fetchNote);
  const updateNote = useNoteStore((s) => s.updateNote);
  const deleteNote = useNoteStore((s) => s.deleteNote);
  const fetchBacklinks = useNoteStore((s) => s.fetchBacklinks);
  const clearCurrentNote = useNoteStore((s) => s.clearCurrentNote);
  const projects = useProjectStore((s) => s.projects);
  const rightPanelOpen = useUIStore((s) => s.rightPanelOpen);
  const toggleRightPanel = useUIStore((s) => s.toggleRightPanel);
  const viewMode = useUIStore((s) => s.editorViewMode);
  const setViewMode = useUIStore((s) => s.setEditorViewMode);
  const [title, setTitle] = useState('');
  const [content, setContent] = useState('');
  const [noteLoading, setNoteLoading] = useState(true);
  const [saveStatus, setSaveStatus] = useState<'saved' | 'saving' | 'unsaved'>('saved');
  const [showMenu, setShowMenu] = useState(false);
  const [linkSuggestions, setLinkSuggestions] = useState<LinkSuggestion[]>([]);
  const [relatedNotes, setRelatedNotes] = useState<RelatedNote[]>([]);
  const [twoHopBacklinks, setTwoHopBacklinks] = useState<TwoHopBacklink[]>([]);
  const [isOrphan, setIsOrphan] = useState(false);
  const [showAIMenu, setShowAIMenu] = useState(false);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const aiMenuRef = useRef<HTMLDivElement>(null);
  const addToast = useToastStore((s) => s.addToast);
  const [aiLoading, setAILoading] = useState(false);
  const [aiResult, setAIResult] = useState<{ action: string; text: string } | null>(null);
  const [draftBanner, setDraftBanner] = useState<{ title: string; body: string; savedAt: number } | null>(null);
  const saveTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const titleTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const contentRef = useRef('');
  const titleRef = useRef('');
  const titleInputRef = useRef<HTMLInputElement>(null);
  const editorRef = useRef<ReactCodeMirrorRef>(null);
  const previewRef = useRef<HTMLDivElement>(null);

  const hasUnsavedChanges = saveStatus === 'unsaved' || saveStatus === 'saving';

  // Warn user before leaving with unsaved changes.
  useEffect(() => {
    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = '';
    };

    if (hasUnsavedChanges) {
      window.addEventListener('beforeunload', handler);
    }

    return () => window.removeEventListener('beforeunload', handler);
  }, [hasUnsavedChanges]);

  useEffect(() => {
    let aborted = false;
    if (id) {
      // Clear previous content and show loading when switching notes.
      setNoteLoading(true);
      setContent('');
      contentRef.current = '';

      fetchNote(id).finally(() => {
        if (!aborted) setNoteLoading(false);
      });
      fetchBacklinks(id);
      // Fetch related notes (semantic similarity).
      getRelatedNotes(id).then((data) => {
        if (!aborted) setRelatedNotes(data);
      }).catch(() => {
        if (!aborted) setRelatedNotes([]);
      });
      // Fetch two-hop backlinks.
      getTwoHopBacklinks(id).then((data) => {
        if (!aborted) setTwoHopBacklinks(data);
      }).catch(() => {
        if (!aborted) setTwoHopBacklinks([]);
      });
      // Orphan status is computed from backlinks and content below.
    }
    return () => {
      aborted = true;
      clearCurrentNote();
      setLinkSuggestions([]);
      setRelatedNotes([]);
      setTwoHopBacklinks([]);
      setIsOrphan(false);
    };
  }, [id, fetchNote, fetchBacklinks, clearCurrentNote]);

  // Check orphan status from already-available data.
  // A note is an orphan if it has no backlinks and no outgoing wikilinks.
  useEffect(() => {
    const hasBacklinks = backlinks.length > 0;
    const hasOutlinks = /\[\[.+?\]\]/.test(content);
    setIsOrphan(!hasBacklinks && !hasOutlinks);
  }, [backlinks, content]);

  // Listen for auto-link suggestions via WebSocket.
  const handleWSMessage = useCallback(
    (msg: WSMessage) => {
      if (msg.type === 'note.link_suggestions') {
        const payload = msg.payload as {
          note_id: string;
          suggestions: LinkSuggestion[];
        };
        if (payload.note_id === id && payload.suggestions?.length > 0) {
          setLinkSuggestions(payload.suggestions);
        }
      }
    },
    [id],
  );
  useWebSocket(handleWSMessage);

  useEffect(() => {
    if (currentNote) {
      setContent(currentNote.body);
      contentRef.current = currentNote.body;
      setTitle(currentNote.title);
      titleRef.current = currentNote.title;
      setSaveStatus('saved');
      // Auto-focus and select title when it's "Untitled"
      if (currentNote.title === 'Untitled') {
        setTimeout(() => {
          titleInputRef.current?.focus();
          titleInputRef.current?.select();
        }, 100);
      }
    }
  }, [currentNote?.id]); // Only reset content on note change, not on every update

  // Check for a local draft that is newer than the server version.
  useEffect(() => {
    if (currentNote) {
      const draft = getDraft(currentNote.id);
      if (draft) {
        const noteUpdatedAt = new Date(currentNote.updated_at).getTime();
        if (draft.savedAt > noteUpdatedAt) {
          setDraftBanner(draft);
        } else {
          clearDraft(currentNote.id);
        }
      }
    }
  }, [currentNote?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleSave = useCallback(async (value: string) => {
    if (!id) return;
    setSaveStatus('saving');
    try {
      await updateNote(id, { body: value });
      setSaveStatus('saved');
      clearDraft(id);
    } catch {
      setSaveStatus('unsaved');
      addToast('Failed to save note', 'error');
    }
  }, [id, updateNote, addToast]);

  const handleTitleChange = useCallback((newTitle: string) => {
    setTitle(newTitle);
    titleRef.current = newTitle;
    setSaveStatus('unsaved');
    if (id) saveDraft(id, newTitle, contentRef.current);

    if (titleTimerRef.current) {
      clearTimeout(titleTimerRef.current);
    }
    titleTimerRef.current = setTimeout(async () => {
      if (!id) return;
      setSaveStatus('saving');
      try {
        await updateNote(id, { title: newTitle });
        setSaveStatus('saved');
        clearDraft(id);
      } catch {
        setSaveStatus('unsaved');
        addToast('Failed to save title', 'error');
      }
    }, 1000);
  }, [id, updateNote, addToast]);

  const handleChange = useCallback((value: string) => {
    setContent(value);
    contentRef.current = value;
    setSaveStatus('unsaved');
    if (id) saveDraft(id, titleRef.current, value);

    if (saveTimerRef.current) {
      clearTimeout(saveTimerRef.current);
    }
    saveTimerRef.current = setTimeout(() => {
      handleSave(value);
    }, 1000);
  }, [id, handleSave]);

  // Cleanup save timers on unmount -- flush any pending saves.
  useEffect(() => {
    return () => {
      if (saveTimerRef.current) {
        clearTimeout(saveTimerRef.current);
        // Flush the pending save with the latest content.
        handleSave(contentRef.current);
      }
      if (titleTimerRef.current) {
        clearTimeout(titleTimerRef.current);
        // Flush the pending title save.
        if (id && titleRef.current !== currentNote?.title) {
          updateNote(id, { title: titleRef.current });
        }
      }
    };
  }, [handleSave, id, updateNote, currentNote?.title]);

  const handleDeleteClick = () => {
    setShowMenu(false);
    setShowDeleteConfirm(true);
  };

  const handleDeleteConfirm = async () => {
    if (!id) return;
    setShowDeleteConfirm(false);
    try {
      await deleteNote(id);
      navigate('/');
    } catch {
      addToast('Failed to delete note', 'error');
    }
  };

  const handleDuplicate = useCallback(async () => {
    if (!currentNote) return;
    setShowMenu(false);
    try {
      const { createNote } = useNoteStore.getState();
      const note = await createNote({
        title: `${currentNote.title} (copy)`,
        body: currentNote.body,
        project_id: currentNote.project_id,
        tags: currentNote.tags,
      });
      navigate(`/notes/${note.id}`);
    } catch {
      addToast('Failed to duplicate note', 'error');
    }
  }, [currentNote, navigate, addToast]);

  const handleCopyLink = useCallback(() => {
    if (!id) return;
    setShowMenu(false);
    navigator.clipboard.writeText(`${window.location.origin}/notes/${id}`);
    addToast('Link copied to clipboard', 'success');
  }, [id, addToast]);

  const handleExportMarkdown = useCallback(() => {
    if (!currentNote) return;
    setShowMenu(false);
    const blob = new Blob([currentNote.body], { type: 'text/markdown' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${currentNote.title.replace(/[/\\]/g, '_')}.md`;
    a.click();
    URL.revokeObjectURL(url);
  }, [currentNote]);

  const handleOpenInNewTab = useCallback(() => {
    if (!id) return;
    setShowMenu(false);
    window.open(`/notes/${id}`, '_blank');
  }, [id]);

  const handleAcceptLink = useCallback(
    (targetTitle: string) => {
      const view = editorRef.current?.view;
      const wikilink = `[[${targetTitle}]]`;
      if (view) {
        const { to } = view.state.selection.main;
        // Insert at cursor position.
        const insertText = to > 0 ? ` ${wikilink}` : wikilink;
        view.dispatch({
          changes: { from: to, to, insert: insertText },
        });
        view.focus();
      } else {
        // Fallback: append to end.
        const newContent = content + `\n${wikilink}`;
        setContent(newContent);
        handleSave(newContent);
      }
      // Remove this suggestion from the list.
      setLinkSuggestions((prev) =>
        prev.filter((s) => s.target_title !== targetTitle),
      );
    },
    [content, handleSave],
  );

  const handleDismissSuggestion = useCallback((targetTitle: string) => {
    setLinkSuggestions((prev) =>
      prev.filter((s) => s.target_title !== targetTitle),
    );
  }, []);

  const handleProjectChange = useCallback(async (newProjectId: string) => {
    if (!id) return;
    try {
      await updateNote(id, { project_id: newProjectId || '' });
    } catch {
      addToast('Failed to move note', 'error');
    }
  }, [id, updateNote, addToast]);

  // -- Tag editing --
  const allTags = useUIStore((s) => s.tags);
  const fetchTags = useUIStore((s) => s.fetchTags);
  const [tagInput, setTagInput] = useState('');
  const [tagSuggestions, setTagSuggestions] = useState<TagCount[]>([]);
  const [tagSuggestionIndex, setTagSuggestionIndex] = useState(-1);
  const tagTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const saveTagsDebounced = useCallback((newTags: string[]) => {
    if (!id) return;
    if (tagTimerRef.current) clearTimeout(tagTimerRef.current);
    tagTimerRef.current = setTimeout(async () => {
      try {
        await updateNote(id, { tags: newTags });
        fetchTags();
      } catch {
        addToast('Failed to update tags', 'error');
      }
    }, 500);
  }, [id, updateNote, fetchTags, addToast]);

  const handleAddTag = useCallback((tagName: string) => {
    const cleaned = tagName.trim().replace(/^#/, '').toLowerCase();
    if (!cleaned || !currentNote) return;
    const existing = currentNote.tags ?? [];
    if (existing.includes(cleaned)) {
      setTagInput('');
      return;
    }
    const newTags = [...existing, cleaned];
    saveTagsDebounced(newTags);
    setTagInput('');
    setTagSuggestions([]);
    setTagSuggestionIndex(-1);
  }, [currentNote, saveTagsDebounced]);

  const handleRemoveTag = useCallback((tagName: string) => {
    if (!currentNote) return;
    const existing = currentNote.tags ?? [];
    const newTags = existing.filter((t) => t !== tagName);
    saveTagsDebounced(newTags);
  }, [currentNote, saveTagsDebounced]);

  const handleTagInputChange = useCallback((value: string) => {
    setTagInput(value);
    setTagSuggestionIndex(-1);
    if (!value.trim()) {
      setTagSuggestions([]);
      return;
    }
    const query = value.trim().toLowerCase().replace(/^#/, '');
    const existing = new Set(currentNote?.tags ?? []);
    const matches = allTags
      .filter((t) => t.name.toLowerCase().includes(query) && !existing.has(t.name))
      .slice(0, 8);
    setTagSuggestions(matches);
  }, [allTags, currentNote?.tags]);

  const handleTagInputKeyDown = useCallback((e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      if (tagSuggestionIndex >= 0 && tagSuggestionIndex < tagSuggestions.length) {
        handleAddTag(tagSuggestions[tagSuggestionIndex].name);
      } else if (tagInput.trim()) {
        handleAddTag(tagInput);
      }
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      setTagSuggestionIndex((i) => Math.min(i + 1, tagSuggestions.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setTagSuggestionIndex((i) => Math.max(i - 1, -1));
    } else if (e.key === 'Escape') {
      setTagSuggestions([]);
      setTagSuggestionIndex(-1);
    }
  }, [tagInput, tagSuggestions, tagSuggestionIndex, handleAddTag]);

  // Cleanup tag timer on unmount
  useEffect(() => {
    return () => {
      if (tagTimerRef.current) clearTimeout(tagTimerRef.current);
    };
  }, []);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if ((e.metaKey || e.ctrlKey) && e.key === 's') {
      e.preventDefault();
      handleSave(content);
    }
  }, [content, handleSave]);

  // Toolbar formatting functions that interact with the CodeMirror editor.
  const wrapSelection = useCallback((prefix: string, suffix: string) => {
    const view = editorRef.current?.view;
    if (!view) return;
    const { from, to } = view.state.selection.main;
    const selected = view.state.sliceDoc(from, to);
    const replacement = `${prefix}${selected || 'text'}${suffix}`;
    view.dispatch({
      changes: { from, to, insert: replacement },
      selection: {
        anchor: from + prefix.length,
        head: from + prefix.length + (selected ? selected.length : 4),
      },
    });
    view.focus();
  }, []);

  const insertAtLineStart = useCallback((prefix: string) => {
    const view = editorRef.current?.view;
    if (!view) return;
    const { from } = view.state.selection.main;
    const line = view.state.doc.lineAt(from);
    view.dispatch({
      changes: { from: line.from, to: line.from, insert: prefix },
    });
    view.focus();
  }, []);

  const handleBold = useCallback(() => wrapSelection('**', '**'), [wrapSelection]);
  const handleItalic = useCallback(() => wrapSelection('*', '*'), [wrapSelection]);
  const handleHeading = useCallback(() => insertAtLineStart('## '), [insertAtLineStart]);
  const handleLink = useCallback(() => {
    const view = editorRef.current?.view;
    if (!view) return;
    const { from, to } = view.state.selection.main;
    const selected = view.state.sliceDoc(from, to);
    const replacement = `[${selected || 'text'}](url)`;
    view.dispatch({
      changes: { from, to, insert: replacement },
      selection: { anchor: from + 1, head: from + 1 + (selected ? selected.length : 4) },
    });
    view.focus();
  }, []);
  const handleWikilink = useCallback(() => wrapSelection('[[', ']]'), [wrapSelection]);
  const handleCode = useCallback(() => wrapSelection('`', '`'), [wrapSelection]);
  const handleList = useCallback(() => insertAtLineStart('- '), [insertAtLineStart]);
  const handleChecklist = useCallback(() => insertAtLineStart('- [ ] '), [insertAtLineStart]);

  const getSelectedText = useCallback((): string => {
    const view = editorRef.current?.view;
    if (!view) return '';
    const { from, to } = view.state.selection.main;
    if (from === to) return '';
    return view.state.sliceDoc(from, to);
  }, []);

  const handleAIAssist = useCallback(async (action: AIAssistReq['action']) => {
    if (!id) return;
    setShowAIMenu(false);
    setAILoading(true);
    setAIResult(null);
    try {
      const selection = getSelectedText();
      const result = await aiAssist(id, action, selection || undefined);
      setAIResult({ action, text: result.result });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'AI assist request failed';
      addToast(message, 'error');
    } finally {
      setAILoading(false);
    }
  }, [id, getSelectedText, addToast]);

  const handleInsertAIResult = useCallback(() => {
    if (!aiResult) return;
    const view = editorRef.current?.view;
    if (view) {
      const { from, to } = view.state.selection.main;
      // If there was a selection, replace it; otherwise append at cursor.
      const insert = from === to
        ? `\n\n${aiResult.text}`
        : aiResult.text;
      view.dispatch({
        changes: { from, to, insert },
      });
      view.focus();
    } else {
      // Fallback: append to content.
      const newContent = content + `\n\n${aiResult.text}`;
      setContent(newContent);
      handleSave(newContent);
    }
    setAIResult(null);
  }, [aiResult, content, handleSave]);

  const handleDismissAIResult = useCallback(() => {
    setAIResult(null);
  }, []);

  // Close dropdown menus on outside click or Escape key.
  useEffect(() => {
    if (!showMenu && !showAIMenu) return;

    const handleClickOutside = (e: MouseEvent) => {
      if (showMenu && menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setShowMenu(false);
      }
      if (showAIMenu && aiMenuRef.current && !aiMenuRef.current.contains(e.target as Node)) {
        setShowAIMenu(false);
      }
    };

    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setShowMenu(false);
        setShowAIMenu(false);
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    document.addEventListener('keydown', handleEscape);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      document.removeEventListener('keydown', handleEscape);
    };
  }, [showMenu, showAIMenu]);

  // Handle wikilink clicks in the preview pane.
  useEffect(() => {
    const container = previewRef.current;
    if (!container) return;

    const handleClick = (e: MouseEvent) => {
      const anchor = (e.target as HTMLElement).closest('a[data-wikilink]');
      if (!anchor) return;
      e.preventDefault();
      const target = anchor.getAttribute('data-wikilink');
      if (target) {
        navigate(`/search?q=${encodeURIComponent(target)}`);
      }
    };

    container.addEventListener('click', handleClick);
    return () => {
      container.removeEventListener('click', handleClick);
    };
  }, [navigate, viewMode]);

  const handleRestoreDraft = useCallback(() => {
    if (!draftBanner || !id) return;
    setTitle(draftBanner.title);
    titleRef.current = draftBanner.title;
    setContent(draftBanner.body);
    contentRef.current = draftBanner.body;
    setSaveStatus('unsaved');
    setDraftBanner(null);
  }, [draftBanner, id]);

  const handleDiscardDraft = useCallback(() => {
    if (id) clearDraft(id);
    setDraftBanner(null);
  }, [id]);

  const wordStats = useMemo(() => {
    const chars = content.length;
    const words = content.trim() ? content.trim().split(/\s+/).length : 0;
    const readingMinutes = Math.max(1, Math.ceil(words / 238));
    return { words, chars, readingMinutes };
  }, [content]);

  const renderedHtml = useMemo(
    () => (viewMode === 'editor' ? '' : sanitizeHtml(renderMarkdown(content))),
    [content, viewMode],
  );

  const cmExtensions = useMemo(
    () => [markdown(), wikilinkDecorationPlugin, wikilinkDecorationTheme, wikilinkAutocomplete],
    [],
  );

  return (
    <div className={styles.page} onKeyDown={handleKeyDown}>
      {/* Toolbar */}
      <div className={styles.toolbar}>
        <div className={styles.toolbarLeft}>
          <button className={styles.toolButton} title="Bold (Cmd+B)" aria-label="Bold" onClick={handleBold}>
            <Bold size={16} />
          </button>
          <button className={styles.toolButton} title="Italic (Cmd+I)" aria-label="Italic" onClick={handleItalic}>
            <Italic size={16} />
          </button>
          <button className={styles.toolButton} title="Heading" aria-label="Heading" onClick={handleHeading}>
            <Heading size={16} />
          </button>
          <button className={styles.toolButton} title="Link" aria-label="Link" onClick={handleLink}>
            <Link size={16} />
          </button>
          <button className={styles.toolButton} title="Wikilink" aria-label="Wikilink" onClick={handleWikilink}>
            <Link2 size={16} />
          </button>
          <button className={styles.toolButton} title="Code" aria-label="Code" onClick={handleCode}>
            <Code size={16} />
          </button>
          <button className={styles.toolButton} title="List" aria-label="List" onClick={handleList}>
            <List size={16} />
          </button>
          <button className={styles.toolButton} title="Checklist" aria-label="Checklist" onClick={handleChecklist}>
            <ListChecks size={16} />
          </button>

          <div className={styles.toolbarSeparator} />

          <div className={styles.menuContainer} ref={aiMenuRef}>
            <button
              className={`${styles.toolButton} ${aiLoading ? styles.activeView : ''}`}
              onClick={() => setShowAIMenu(!showAIMenu)}
              title="AI Assist"
              aria-label="AI Assist"
              aria-expanded={showAIMenu}
              aria-haspopup="menu"
              disabled={aiLoading}
            >
              <Sparkles size={16} />
            </button>
            {showAIMenu && (
              <div className={styles.menu} role="menu">
                <button
                  className={styles.menuItemDefault}
                  role="menuitem"
                  onClick={() => handleAIAssist('expand')}
                >
                  <Sparkles size={14} />
                  Expand
                </button>
                <button
                  className={styles.menuItemDefault}
                  role="menuitem"
                  onClick={() => handleAIAssist('summarize')}
                >
                  <Sparkles size={14} />
                  Summarize
                </button>
                <button
                  className={styles.menuItemDefault}
                  role="menuitem"
                  onClick={() => handleAIAssist('extract-actions')}
                >
                  <ListChecks size={14} />
                  Extract Actions
                </button>
              </div>
            )}
          </div>
        </div>

        <div className={styles.toolbarRight}>
          <button
            className={`${styles.toolButton} ${viewMode === 'editor' ? styles.activeView : ''}`}
            onClick={() => setViewMode('editor')}
            title="Editor only"
            aria-label="Editor only"
          >
            <PenLine size={16} />
          </button>
          <button
            className={`${styles.toolButton} ${viewMode === 'split' ? styles.activeView : ''}`}
            onClick={() => setViewMode('split')}
            title="Split view"
            aria-label="Split view"
          >
            <Columns2 size={16} />
          </button>
          <button
            className={`${styles.toolButton} ${viewMode === 'preview' ? styles.activeView : ''}`}
            onClick={() => setViewMode('preview')}
            title="Preview only"
            aria-label="Preview only"
          >
            <Eye size={16} />
          </button>

          <div className={styles.toolbarSeparator} />

          <button
            className={styles.toolButton}
            onClick={toggleRightPanel}
            title="Toggle right panel"
            aria-label="Toggle right panel"
          >
            <PanelRight size={16} />
          </button>

          <div className={styles.menuContainer} ref={menuRef}>
            <button
              className={styles.toolButton}
              onClick={() => setShowMenu(!showMenu)}
              title="More options"
              aria-label="More options"
              aria-expanded={showMenu}
              aria-haspopup="menu"
            >
              <MoreHorizontal size={16} />
            </button>
            {showMenu && (
              <div className={styles.menu} role="menu">
                <button className={styles.menuItemDefault} role="menuitem" onClick={handleDuplicate}>
                  <Files size={14} />
                  Duplicate note
                </button>
                <button className={styles.menuItemDefault} role="menuitem" onClick={handleCopyLink}>
                  <Copy size={14} />
                  Copy link
                </button>
                <button className={styles.menuItemDefault} role="menuitem" onClick={handleExportMarkdown}>
                  <Download size={14} />
                  Export as Markdown
                </button>
                <button className={styles.menuItemDefault} role="menuitem" onClick={handleOpenInNewTab}>
                  <ExternalLink size={14} />
                  Open in new tab
                </button>
                <div className={styles.menuDivider} />
                <div className={styles.menuSubmenu}>
                  <div className={styles.menuSubmenuLabel}>
                    <FolderInput size={14} />
                    Move to project
                  </div>
                  <button
                    className={styles.menuItemDefault}
                    role="menuitem"
                    onClick={() => { handleProjectChange(''); setShowMenu(false); }}
                  >
                    Inbox
                  </button>
                  {projects.map((p) => (
                    <button
                      key={p.id}
                      className={`${styles.menuItemDefault} ${currentNote?.project_id === p.id ? styles.menuItemActive : ''}`}
                      role="menuitem"
                      onClick={() => { handleProjectChange(p.id); setShowMenu(false); }}
                    >
                      {p.name}
                    </button>
                  ))}
                </div>
                <div className={styles.menuDivider} />
                <button className={styles.menuItem} role="menuitem" onClick={handleDeleteClick}>
                  <Trash2 size={14} />
                  Delete note
                </button>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Content area */}
      <div className={styles.contentArea}>
        {noteLoading ? (
          <EditorSkeleton />
        ) : (<>
        {draftBanner && (
          <div className={styles.draftBanner}>
            <span className={styles.draftBannerText}>
              Local draft found (saved{' '}
              {formatDistanceToNow(new Date(draftBanner.savedAt), { addSuffix: true })})
            </span>
            <div className={styles.draftBannerActions}>
              <button className={styles.draftRestore} onClick={handleRestoreDraft}>
                Restore
              </button>
              <button className={styles.draftDiscard} onClick={handleDiscardDraft}>
                Discard
              </button>
            </div>
          </div>
        )}
        <div className={styles.editorWrapper}>
          {/* Editor pane */}
          {viewMode !== 'preview' && (
            <div
              className={styles.editorPane}
              style={{ flex: viewMode === 'split' ? '1' : undefined }}
            >
              <input
                ref={titleInputRef}
                type="text"
                className={styles.titleInput}
                value={title}
                onChange={(e) => handleTitleChange(e.target.value)}
                placeholder="Untitled"
                aria-label="Note title"
              />
              <CodeMirror
                ref={editorRef}
                value={content}
                onChange={handleChange}
                extensions={cmExtensions}
                theme={seamEditorTheme}
                basicSetup={{
                  lineNumbers: true,
                  highlightActiveLine: true,
                  foldGutter: false,
                }}
                className={styles.codeMirror}
              />
              <div className={styles.wordCountBar}>
                <span>{wordStats.words.toLocaleString()} words</span>
                <span className={styles.wordCountSeparator}>/</span>
                <span>{wordStats.chars.toLocaleString()} chars</span>
                <span className={styles.wordCountSeparator}>/</span>
                <span>{wordStats.readingMinutes} min read</span>
              </div>
            </div>
          )}

          {/* Preview pane */}
          {viewMode !== 'editor' && (
            <div
              ref={previewRef}
              className={styles.previewPane}
              style={{ flex: viewMode === 'split' ? '1' : undefined }}
            >
              <div className={styles.previewContent}>
                <h1 className={styles.previewTitle}>{title || 'Untitled'}</h1>
                <div
                  className={styles.renderedMarkdown}
                  dangerouslySetInnerHTML={{ __html: renderedHtml }}
                />
              </div>
            </div>
          )}
        </div>

        {/* Right panel */}
        <AnimatePresence>
        {rightPanelOpen && (
          <motion.aside
            className={styles.rightPanel}
            initial={{ x: '100%', opacity: 0 }}
            animate={{ x: 0, opacity: 1 }}
            exit={{ x: '100%', opacity: 0 }}
            transition={{ duration: 0.2, ease: [0, 0, 0.2, 1] }}
          >
            {/* Link suggestions */}
            {linkSuggestions.length > 0 && (
              <section className={styles.panelSection}>
                <h3 className={styles.panelSectionTitle}>
                  <Sparkles size={12} />
                  Suggested Links
                </h3>
                {linkSuggestions.map((suggestion) => (
                  <div key={suggestion.target_note_id} className={styles.suggestionItem}>
                    <div className={styles.suggestionHeader}>
                      <span className={styles.suggestionTitle}>
                        {suggestion.target_title}
                      </span>
                      <button
                        className={styles.suggestionDismiss}
                        onClick={() => handleDismissSuggestion(suggestion.target_title)}
                        aria-label="Dismiss suggestion"
                      >
                        <X size={12} />
                      </button>
                    </div>
                    <p className={styles.suggestionReason}>{suggestion.reason}</p>
                    <button
                      className={styles.suggestionAccept}
                      onClick={() => handleAcceptLink(suggestion.target_title)}
                    >
                      Link
                    </button>
                  </div>
                ))}
              </section>
            )}

            {/* AI Assist result */}
            {(aiLoading || aiResult) && (
              <section className={styles.panelSection}>
                <h3 className={styles.panelSectionTitle}>
                  <Sparkles size={12} />
                  AI Assist
                </h3>
                {aiLoading && (
                  <p className={styles.panelEmpty}>Generating...</p>
                )}
                {aiResult && (
                  <div className={styles.aiResultBlock}>
                    <p className={styles.aiResultLabel}>
                      {aiResult.action === 'expand' && 'Expanded text'}
                      {aiResult.action === 'summarize' && 'Summary'}
                      {aiResult.action === 'extract-actions' && 'Action items'}
                    </p>
                    <div className={styles.aiResultContent}>
                      <div
                        className={styles.renderedMarkdownSmall}
                        dangerouslySetInnerHTML={{ __html: sanitizeHtml(renderMarkdown(aiResult.text)) }}
                      />
                    </div>
                    <div className={styles.aiResultActions}>
                      <button
                        className={styles.suggestionAccept}
                        onClick={handleInsertAIResult}
                      >
                        Insert
                      </button>
                      <button
                        className={styles.suggestionDismiss}
                        onClick={handleDismissAIResult}
                        aria-label="Dismiss"
                        style={{ width: 'auto', height: 'auto', padding: '2px 8px' }}
                      >
                        Dismiss
                      </button>
                    </div>
                  </div>
                )}
              </section>
            )}

            {/* Related notes */}
            <section className={styles.panelSection}>
              <h3 className={styles.panelSectionTitle}>Related</h3>
              {relatedNotes.length === 0 ? (
                <p className={styles.panelEmpty}>No related notes</p>
              ) : (
                relatedNotes.map((note, i) => (
                  <motion.div
                    key={note.note_id}
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.25, delay: i * 0.03, ease: [0.16, 1, 0.3, 1] }}
                  >
                    <button
                      className={styles.backlinkItem}
                      onClick={() => navigate(`/notes/${note.note_id}`)}
                    >
                      <span className={styles.backlinkTitle}>{note.title}</span>
                      <span className={styles.backlinkMeta}>
                        {Math.round(note.score * 100)}% similar
                      </span>
                    </button>
                  </motion.div>
                ))
              )}
            </section>

            {/* Backlinks */}
            <section className={styles.panelSection}>
              <h3 className={styles.panelSectionTitle}>Backlinks</h3>
              {backlinks.length === 0 ? (
                <p className={styles.panelEmpty}>No backlinks</p>
              ) : (
                backlinks.map((note, i) => (
                  <motion.div
                    key={note.id}
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.25, delay: i * 0.03, ease: [0.16, 1, 0.3, 1] }}
                  >
                    <button
                      className={styles.backlinkItem}
                      onClick={() => navigate(`/notes/${note.id}`)}
                    >
                      <span className={styles.backlinkTitle}>{note.title}</span>
                      <span className={styles.backlinkMeta}>
                        {timeAgo(note.updated_at)}
                      </span>
                    </button>
                  </motion.div>
                ))
              )}
            </section>

            {/* Orphan indicator */}
            {isOrphan && (
              <section className={styles.panelSection}>
                <div className={styles.orphanBadge}>
                  Orphan note -- no links in or out
                </div>
              </section>
            )}

            {/* Two-hop backlinks */}
            {twoHopBacklinks.length > 0 && (
              <section className={styles.panelSection}>
                <h3 className={styles.panelSectionTitle}>2-hop Backlinks</h3>
                {twoHopBacklinks.map((note) => (
                  <div key={note.id} className={styles.twoHopItem}>
                    <button
                      className={styles.backlinkItem}
                      onClick={() => navigate(`/notes/${note.id}`)}
                    >
                      <span className={styles.backlinkTitle}>{note.title}</span>
                    </button>
                    <span className={styles.twoHopVia}>
                      via{' '}
                      <button
                        className={styles.twoHopViaLink}
                        onClick={() => navigate(`/notes/${note.via_id}`)}
                      >
                        {note.via_title}
                      </button>
                    </span>
                  </div>
                ))}
              </section>
            )}

            {/* Tags */}
            <section className={styles.panelSection}>
              <h3 className={styles.panelSectionTitle}>Tags</h3>
              <div className={styles.tagList}>
                {currentNote?.tags?.map((tag) => (
                  <span
                    key={tag}
                    className={styles.tag}
                    style={{
                      backgroundColor: `${getTagColor(tag)}1a`,
                      color: getTagColor(tag),
                    }}
                  >
                    #{tag}
                    <button
                      className={styles.tagRemove}
                      onClick={() => handleRemoveTag(tag)}
                      aria-label={`Remove tag ${tag}`}
                    >
                      <X size={10} />
                    </button>
                  </span>
                ))}
              </div>
              <div className={styles.tagInputWrapper}>
                <input
                  type="text"
                  className={styles.tagInputField}
                  placeholder="Add tag..."
                  value={tagInput}
                  onChange={(e) => handleTagInputChange(e.target.value)}
                  onKeyDown={handleTagInputKeyDown}
                  onBlur={() => {
                    // Delay to allow click on suggestion
                    setTimeout(() => {
                      setTagSuggestions([]);
                      setTagSuggestionIndex(-1);
                    }, 150);
                  }}
                  aria-label="Add tag"
                />
                {tagSuggestions.length > 0 && (
                  <div className={styles.tagSuggestions}>
                    {tagSuggestions.map((t, i) => (
                      <button
                        key={t.name}
                        className={`${styles.tagSuggestionItem} ${i === tagSuggestionIndex ? styles.tagSuggestionItemActive : ''}`}
                        onMouseDown={(e) => {
                          e.preventDefault();
                          handleAddTag(t.name);
                        }}
                      >
                        #{t.name} ({t.count})
                      </button>
                    ))}
                  </div>
                )}
              </div>
            </section>

            {/* Project */}
            <section className={styles.panelSection}>
              <h3 className={styles.panelSectionTitle}>Project</h3>
              <select
                className={styles.projectSelect}
                value={currentNote?.project_id ?? ''}
                onChange={(e) => handleProjectChange(e.target.value)}
                aria-label="Assign project"
              >
                <option value="">Inbox</option>
                {projects.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </section>

            {/* Metadata */}
            <section className={styles.panelSection}>
              <h3 className={styles.panelSectionTitle}>Metadata</h3>
              <div className={styles.metadata}>
                <div className={styles.metaRow}>
                  <span className={styles.metaLabel}>Created</span>
                  <span className={styles.metaValue}>
                    {currentNote ? formatDateTime(currentNote.created_at) : ''}
                  </span>
                </div>
                <div className={styles.metaRow}>
                  <span className={styles.metaLabel}>Modified</span>
                  <span className={styles.metaValue}>
                    {currentNote ? formatDateTime(currentNote.updated_at) : ''}
                  </span>
                </div>
                <div className={styles.metaRow}>
                  <span className={styles.metaLabel}>Path</span>
                  <span className={styles.metaValue}>
                    {currentNote?.file_path}
                  </span>
                </div>
              </div>
            </section>
          </motion.aside>
        )}
        </AnimatePresence>
        </>)}
      </div>

      {/* Save status */}
      <div className={styles.saveStatus}>
        {saveStatus === 'saving' && 'Saving...'}
        {saveStatus === 'saved' && (
          <>
            <Check size={12} /> Saved
          </>
        )}
        {saveStatus === 'unsaved' && 'Unsaved'}
      </div>

      <ConfirmModal
        open={showDeleteConfirm}
        title="Delete note"
        message="This note will be permanently deleted. This cannot be undone."
        confirmLabel="Delete"
        destructive
        onConfirm={handleDeleteConfirm}
        onCancel={() => setShowDeleteConfirm(false)}
      />
    </div>
  );
}

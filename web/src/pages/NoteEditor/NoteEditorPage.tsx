import { useEffect, useState, useCallback, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import CodeMirror, { type ReactCodeMirrorRef } from '@uiw/react-codemirror';
import { markdown } from '@codemirror/lang-markdown';
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
} from 'lucide-react';
import { useNoteStore } from '../../stores/noteStore';
import { useUIStore } from '../../stores/uiStore';
import { useWebSocket } from '../../hooks/useWebSocket';
import { getRelatedNotes, aiAssist, getTwoHopBacklinks, getOrphanNotes } from '../../api/client';
import { renderMarkdown } from '../../lib/markdown';
import { timeAgo, formatDateTime } from '../../lib/dates';
import { getTagColor } from '../../lib/tagColor';
import { seamEditorTheme } from './editorTheme';
import {
  wikilinkDecorationPlugin,
  wikilinkDecorationTheme,
  wikilinkAutocomplete,
} from './wikilinkExtension';
import type { AIAssistReq, LinkSuggestion, RelatedNote, TwoHopBacklink, WSMessage } from '../../api/types';
import styles from './NoteEditorPage.module.css';

type ViewMode = 'editor' | 'split' | 'preview';

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
  const rightPanelOpen = useUIStore((s) => s.rightPanelOpen);
  const toggleRightPanel = useUIStore((s) => s.toggleRightPanel);

  const [viewMode, setViewMode] = useState<ViewMode>('split');
  const [content, setContent] = useState('');
  const [saveStatus, setSaveStatus] = useState<'saved' | 'saving' | 'unsaved'>('saved');
  const [showMenu, setShowMenu] = useState(false);
  const [linkSuggestions, setLinkSuggestions] = useState<LinkSuggestion[]>([]);
  const [relatedNotes, setRelatedNotes] = useState<RelatedNote[]>([]);
  const [twoHopBacklinks, setTwoHopBacklinks] = useState<TwoHopBacklink[]>([]);
  const [isOrphan, setIsOrphan] = useState(false);
  const [showAIMenu, setShowAIMenu] = useState(false);
  const [aiLoading, setAILoading] = useState(false);
  const [aiResult, setAIResult] = useState<{ action: string; text: string } | null>(null);
  const saveTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const editorRef = useRef<ReactCodeMirrorRef>(null);

  useEffect(() => {
    if (id) {
      fetchNote(id);
      fetchBacklinks(id);
      // Fetch related notes (semantic similarity).
      getRelatedNotes(id).then(setRelatedNotes).catch(() => setRelatedNotes([]));
      // Fetch two-hop backlinks.
      getTwoHopBacklinks(id).then(setTwoHopBacklinks).catch(() => setTwoHopBacklinks([]));
      // Check if this note is an orphan (no links in or out).
      getOrphanNotes()
        .then((orphans) => setIsOrphan(orphans.some((o) => o.id === id)))
        .catch(() => setIsOrphan(false));
    }
    return () => {
      clearCurrentNote();
      setLinkSuggestions([]);
      setRelatedNotes([]);
      setTwoHopBacklinks([]);
      setIsOrphan(false);
    };
  }, [id, fetchNote, fetchBacklinks, clearCurrentNote]);

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
      setSaveStatus('saved');
    }
  }, [currentNote?.id]); // Only reset content on note change, not on every update

  const handleSave = useCallback(async (value: string) => {
    if (!id) return;
    setSaveStatus('saving');
    try {
      await updateNote(id, { body: value });
      setSaveStatus('saved');
    } catch {
      setSaveStatus('unsaved');
    }
  }, [id, updateNote]);

  const handleChange = useCallback((value: string) => {
    setContent(value);
    setSaveStatus('unsaved');

    if (saveTimerRef.current) {
      clearTimeout(saveTimerRef.current);
    }
    saveTimerRef.current = setTimeout(() => {
      handleSave(value);
    }, 1000);
  }, [handleSave]);

  // Cleanup save timer on unmount
  useEffect(() => {
    return () => {
      if (saveTimerRef.current) {
        clearTimeout(saveTimerRef.current);
      }
    };
  }, []);

  const handleDelete = async () => {
    if (!id) return;
    if (window.confirm('Delete this note? This cannot be undone.')) {
      await deleteNote(id);
      navigate('/');
    }
    setShowMenu(false);
  };

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
    } catch {
      // Failed silently -- could add error state here
    } finally {
      setAILoading(false);
    }
  }, [id, getSelectedText]);

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

  const renderedHtml = renderMarkdown(content);

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

          <div className={styles.menuContainer}>
            <button
              className={`${styles.toolButton} ${aiLoading ? styles.activeView : ''}`}
              onClick={() => setShowAIMenu(!showAIMenu)}
              title="AI Assist"
              aria-label="AI Assist"
              disabled={aiLoading}
            >
              <Sparkles size={16} />
            </button>
            {showAIMenu && (
              <div className={styles.menu}>
                <button
                  className={styles.menuItemDefault}
                  onClick={() => handleAIAssist('expand')}
                >
                  <Sparkles size={14} />
                  Expand
                </button>
                <button
                  className={styles.menuItemDefault}
                  onClick={() => handleAIAssist('summarize')}
                >
                  <Sparkles size={14} />
                  Summarize
                </button>
                <button
                  className={styles.menuItemDefault}
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

          <div className={styles.menuContainer}>
            <button
              className={styles.toolButton}
              onClick={() => setShowMenu(!showMenu)}
              title="More options"
              aria-label="More options"
            >
              <MoreHorizontal size={16} />
            </button>
            {showMenu && (
              <div className={styles.menu}>
                <button className={styles.menuItem} onClick={handleDelete}>
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
        <div className={styles.editorWrapper}>
          {/* Editor pane */}
          {viewMode !== 'preview' && (
            <div
              className={styles.editorPane}
              style={{ flex: viewMode === 'split' ? '1' : undefined }}
            >
              <CodeMirror
                ref={editorRef}
                value={content}
                onChange={handleChange}
                extensions={[
                  markdown(),
                  wikilinkDecorationPlugin,
                  wikilinkDecorationTheme,
                  wikilinkAutocomplete,
                ]}
                theme={seamEditorTheme}
                basicSetup={{
                  lineNumbers: true,
                  highlightActiveLine: true,
                  foldGutter: false,
                }}
                className={styles.codeMirror}
              />
            </div>
          )}

          {/* Preview pane */}
          {viewMode !== 'editor' && (
            <div
              className={styles.previewPane}
              style={{ flex: viewMode === 'split' ? '1' : undefined }}
            >
              <div className={styles.previewContent}>
                <h1 className={styles.previewTitle}>{currentNote?.title}</h1>
                <div
                  className={styles.renderedMarkdown}
                  dangerouslySetInnerHTML={{ __html: renderedHtml }}
                />
              </div>
            </div>
          )}
        </div>

        {/* Right panel */}
        {rightPanelOpen && (
          <aside className={styles.rightPanel}>
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
                        dangerouslySetInnerHTML={{ __html: renderMarkdown(aiResult.text) }}
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
                relatedNotes.map((note) => (
                  <button
                    key={note.note_id}
                    className={styles.backlinkItem}
                    onClick={() => navigate(`/notes/${note.note_id}`)}
                  >
                    <span className={styles.backlinkTitle}>{note.title}</span>
                    <span className={styles.backlinkMeta}>
                      {Math.round(note.score * 100)}% similar
                    </span>
                  </button>
                ))
              )}
            </section>

            {/* Backlinks */}
            <section className={styles.panelSection}>
              <h3 className={styles.panelSectionTitle}>Backlinks</h3>
              {backlinks.length === 0 ? (
                <p className={styles.panelEmpty}>No backlinks</p>
              ) : (
                backlinks.map((note) => (
                  <button
                    key={note.id}
                    className={styles.backlinkItem}
                    onClick={() => navigate(`/notes/${note.id}`)}
                  >
                    <span className={styles.backlinkTitle}>{note.title}</span>
                    <span className={styles.backlinkMeta}>
                      {timeAgo(note.updated_at)}
                    </span>
                  </button>
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
                  </span>
                ))}
                {(!currentNote?.tags || currentNote.tags.length === 0) && (
                  <p className={styles.panelEmpty}>No tags</p>
                )}
              </div>
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
          </aside>
        )}
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
    </div>
  );
}

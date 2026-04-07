import { useEffect, useState, useCallback, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { motion, AnimatePresence } from 'motion/react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { ArrowUpDown, CheckSquare, Plus, Sparkles } from 'lucide-react';
import { useNoteStore } from '../../stores/noteStore';
import { useProjectStore } from '../../stores/projectStore';
import { NoteCard } from '../../components/NoteCard/NoteCard';
import { NotePreview } from '../../components/NotePreview/NotePreview';
import { BulkActionBar } from '../../components/BulkActionBar/BulkActionBar';
import { EmptyState } from '../../components/EmptyState/EmptyState';
import { SynthesisModal } from '../../components/SynthesisModal/SynthesisModal';
import { getProjectColor } from '../../lib/tagColor';
import { useUIStore } from '../../stores/uiStore';
import { NoteListSkeleton } from '../../components/Skeleton/Skeleton';
import type { Note } from '../../api/types';
import styles from './ProjectPage.module.css';

const MIN_LIST_PCT = 25;
const MAX_LIST_PCT = 75;
const DEFAULT_LIST_PCT = 50;

export function ProjectPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const notes = useNoteStore((s) => s.notes);
  const total = useNoteStore((s) => s.total);
  const isLoading = useNoteStore((s) => s.isLoading);
  const fetchNotes = useNoteStore((s) => s.fetchNotes);
  const selectedNoteIds = useNoteStore((s) => s.selectedNoteIds);
  const isSelectionMode = useNoteStore((s) => s.isSelectionMode);
  const toggleNoteSelection = useNoteStore((s) => s.toggleNoteSelection);
  const selectAll = useNoteStore((s) => s.selectAll);
  const clearSelection = useNoteStore((s) => s.clearSelection);
  const projects = useProjectStore((s) => s.projects);
  const currentProject = useProjectStore((s) => s.currentProject);
  const fetchProject = useProjectStore((s) => s.fetchProject);
  const setCaptureModalOpen = useUIStore((s) => s.setCaptureModalOpen);
  const [sort, setSort] = useState<'modified' | 'created'>('modified');
  const [showSynthesis, setShowSynthesis] = useState(false);
  const [loadedNotes, setLoadedNotes] = useState<Note[]>([]);
  const [previewNoteId, setPreviewNoteId] = useState<string | null>(null);
  const [listPct, setListPct] = useState(DEFAULT_LIST_PCT);
  const scrollRef = useRef<HTMLDivElement>(null);
  const splitRef = useRef<HTMLDivElement>(null);
  const lastSelectedIndexRef = useRef<number | null>(null);
  const isDragging = useRef(false);

  const projectIndex = projects.findIndex((p) => p.id === id);
  const projectColor = getProjectColor(
    projectIndex >= 0 ? projectIndex : 0,
  );

  const previewNote = previewNoteId
    ? loadedNotes.find((n) => n.id === previewNoteId) ?? null
    : null;

  // -- Drag resize logic --
  const handleDragStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    isDragging.current = true;
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';

    function onMove(ev: MouseEvent) {
      if (!isDragging.current || !splitRef.current) return;
      const rect = splitRef.current.getBoundingClientRect();
      const x = ev.clientX - rect.left;
      const pct = Math.min(MAX_LIST_PCT, Math.max(MIN_LIST_PCT, (x / rect.width) * 100));
      setListPct(pct);
    }

    function onUp() {
      isDragging.current = false;
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
    }

    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
  }, []);

  useEffect(() => {
    if (id) {
      fetchProject(id);
      fetchNotes({ project: id, sort, limit: 100 });
    }
  }, [id, fetchProject, fetchNotes, sort]);

  // Replace loadedNotes when the store notes change from a fresh fetch.
  useEffect(() => {
    setLoadedNotes(notes);
  }, [notes]);

  // Clear preview when project changes.
  useEffect(() => {
    setPreviewNoteId(null);
  }, [id]);

  const handleLoadMore = useCallback(() => {
    if (!id) return;
    fetchNotes({
      project: id,
      sort,
      limit: 100,
      offset: loadedNotes.length,
    });
  }, [id, fetchNotes, sort, loadedNotes.length]);

  // Handle Escape to exit selection mode / close preview, and Cmd+A to select all.
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        if (isSelectionMode) {
          clearSelection();
        } else if (previewNoteId) {
          setPreviewNoteId(null);
        }
      }
      if (
        (e.metaKey || e.ctrlKey) &&
        e.key === 'a' &&
        isSelectionMode &&
        loadedNotes.length > 0
      ) {
        e.preventDefault();
        selectAll(loadedNotes.map((n) => n.id));
      }
    }
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [isSelectionMode, clearSelection, selectAll, loadedNotes, previewNoteId]);

  // Clear selection when navigating away.
  useEffect(() => {
    return () => clearSelection();
  }, [clearSelection]);

  const handleNoteSelect = useCallback(
    (noteId: string, index: number, shiftKey: boolean) => {
      if (shiftKey && lastSelectedIndexRef.current !== null) {
        const start = Math.min(lastSelectedIndexRef.current, index);
        const end = Math.max(lastSelectedIndexRef.current, index);
        const ids = loadedNotes.slice(start, end + 1).map((n) => n.id);
        selectAll(ids);
      } else {
        toggleNoteSelection(noteId);
        lastSelectedIndexRef.current = index;
      }
    },
    [loadedNotes, selectAll, toggleNoteSelection],
  );

  const handleNotePreview = useCallback((noteId: string) => {
    setPreviewNoteId((prev) => (prev === noteId ? null : noteId));
  }, []);

  // Include "load more" as an extra row if there are more notes.
  const hasMore = loadedNotes.length < total;
  const itemCount = loadedNotes.length + (hasMore ? 1 : 0);

  // TanStack Virtual returns getters that intentionally cannot be memoized;
  // React Compiler skip is expected.
  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count: itemCount,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 100,
    overscan: 10,
    gap: 8,
  });

  if (!currentProject && !isLoading) {
    return (
      <EmptyState
        heading="Project not found"
        subtext="This project may have been deleted"
        action={{ label: 'Go to Inbox', onClick: () => navigate('/') }}
      />
    );
  }

  return (
    <div className={styles.splitLayout} ref={splitRef}>
      <div
        className={styles.listPane}
        ref={scrollRef}
        style={previewNote ? { flex: `0 0 ${listPct}%` } : undefined}
      >
        <header className={styles.header}>
          <h1 className={styles.title}>{currentProject?.name}</h1>
          <div className={styles.controls}>
            {isSelectionMode ? (
              <>
                <button
                  className={styles.sortButton}
                  onClick={() =>
                    selectAll(loadedNotes.map((n) => n.id))
                  }
                >
                  Select all
                </button>
                <button
                  className={styles.sortButton}
                  onClick={clearSelection}
                >
                  Deselect
                </button>
              </>
            ) : (
              <>
                <button
                  className={styles.sortButton}
                  onClick={() =>
                    setSort(sort === 'modified' ? 'created' : 'modified')
                  }
                >
                  <ArrowUpDown size={14} />
                  <span>{sort === 'modified' ? 'Modified' : 'Created'}</span>
                </button>
                {loadedNotes.length > 0 && (
                  <button
                    className={styles.sortButton}
                    onClick={() => {
                      if (loadedNotes.length > 0) {
                        toggleNoteSelection(loadedNotes[0].id);
                        lastSelectedIndexRef.current = 0;
                      }
                    }}
                    title="Select notes for bulk operations"
                  >
                    <CheckSquare size={14} />
                    <span>Select</span>
                  </button>
                )}
                <button
                  className={styles.sortButton}
                  onClick={() => setShowSynthesis(true)}
                  title="Summarize this project"
                >
                  <Sparkles size={14} />
                  <span>Summarize</span>
                </button>
                <button
                  className={styles.newNoteButton}
                  onClick={() => setCaptureModalOpen(true, id)}
                >
                  <Plus size={14} />
                  <span>New note</span>
                </button>
              </>
            )}
          </div>
        </header>

        {currentProject?.description && (
          <p className={styles.description}>{currentProject.description}</p>
        )}

        <div className={styles.divider} />

        {isLoading ? (
          <NoteListSkeleton count={6} />
        ) : loadedNotes.length === 0 ? (
          <EmptyState
            heading="No notes yet"
            subtext="Create the first note in this project"
            action={{
              label: 'New note',
              onClick: () => setCaptureModalOpen(true, id),
            }}
          />
        ) : (
          <div
            className={styles.noteList}
            role="list"
            style={{ height: virtualizer.getTotalSize(), position: 'relative' }}
          >
            {virtualizer.getVirtualItems().map((virtualRow) => {
              // "Load more" button as the last virtual row.
              if (virtualRow.index >= loadedNotes.length) {
                return (
                  <div
                    key="load-more"
                    style={{
                      position: 'absolute',
                      top: 0,
                      left: 0,
                      right: 0,
                      transform: `translateY(${virtualRow.start}px)`,
                    }}
                  >
                    <button
                      className={styles.loadMore}
                      onClick={handleLoadMore}
                    >
                      Load more
                    </button>
                  </div>
                );
              }

              const note = loadedNotes[virtualRow.index];
              const noteIndex = virtualRow.index;
              return (
                <div
                  key={note.id}
                  ref={virtualizer.measureElement}
                  data-index={virtualRow.index}
                  style={{
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    right: 0,
                    transform: `translateY(${virtualRow.start}px)`,
                  }}
                  onClick={(e) => {
                    if (e.shiftKey && isSelectionMode) {
                      e.preventDefault();
                      handleNoteSelect(note.id, noteIndex, true);
                    }
                  }}
                >
                  <motion.div
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{
                      duration: 0.2,
                      ease: [0.16, 1, 0.3, 1],
                      delay: virtualRow.index < 20 ? virtualRow.index * 0.03 : 0,
                    }}
                  >
                    <NoteCard
                      note={note}
                      projectName={currentProject?.name}
                      projectColor={projectColor}
                      selected={selectedNoteIds.has(note.id)}
                      selectionMode={isSelectionMode}
                      onSelect={(noteId) =>
                        handleNoteSelect(noteId, noteIndex, false)
                      }
                      previewMode={!isSelectionMode}
                      onPreview={handleNotePreview}
                      previewing={previewNoteId === note.id}
                    />
                  </motion.div>
                </div>
              );
            })}
          </div>
        )}

        <BulkActionBar />

        {showSynthesis && id && (
          <SynthesisModal
            scope="project"
            projectId={id}
            title={`Summarize: ${currentProject?.name ?? 'Project'}`}
            onClose={() => setShowSynthesis(false)}
          />
        )}
      </div>

      <AnimatePresence>
        {previewNote && (
          <>
            {/* Drag handle */}
            <div
              className={styles.resizeHandle}
              onMouseDown={handleDragStart}
              role="separator"
              aria-orientation="vertical"
              aria-label="Resize list and preview panes"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'ArrowLeft') {
                  e.preventDefault();
                  setListPct((p) => Math.max(MIN_LIST_PCT, p - 2));
                } else if (e.key === 'ArrowRight') {
                  e.preventDefault();
                  setListPct((p) => Math.min(MAX_LIST_PCT, p + 2));
                }
              }}
            >
              <div className={styles.resizeHandleBar} />
            </div>

            <motion.div
              className={styles.previewPane}
              initial={{ opacity: 0, x: 20 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: 20 }}
              transition={{ duration: 0.2, ease: [0.16, 1, 0.3, 1] }}
            >
              <NotePreview note={previewNote} />
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </div>
  );
}

import { useEffect, useState, useCallback, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { motion } from 'motion/react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { ArrowUpDown, CheckSquare, Plus, Sparkles } from 'lucide-react';
import { useNoteStore } from '../../stores/noteStore';
import { useProjectStore } from '../../stores/projectStore';
import { NoteCard } from '../../components/NoteCard/NoteCard';
import { BulkActionBar } from '../../components/BulkActionBar/BulkActionBar';
import { EmptyState } from '../../components/EmptyState/EmptyState';
import { SynthesisModal } from '../../components/SynthesisModal/SynthesisModal';
import { getProjectColor } from '../../lib/tagColor';
import { useUIStore } from '../../stores/uiStore';
import { NoteListSkeleton } from '../../components/Skeleton/Skeleton';
import type { Note } from '../../api/types';
import styles from './ProjectPage.module.css';

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
  const scrollRef = useRef<HTMLDivElement>(null);
  const lastSelectedIndexRef = useRef<number | null>(null);

  const projectIndex = projects.findIndex((p) => p.id === id);
  const projectColor = getProjectColor(
    projectIndex >= 0 ? projectIndex : 0,
  );

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

  const handleLoadMore = useCallback(() => {
    if (!id) return;
    fetchNotes({
      project: id,
      sort,
      limit: 100,
      offset: loadedNotes.length,
    }).then(() => {
      const storeState = useNoteStore.getState();
      setLoadedNotes((prev) => {
        const existingIds = new Set(prev.map((n) => n.id));
        const newNotes = storeState.notes.filter((n) => !existingIds.has(n.id));
        return [...prev, ...newNotes];
      });
    });
  }, [id, fetchNotes, sort, loadedNotes.length]);

  // Handle Escape to exit selection mode and Cmd+A to select all.
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape' && isSelectionMode) {
        clearSelection();
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
  }, [isSelectionMode, clearSelection, selectAll, loadedNotes]);

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

  // Include "load more" as an extra row if there are more notes.
  const hasMore = loadedNotes.length < total;
  const itemCount = loadedNotes.length + (hasMore ? 1 : 0);

  const virtualizer = useVirtualizer({
    count: itemCount,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 100,
    overscan: 10,
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
    <div className={styles.page} ref={scrollRef}>
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
              <motion.div
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
                initial={{ opacity: 0, y: 4 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{
                  duration: 0.2,
                  ease: [0.16, 1, 0.3, 1],
                  delay: virtualRow.index < 20 ? virtualRow.index * 0.03 : 0,
                }}
                onClick={(e) => {
                  if (e.shiftKey && isSelectionMode) {
                    e.preventDefault();
                    handleNoteSelect(note.id, noteIndex, true);
                  }
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
                />
              </motion.div>
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
  );
}

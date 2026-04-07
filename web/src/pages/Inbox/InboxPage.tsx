import { useEffect, useState, useCallback, useRef } from 'react';
import { useSearchParams } from 'react-router-dom';
import { motion } from 'motion/react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { ArrowUpDown, CheckSquare, Sparkles } from 'lucide-react';
import { useNoteStore } from '../../stores/noteStore';
import { useUIStore } from '../../stores/uiStore';
import { NoteCard } from '../../components/NoteCard/NoteCard';
import { BulkActionBar } from '../../components/BulkActionBar/BulkActionBar';
import { EmptyState } from '../../components/EmptyState/EmptyState';
import { SynthesisModal } from '../../components/SynthesisModal/SynthesisModal';
import { NoteListSkeleton } from '../../components/Skeleton/Skeleton';
import type { Note } from '../../api/types';
import styles from './InboxPage.module.css';

export function InboxPage() {
  const notes = useNoteStore((s) => s.notes);
  const total = useNoteStore((s) => s.total);
  const isLoading = useNoteStore((s) => s.isLoading);
  const fetchNotes = useNoteStore((s) => s.fetchNotes);
  const selectedNoteIds = useNoteStore((s) => s.selectedNoteIds);
  const isSelectionMode = useNoteStore((s) => s.isSelectionMode);
  const toggleNoteSelection = useNoteStore((s) => s.toggleNoteSelection);
  const selectAll = useNoteStore((s) => s.selectAll);
  const clearSelection = useNoteStore((s) => s.clearSelection);
  const setCaptureModalOpen = useUIStore((s) => s.setCaptureModalOpen);
  const fetchTags = useUIStore((s) => s.fetchTags);
  const [searchParams] = useSearchParams();
  const tagFilter = searchParams.get('tag');
  const [sort, setSort] = useState<'modified' | 'created'>('modified');
  const [showSynthesis, setShowSynthesis] = useState(false);
  const [loadedNotes, setLoadedNotes] = useState<Note[]>([]);
  const scrollRef = useRef<HTMLDivElement>(null);
  const lastSelectedIndexRef = useRef<number | null>(null);

  useEffect(() => {
    fetchNotes({
      project: 'inbox',
      tag: tagFilter ?? undefined,
      sort,
      limit: 100,
    });
    fetchTags();
  }, [fetchNotes, fetchTags, tagFilter, sort]);

  // Replace loadedNotes when the store notes change from a fresh fetch.
  useEffect(() => {
    setLoadedNotes(notes);
  }, [notes]);

  const handleLoadMore = useCallback(() => {
    fetchNotes({
      project: 'inbox',
      tag: tagFilter ?? undefined,
      sort,
      limit: 100,
      offset: loadedNotes.length,
    });
  }, [fetchNotes, tagFilter, sort, loadedNotes.length]);

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

  // Clear selection when navigating away (tag filter changes, etc).
  useEffect(() => {
    return () => clearSelection();
  }, [clearSelection]);

  const handleNoteSelect = useCallback(
    (id: string, index: number, shiftKey: boolean) => {
      if (shiftKey && lastSelectedIndexRef.current !== null) {
        const start = Math.min(lastSelectedIndexRef.current, index);
        const end = Math.max(lastSelectedIndexRef.current, index);
        const ids = loadedNotes.slice(start, end + 1).map((n) => n.id);
        selectAll(ids);
      } else {
        toggleNoteSelection(id);
        lastSelectedIndexRef.current = index;
      }
    },
    [loadedNotes, selectAll, toggleNoteSelection],
  );

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

  return (
    <div className={styles.page} ref={scrollRef}>
      <header className={styles.header}>
        <h1 className={styles.title}>Inbox</h1>
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
                title={`Sort by ${sort === 'modified' ? 'created' : 'modified'}`}
              >
                <ArrowUpDown size={14} />
                <span>{sort === 'modified' ? 'Modified' : 'Created'}</span>
              </button>
              {loadedNotes.length > 0 && (
                <button
                  className={styles.sortButton}
                  onClick={() => {
                    // Enter selection mode by selecting nothing (shows checkboxes)
                    // User can then click cards to select.
                    // Start by selecting the first note to make it obvious.
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
              {tagFilter && (
                <button
                  className={styles.sortButton}
                  onClick={() => setShowSynthesis(true)}
                  title={`Summarize notes tagged #${tagFilter}`}
                >
                  <Sparkles size={14} />
                  <span>Summarize</span>
                </button>
              )}
            </>
          )}
        </div>
      </header>

      {tagFilter && (
        <div className={styles.activeFilter}>
          <span className={styles.filterPill}>#{tagFilter}</span>
        </div>
      )}

      <div className={styles.divider} />

      {isLoading ? (
        <NoteListSkeleton count={6} />
      ) : loadedNotes.length === 0 ? (
        <EmptyState
          heading="Nothing in the inbox"
          subtext="Capture a thought to get started"
          action={{
            label: 'Capture',
            onClick: () => setCaptureModalOpen(true),
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
                    selected={selectedNoteIds.has(note.id)}
                    selectionMode={isSelectionMode}
                    onSelect={(id) =>
                      handleNoteSelect(id, noteIndex, false)
                    }
                  />
                </motion.div>
              </div>
            );
          })}
        </div>
      )}

      <BulkActionBar />

      {showSynthesis && tagFilter && (
        <SynthesisModal
          scope="tag"
          tag={tagFilter}
          title={`Summarize: #${tagFilter}`}
          onClose={() => setShowSynthesis(false)}
        />
      )}
    </div>
  );
}

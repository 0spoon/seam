import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { format, isToday, parseISO } from 'date-fns';
import { listNotes } from '../../api/client';
import { useToastStore } from '../../components/Toast/ToastContainer';
import { NoteListSkeleton } from '../../components/Skeleton/Skeleton';
import type { Note } from '../../api/types';
import styles from './TimelinePage.module.css';

type SortMode = 'created' | 'modified';

interface DateGroup {
  date: string; // YYYY-MM-DD
  displayDate: string;
  isToday: boolean;
  notes: Note[];
}

function groupNotesByDate(notes: Note[], mode: SortMode): DateGroup[] {
  const groups = new Map<string, Note[]>();

  for (const note of notes) {
    const dateStr = mode === 'created' ? note.created_at : note.updated_at;
    const parsed = parseISO(dateStr);
    const key = format(parsed, 'yyyy-MM-dd');
    const existing = groups.get(key) ?? [];
    existing.push(note);
    groups.set(key, existing);
  }

  // Sort by date descending.
  const sorted = Array.from(groups.entries()).sort(
    ([a], [b]) => b.localeCompare(a),
  );

  return sorted.map(([dateKey, notes]) => {
    const parsed = parseISO(dateKey);
    return {
      date: dateKey,
      displayDate: format(parsed, 'MMM d, yyyy'),
      isToday: isToday(parsed),
      notes,
    };
  });
}

const PAGE_SIZE = 50;

export function TimelinePage() {
  const navigate = useNavigate();
  const addToast = useToastStore((s) => s.addToast);
  const [notes, setNotes] = useState<Note[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [totalNotes, setTotalNotes] = useState(0);
  const [sortMode, setSortMode] = useState<SortMode>('modified');
  const [jumpDate, setJumpDate] = useState('');

  const hasMore = notes.length < totalNotes;

  const fetchNotes = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const { notes: fetchedNotes, total } = await listNotes({
        sort: sortMode,
        sort_dir: 'desc',
        limit: PAGE_SIZE,
        offset: 0,
      });
      setNotes(fetchedNotes);
      setTotalNotes(total);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load timeline';
      setError(message);
      addToast(message, 'error');
    } finally {
      setLoading(false);
    }
  }, [sortMode, addToast]);

  const loadMore = useCallback(async () => {
    if (loadingMore || !hasMore) return;
    setLoadingMore(true);
    try {
      const { notes: moreNotes, total } = await listNotes({
        sort: sortMode,
        sort_dir: 'desc',
        limit: PAGE_SIZE,
        offset: notes.length,
      });
      setNotes((prev) => [...prev, ...moreNotes]);
      setTotalNotes(total);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load more notes';
      addToast(message, 'error');
    } finally {
      setLoadingMore(false);
    }
  }, [sortMode, notes.length, loadingMore, hasMore, addToast]);

  useEffect(() => {
    fetchNotes();
  }, [fetchNotes]);

  const dateGroups = groupNotesByDate(notes, sortMode);

  // Scroll to date when jump date changes.
  useEffect(() => {
    if (!jumpDate) return;
    const el = document.getElementById(`date-${jumpDate}`);
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
  }, [jumpDate]);

  if (loading) {
    return (
      <div className={styles.container}>
        <div className={styles.header}>
          <h1 className={styles.title}>Timeline</h1>
        </div>
        <NoteListSkeleton count={8} />
      </div>
    );
  }

  if (error && notes.length === 0) {
    return (
      <div className={styles.container}>
        <div className={styles.header}>
          <h1 className={styles.title}>Timeline</h1>
        </div>
        <div className={styles.errorState}>
          <div className={styles.errorTitle}>Failed to load timeline</div>
          <div className={styles.errorDescription}>{error}</div>
          <button className={styles.retryButton} onClick={fetchNotes}>
            Retry
          </button>
        </div>
      </div>
    );
  }

  if (notes.length === 0) {
    return (
      <div className={styles.container}>
        <div className={styles.header}>
          <h1 className={styles.title}>Timeline</h1>
        </div>
        <div className={styles.empty}>
          <div className={styles.emptyTitle}>No notes yet</div>
        </div>
      </div>
    );
  }

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <h1 className={styles.title}>Timeline</h1>
        <div className={styles.controls}>
          <div className={styles.toggle}>
            <button
              className={`${styles.toggleButton} ${sortMode === 'created' ? styles.toggleActive : ''}`}
              onClick={() => setSortMode('created')}
            >
              Created
            </button>
            <button
              className={`${styles.toggleButton} ${sortMode === 'modified' ? styles.toggleActive : ''}`}
              onClick={() => setSortMode('modified')}
            >
              Modified
            </button>
          </div>
          <input
            type="date"
            className={styles.dateInput}
            value={jumpDate}
            onChange={(e) => setJumpDate(e.target.value)}
            title="Jump to date"
            aria-label="Jump to date"
          />
        </div>
      </div>

      <div className={styles.timeline}>
        <div className={styles.timelineLine} />

        {dateGroups.map((group) => (
          <div
            key={group.date}
            id={`date-${group.date}`}
            className={styles.dateGroup}
          >
            <div className={styles.dateMarker}>
              <span
                className={`${styles.dateDot} ${group.isToday ? styles.dateDotToday : ''}`}
              />
              <span className={styles.dateText}>{group.displayDate}</span>
              <span className={styles.noteCount}>
                {group.notes.length} {group.notes.length === 1 ? 'note' : 'notes'}
              </span>
            </div>

            <div className={styles.noteList}>
              {group.notes.map((note) => (
                <button
                  key={note.id}
                  className={styles.noteItem}
                  onClick={() => navigate(`/notes/${note.id}`)}
                >
                  <span className={styles.noteTitle}>{note.title}</span>
                  <div className={styles.noteMeta}>
                    <span className={styles.noteTime}>
                      {format(
                        parseISO(
                          sortMode === 'created'
                            ? note.created_at
                            : note.updated_at,
                        ),
                        'h:mm a',
                      )}
                    </span>
                    {note.tags && note.tags.length > 0 && (
                      <div className={styles.noteTags}>
                        {note.tags.slice(0, 3).map((tag) => (
                          <span key={tag} className={styles.noteTag}>
                            #{tag}
                          </span>
                        ))}
                      </div>
                    )}
                  </div>
                </button>
              ))}
            </div>
          </div>
        ))}

        {hasMore && (
          <div className={styles.loadMoreWrapper}>
            <button
              className={styles.loadMoreButton}
              onClick={loadMore}
              disabled={loadingMore}
            >
              {loadingMore ? 'Loading...' : `Load more (${totalNotes - notes.length} remaining)`}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ArrowUpDown, Sparkles } from 'lucide-react';
import { useNoteStore } from '../../stores/noteStore';
import { useUIStore } from '../../stores/uiStore';
import { NoteCard } from '../../components/NoteCard/NoteCard';
import { EmptyState } from '../../components/EmptyState/EmptyState';
import { SynthesisModal } from '../../components/SynthesisModal/SynthesisModal';
import styles from './InboxPage.module.css';

export function InboxPage() {
  const notes = useNoteStore((s) => s.notes);
  const total = useNoteStore((s) => s.total);
  const isLoading = useNoteStore((s) => s.isLoading);
  const fetchNotes = useNoteStore((s) => s.fetchNotes);
  const setCaptureModalOpen = useUIStore((s) => s.setCaptureModalOpen);
  const fetchTags = useUIStore((s) => s.fetchTags);
  const [searchParams] = useSearchParams();
  const tagFilter = searchParams.get('tag');
  const [sort, setSort] = useState<'modified' | 'created'>('modified');
  const [showSynthesis, setShowSynthesis] = useState(false);

  useEffect(() => {
    fetchNotes({
      project: 'inbox',
      tag: tagFilter ?? undefined,
      sort,
      limit: 100,
    });
    fetchTags();
  }, [fetchNotes, fetchTags, tagFilter, sort]);

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <h1 className={styles.title}>Inbox</h1>
        <div className={styles.controls}>
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
        </div>
      </header>

      {tagFilter && (
        <div className={styles.activeFilter}>
          <span className={styles.filterPill}>#{tagFilter}</span>
        </div>
      )}

      <div className={styles.divider} />

      {isLoading ? (
        <div className={styles.loading}>Loading...</div>
      ) : notes.length === 0 ? (
        <EmptyState
          heading="Nothing in the inbox"
          subtext="Capture a thought to get started"
          action={{
            label: 'Capture',
            onClick: () => setCaptureModalOpen(true),
          }}
        />
      ) : (
        <div className={styles.noteList} role="list">
          {notes.map((note) => (
            <NoteCard key={note.id} note={note} />
          ))}
          {notes.length < total && (
            <button
              className={styles.loadMore}
              onClick={() =>
                fetchNotes({
                  project: 'inbox',
                  tag: tagFilter ?? undefined,
                  sort,
                  limit: 100,
                  offset: notes.length,
                })
              }
            >
              Load more
            </button>
          )}
        </div>
      )}

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

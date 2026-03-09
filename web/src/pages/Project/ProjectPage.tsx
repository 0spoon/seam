import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { ArrowUpDown, Plus, Sparkles } from 'lucide-react';
import { useNoteStore } from '../../stores/noteStore';
import { useProjectStore } from '../../stores/projectStore';
import { NoteCard } from '../../components/NoteCard/NoteCard';
import { EmptyState } from '../../components/EmptyState/EmptyState';
import { SynthesisModal } from '../../components/SynthesisModal/SynthesisModal';
import { getProjectColor } from '../../lib/tagColor';
import { useUIStore } from '../../stores/uiStore';
import styles from './ProjectPage.module.css';

export function ProjectPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const notes = useNoteStore((s) => s.notes);
  const total = useNoteStore((s) => s.total);
  const isLoading = useNoteStore((s) => s.isLoading);
  const fetchNotes = useNoteStore((s) => s.fetchNotes);
  const projects = useProjectStore((s) => s.projects);
  const currentProject = useProjectStore((s) => s.currentProject);
  const fetchProject = useProjectStore((s) => s.fetchProject);
  const setCaptureModalOpen = useUIStore((s) => s.setCaptureModalOpen);
  const [sort, setSort] = useState<'modified' | 'created'>('modified');
  const [showSynthesis, setShowSynthesis] = useState(false);

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
    <div className={styles.page}>
      <header className={styles.header}>
        <h1 className={styles.title}>{currentProject?.name}</h1>
        <div className={styles.controls}>
          <button
            className={styles.sortButton}
            onClick={() =>
              setSort(sort === 'modified' ? 'created' : 'modified')
            }
          >
            <ArrowUpDown size={14} />
            <span>{sort === 'modified' ? 'Modified' : 'Created'}</span>
          </button>
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
            onClick={() => setCaptureModalOpen(true)}
          >
            <Plus size={14} />
            <span>New note</span>
          </button>
        </div>
      </header>

      {currentProject?.description && (
        <p className={styles.description}>{currentProject.description}</p>
      )}

      <div className={styles.divider} />

      {isLoading ? (
        <div className={styles.loading}>Loading...</div>
      ) : notes.length === 0 ? (
        <EmptyState
          heading="No notes yet"
          subtext="Create the first note in this project"
          action={{
            label: 'New note',
            onClick: () => setCaptureModalOpen(true),
          }}
        />
      ) : (
        <div className={styles.noteList} role="list">
          {notes.map((note) => (
            <NoteCard
              key={note.id}
              note={note}
              projectName={currentProject?.name}
              projectColor={projectColor}
            />
          ))}
          {notes.length < total && (
            <button
              className={styles.loadMore}
              onClick={() =>
                fetchNotes({
                  project: id,
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

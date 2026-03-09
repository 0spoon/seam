import { useNavigate } from 'react-router-dom';
import type { Note } from '../../api/types';
import { timeAgo } from '../../lib/dates';
import { getTagColor } from '../../lib/tagColor';
import styles from './NoteCard.module.css';

interface NoteCardProps {
  note: Note;
  projectName?: string;
  projectColor?: string;
}

export function NoteCard({ note, projectName, projectColor }: NoteCardProps) {
  const navigate = useNavigate();

  const preview = note.body
    .replace(/^---[\s\S]*?---\s*/m, '')
    .replace(/#{1,6}\s/g, '')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g, (_, target, display) => display ?? target)
    .replace(/[*_~`]/g, '')
    .trim()
    .slice(0, 200);

  return (
    <article
      className={styles.card}
      onClick={() => navigate(`/notes/${note.id}`)}
      role="listitem"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Enter') navigate(`/notes/${note.id}`);
      }}
    >
      <div className={styles.header}>
        <h3 className={styles.title}>{note.title}</h3>
        <time className={styles.timestamp}>{timeAgo(note.updated_at)}</time>
      </div>
      {preview && <p className={styles.preview}>{preview}</p>}
      <div className={styles.footer}>
        <div className={styles.tags}>
          {note.tags?.map((tag) => (
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
        </div>
        {projectName && (
          <span className={styles.project}>
            {projectColor && (
              <span
                className={styles.projectDot}
                style={{ backgroundColor: projectColor }}
              />
            )}
            {projectName}
          </span>
        )}
      </div>
    </article>
  );
}

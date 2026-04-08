import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { ExternalLink } from 'lucide-react';
import type { Note } from '../../api/types';
import { renderMarkdown } from '../../lib/markdown';
import { sanitizeHtml } from '../../lib/sanitize';
import { timeAgo } from '../../lib/dates';
import { getTagColor } from '../../lib/tagColor';
import styles from './NotePreview.module.css';

interface NotePreviewProps {
  note: Note;
}

export function NotePreview({ note }: NotePreviewProps) {
  const navigate = useNavigate();

  const bodyWithoutFrontmatter = note.body.replace(/^---[\s\S]*?---\s*/m, '');

  const rendered = useMemo(
    () => sanitizeHtml(renderMarkdown(bodyWithoutFrontmatter)),
    [bodyWithoutFrontmatter],
  );

  return (
    <div className={styles.preview}>
      <header className={styles.header}>
        <h2 className={styles.title}>{note.title}</h2>
        <button
          className={styles.openButton}
          onClick={() => navigate(`/notes/${note.id}`)}
          title="Open in editor"
        >
          <ExternalLink size={14} />
          <span>Edit</span>
        </button>
      </header>

      <div className={styles.meta}>
        <time className={styles.timestamp}>{timeAgo(note.updated_at)}</time>
        {note.tags?.length > 0 && (
          <div className={styles.tags}>
            {note.tags.map((tag) => (
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
        )}
      </div>

      <div className={styles.divider} />

      <div className={styles.renderedMarkdown} dangerouslySetInnerHTML={{ __html: rendered }} />
    </div>
  );
}

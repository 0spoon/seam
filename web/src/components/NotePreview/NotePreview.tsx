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

  const hasContent = bodyWithoutFrontmatter.trim().length > 0;

  return (
    <div className={styles.preview}>
      <div className={styles.inner}>
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
          {note.source_url && (
            <a
              className={styles.sourceLink}
              href={note.source_url}
              target="_blank"
              rel="noopener noreferrer"
            >
              Source
            </a>
          )}
        </div>

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

        <div className={styles.divider} />

        {hasContent ? (
          <div
            className={styles.renderedMarkdown}
            dangerouslySetInnerHTML={{ __html: rendered }}
          />
        ) : (
          <p className={styles.emptyBody}>No content</p>
        )}
      </div>
    </div>
  );
}

import { memo, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { Check } from 'lucide-react';
import type { Note } from '../../api/types';
import { timeAgo } from '../../lib/dates';
import { getTagColor } from '../../lib/tagColor';
import styles from './NoteCard.module.css';

interface NoteCardProps {
  note: Note;
  projectName?: string;
  projectColor?: string;
  selected?: boolean;
  selectionMode?: boolean;
  onSelect?: (id: string) => void;
  /** When true, single-click calls onPreview, double-click navigates. */
  previewMode?: boolean;
  /** Called on single-click when previewMode is true. */
  onPreview?: (id: string) => void;
  /** Whether this card is the actively previewed note. */
  previewing?: boolean;
}

export const NoteCard = memo(function NoteCard({
  note,
  projectName,
  projectColor,
  selected = false,
  selectionMode = false,
  onSelect,
  previewMode = false,
  onPreview,
  previewing = false,
}: NoteCardProps) {
  const navigate = useNavigate();

  const preview = note.body
    .replace(/^---[\s\S]*?---\s*/m, '')
    .replace(/#{1,6}\s/g, '')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g, (_, target, display) => display ?? target)
    .replace(/[*_~`]/g, '')
    .trim()
    .slice(0, 200);

  const handleClick = useCallback(
    (e: React.MouseEvent) => {
      // Cmd/Ctrl+Click toggles selection regardless of mode.
      if ((e.metaKey || e.ctrlKey) && onSelect) {
        e.preventDefault();
        onSelect(note.id);
        return;
      }
      // In selection mode, any click toggles selection.
      if (selectionMode && onSelect) {
        onSelect(note.id);
        return;
      }
      // In preview mode, single-click selects for preview.
      if (previewMode && onPreview) {
        onPreview(note.id);
        return;
      }
      navigate(`/notes/${note.id}`);
    },
    [navigate, note.id, selectionMode, onSelect, previewMode, onPreview],
  );

  const handleDoubleClick = useCallback(
    (e: React.MouseEvent) => {
      // In selection mode, double-click does nothing special.
      if (selectionMode) return;
      // Double-click always navigates to the editor.
      e.preventDefault();
      navigate(`/notes/${note.id}`);
    },
    [navigate, note.id, selectionMode],
  );

  const handleCheckboxClick = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      onSelect?.(note.id);
    },
    [note.id, onSelect],
  );

  const cardClass = [
    styles.card,
    selected ? styles.selected : '',
    previewing ? styles.previewing : '',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <article
      className={cardClass}
      onClick={handleClick}
      onDoubleClick={handleDoubleClick}
      role="listitem"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          if (selectionMode && onSelect) {
            onSelect(note.id);
          } else if (previewMode && onPreview) {
            onPreview(note.id);
          } else {
            navigate(`/notes/${note.id}`);
          }
        }
      }}
    >
      <div className={styles.header}>
        {(selectionMode || selected) && (
          <div
            className={`${styles.checkbox} ${selected ? styles.checked : ''}`}
            onClick={handleCheckboxClick}
            role="checkbox"
            aria-checked={selected}
            tabIndex={-1}
          >
            {selected && <Check size={12} strokeWidth={3} />}
          </div>
        )}
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
              <span className={styles.projectDot} style={{ backgroundColor: projectColor }} />
            )}
            {projectName}
          </span>
        )}
      </div>
    </article>
  );
});

import { getTagColor } from '../../lib/tagColor';
import styles from './LinkPreviewCard.module.css';

interface LinkPreviewCardProps {
  title: string;
  snippet?: string;
  tags?: string[];
  dangling: boolean;
  position: { top: number; left: number };
  onCreateNote?: (title: string) => void;
  onMouseEnter?: () => void;
  onMouseLeave?: () => void;
}

export function LinkPreviewCard({
  title,
  snippet,
  tags,
  dangling,
  position,
  onCreateNote,
  onMouseEnter,
  onMouseLeave,
}: LinkPreviewCardProps) {
  // Clamp position to viewport bounds.
  const style: React.CSSProperties = {
    top: Math.min(position.top, window.innerHeight - 200),
    left: Math.min(position.left, window.innerWidth - 340),
  };

  if (dangling) {
    return (
      <div
        className={styles.card}
        style={style}
        onMouseEnter={onMouseEnter}
        onMouseLeave={onMouseLeave}
      >
        <h4 className={styles.title}>{title}</h4>
        <p className={styles.danglingMessage}>Note does not exist</p>
        {onCreateNote && (
          <button
            className={styles.createButton}
            onClick={() => onCreateNote(title)}
          >
            Create &ldquo;{title}&rdquo;
          </button>
        )}
      </div>
    );
  }

  return (
    <div
      className={styles.card}
      style={style}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
    >
      <h4 className={styles.title}>{title}</h4>
      {snippet && <p className={styles.snippet}>{snippet}</p>}
      {tags && tags.length > 0 && (
        <div className={styles.tags}>
          {tags.map((tag) => (
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
  );
}

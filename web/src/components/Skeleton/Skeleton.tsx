import styles from './Skeleton.module.css';

interface SkeletonProps {
  width?: string | number;
  height?: string | number;
  borderRadius?: string;
  className?: string;
}

export function Skeleton({ width, height, borderRadius, className }: SkeletonProps) {
  return (
    <div
      className={`${styles.skeleton} ${className ?? ''}`}
      style={{ width, height, borderRadius }}
      aria-hidden="true"
    />
  );
}

export function NoteCardSkeleton() {
  return (
    <div className={styles.noteCard} aria-hidden="true">
      <div className={styles.noteCardTitle} />
      <div className={styles.noteCardBody} />
      <div className={styles.noteCardBodyShort} />
      <div className={styles.noteCardMeta}>
        <div className={styles.noteCardTag} />
        <div className={styles.noteCardDate} />
      </div>
    </div>
  );
}

export function NoteListSkeleton({ count = 5 }: { count?: number }) {
  return (
    <div role="status" aria-label="Loading notes">
      {Array.from({ length: count }, (_, i) => (
        <NoteCardSkeleton key={i} />
      ))}
    </div>
  );
}

export function EditorSkeleton() {
  return (
    <div className={styles.editor} role="status" aria-label="Loading editor">
      <div className={styles.editorTitle} />
      <div className={styles.editorLineFull} />
      <div className={styles.editorLineFull} />
      <div className={styles.editorLineMedium} />
      <div className={styles.editorLineFull} />
      <div className={styles.editorLineShort} />
      <div className={styles.editorLineFull} />
      <div className={styles.editorLineMedium} />
    </div>
  );
}

export function FullPageSkeleton() {
  return (
    <div className={styles.fullPage} role="status" aria-label="Loading">
      <div className={styles.fullPageLogo} />
      <div className={styles.fullPageText} />
    </div>
  );
}

export function GraphSkeleton() {
  return (
    <div className={styles.graph} role="status" aria-label="Loading graph">
      <div className={styles.graphCircle} />
    </div>
  );
}

export function SearchResultSkeleton({ count = 4 }: { count?: number }) {
  return (
    <div role="status" aria-label="Loading results">
      {Array.from({ length: count }, (_, i) => (
        <div key={i} className={styles.searchResult}>
          <div className={styles.searchResultTitle} />
          <div className={styles.searchResultSnippet} />
          <div className={styles.searchResultSnippetShort} />
        </div>
      ))}
    </div>
  );
}

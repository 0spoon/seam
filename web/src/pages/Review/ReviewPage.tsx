import { useEffect } from 'react';
import { Sprout } from 'lucide-react';
import { useReviewStore } from '../../stores/reviewStore';
import { EmptyState } from '../../components/EmptyState/EmptyState';
import { GenericPageSkeleton } from '../../components/Skeleton/Skeleton';
import { ReviewCard } from './ReviewCard';
import styles from './ReviewPage.module.css';

export function ReviewPage() {
  const queue = useReviewStore((s) => s.queue);
  const stats = useReviewStore((s) => s.stats);
  const isLoading = useReviewStore((s) => s.isLoading);
  const error = useReviewStore((s) => s.error);
  const fetchQueue = useReviewStore((s) => s.fetchQueue);
  const dismissItem = useReviewStore((s) => s.dismissItem);
  const recordAction = useReviewStore((s) => s.recordAction);

  useEffect(() => {
    fetchQueue();
  }, [fetchQueue]);

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <h1 className={styles.title}>
          <Sprout size={20} style={{ verticalAlign: 'text-bottom', marginRight: 'var(--space-2)' }} />
          Garden
        </h1>
        <p className={styles.subtitle}>
          Review and organize notes that need attention
        </p>
      </div>

      {(stats.reviewed > 0 || stats.tagged > 0 || stats.linked > 0 || stats.moved > 0) && (
        <div className={styles.statsBar}>
          <div className={styles.stat}>
            <span className={styles.statValue}>{stats.reviewed}</span> reviewed
          </div>
          <div className={styles.statDivider} />
          <div className={styles.stat}>
            <span className={styles.statValue}>{stats.linked}</span> links
          </div>
          <div className={styles.statDivider} />
          <div className={styles.stat}>
            <span className={styles.statValue}>{stats.tagged}</span> tags
          </div>
          <div className={styles.statDivider} />
          <div className={styles.stat}>
            <span className={styles.statValue}>{stats.moved}</span> moved
          </div>
        </div>
      )}

      {error && <p className={styles.errorMessage}>{error}</p>}

      {isLoading ? (
        <GenericPageSkeleton />
      ) : queue.length === 0 ? (
        <EmptyState
          heading="Your garden is tended"
          subtext="All notes are connected, tagged, and organized"
        />
      ) : (
        <div className={styles.queue}>
          {queue.map((item) => (
            <ReviewCard
              key={item.note_id}
              item={item}
              onDismiss={() => dismissItem(item.note_id)}
              onAction={recordAction}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export default ReviewPage;

import styles from './EmptyState.module.css';

interface EmptyStateProps {
  heading: string;
  subtext: string;
  action?: {
    label: string;
    onClick: () => void;
  };
}

export function EmptyState({ heading, subtext, action }: EmptyStateProps) {
  return (
    <div className={styles.container}>
      <div className={styles.bgPattern} aria-hidden="true" />
      <h2 className={styles.heading}>{heading}</h2>
      <p className={styles.subtext}>{subtext}</p>
      {action && (
        <button className={styles.action} onClick={action.onClick}>
          {action.label}
        </button>
      )}
    </div>
  );
}

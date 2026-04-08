import { useMemo } from 'react';
import { AlertTriangle, Check, X, Loader2 } from 'lucide-react';
import styles from './ToolConfirmationCard.module.css';

interface ToolConfirmationCardProps {
  toolName: string;
  arguments: string;
  onApprove: () => void;
  onReject: () => void;
  loading?: boolean;
}

export function ToolConfirmationCard({
  toolName,
  arguments: argsString,
  onApprove,
  onReject,
  loading = false,
}: ToolConfirmationCardProps) {
  const prettyArgs = useMemo(() => {
    try {
      return JSON.stringify(JSON.parse(argsString), null, 2);
    } catch {
      return argsString;
    }
  }, [argsString]);

  return (
    <div className={styles.card} role="alertdialog" aria-label="Approve tool">
      <div className={styles.header}>
        <AlertTriangle size={16} className={styles.warnIcon} />
        <span className={styles.title}>
          Approve <span className={styles.toolName}>&quot;{toolName}&quot;</span>?
        </span>
      </div>
      <pre className={styles.args}>{prettyArgs}</pre>
      <div className={styles.footer}>
        <button
          type="button"
          className={styles.rejectButton}
          onClick={onReject}
          disabled={loading}
        >
          {loading ? (
            <Loader2 size={14} className={styles.spinner} />
          ) : (
            <X size={14} />
          )}
          Reject
        </button>
        <button
          type="button"
          className={styles.approveButton}
          onClick={onApprove}
          disabled={loading}
        >
          {loading ? (
            <Loader2 size={14} className={styles.spinner} />
          ) : (
            <Check size={14} />
          )}
          Approve
        </button>
      </div>
    </div>
  );
}

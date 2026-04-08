import { useState } from 'react';
import { formatDistanceToNow } from 'date-fns';
import { useConnectionStatus } from '../../hooks/useConnectionStatus';
import styles from './ConnectionStatus.module.css';

export function ConnectionStatus() {
  const { status, lastConnectedAt, reconnectAttempts } = useConnectionStatus();
  const [showTooltip, setShowTooltip] = useState(false);

  const statusLabel =
    status === 'connected'
      ? 'Connected'
      : status === 'reconnecting'
        ? 'Reconnecting...'
        : 'Offline';

  const dotClass =
    status === 'connected'
      ? styles.connected
      : status === 'reconnecting'
        ? styles.reconnecting
        : styles.disconnected;

  const lastConnectedText = lastConnectedAt
    ? formatDistanceToNow(lastConnectedAt, { addSuffix: true })
    : null;

  return (
    <div
      className={styles.indicator}
      onMouseEnter={() => setShowTooltip(true)}
      onMouseLeave={() => setShowTooltip(false)}
    >
      <span className={`${styles.dot} ${dotClass}`} />
      {status !== 'connected' && <span className={styles.statusText}>{statusLabel}</span>}
      {status === 'disconnected' && (
        <span className={styles.statusText}>- Edits saved locally</span>
      )}
      {showTooltip && (
        <div className={styles.tooltip}>
          <span className={styles.tooltipLine}>{statusLabel}</span>
          {lastConnectedText && (
            <span className={styles.tooltipLine}>Last connected {lastConnectedText}</span>
          )}
          {status === 'reconnecting' && reconnectAttempts > 0 && (
            <span className={styles.tooltipLine}>Attempt {reconnectAttempts}</span>
          )}
        </div>
      )}
    </div>
  );
}

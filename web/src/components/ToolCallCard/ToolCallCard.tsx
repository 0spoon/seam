import { useState, useMemo } from 'react';
import { Loader2, Check, X, ChevronDown, ChevronRight, Wrench } from 'lucide-react';
import { renderToolResult } from './renderers';
import styles from './ToolCallCard.module.css';

interface ToolCallCardProps {
  toolName: string;
  status: 'running' | 'ok' | 'error';
  resultJson?: string;
  errorMessage?: string;
}

export function ToolCallCard({ toolName, status, resultJson, errorMessage }: ToolCallCardProps) {
  const [expanded, setExpanded] = useState(false);

  const parsed = useMemo<unknown>(() => {
    if (!resultJson) return undefined;
    try {
      return JSON.parse(resultJson);
    } catch {
      return resultJson;
    }
  }, [resultJson]);

  const body = parsed !== undefined ? renderToolResult(toolName, parsed) : null;

  let StatusIcon: React.ReactNode;
  let statusClass = '';
  if (status === 'running') {
    StatusIcon = <Loader2 size={14} className={styles.spinner} />;
    statusClass = styles.statusRunning;
  } else if (status === 'error') {
    StatusIcon = <X size={14} />;
    statusClass = styles.statusError;
  } else {
    StatusIcon = <Check size={14} />;
    statusClass = styles.statusOk;
  }

  return (
    <div className={styles.card}>
      <button
        type="button"
        className={styles.header}
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
      >
        <span className={`${styles.statusIcon} ${statusClass}`}>{StatusIcon}</span>
        <Wrench size={12} className={styles.toolIcon} />
        <span className={styles.toolName}>{toolName}</span>
        <span className={styles.caret}>
          {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        </span>
      </button>
      {errorMessage && <div className={styles.errorMessage}>{errorMessage}</div>}
      {expanded && body && <div className={styles.body}>{body}</div>}
    </div>
  );
}

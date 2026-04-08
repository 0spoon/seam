import { useMemo } from 'react';
import styles from './DiffView.module.css';

interface DiffViewProps {
  oldText: string;
  newText: string;
  oldLabel: string;
  newLabel: string;
}

interface DiffLine {
  type: 'added' | 'removed' | 'context';
  content: string;
}

// Simple line-by-line diff using LCS (Longest Common Subsequence).
// This is intentionally minimal to avoid adding external dependencies.
function computeDiff(oldLines: string[], newLines: string[]): DiffLine[] {
  const m = oldLines.length;
  const n = newLines.length;

  // Build LCS table.
  const dp: number[][] = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0));

  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      if (oldLines[i - 1] === newLines[j - 1]) {
        dp[i][j] = dp[i - 1][j - 1] + 1;
      } else {
        dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
      }
    }
  }

  // Backtrack to produce diff.
  const result: DiffLine[] = [];
  let i = m;
  let j = n;

  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && oldLines[i - 1] === newLines[j - 1]) {
      result.unshift({ type: 'context', content: oldLines[i - 1] });
      i--;
      j--;
    } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
      result.unshift({ type: 'added', content: newLines[j - 1] });
      j--;
    } else {
      result.unshift({ type: 'removed', content: oldLines[i - 1] });
      i--;
    }
  }

  return result;
}

export function DiffView({ oldText, newText, oldLabel, newLabel }: DiffViewProps) {
  const diffLines = useMemo(() => {
    const oldLines = oldText.split('\n');
    const newLines = newText.split('\n');
    return computeDiff(oldLines, newLines);
  }, [oldText, newText]);

  const hasChanges = diffLines.some((l) => l.type !== 'context');

  if (!hasChanges) {
    return (
      <div className={styles.container}>
        <div className={styles.noDiff}>No differences</div>
      </div>
    );
  }

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <span className={styles.labelOld}>{oldLabel}</span>
        <span className={styles.arrow}>&rarr;</span>
        <span className={styles.labelNew}>{newLabel}</span>
      </div>
      <div className={styles.lines}>
        {diffLines.map((line, idx) => {
          let className = styles.lineContext;
          let prefix = '  ';
          if (line.type === 'added') {
            className = styles.lineAdded;
            prefix = '+ ';
          } else if (line.type === 'removed') {
            className = styles.lineRemoved;
            prefix = '- ';
          }
          return (
            <div key={idx} className={`${styles.line} ${className}`}>
              {prefix}
              {line.content}
            </div>
          );
        })}
      </div>
    </div>
  );
}

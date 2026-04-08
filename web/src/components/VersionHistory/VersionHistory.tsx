import { useState, useCallback, useEffect } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { History, RotateCcw, ChevronDown } from 'lucide-react';
import { listVersions, getVersion, restoreVersion } from '../../api/client';
import { useToastStore } from '../Toast/ToastContainer';
import { timeAgo } from '../../lib/dates';
import { DiffView } from './DiffView';
import type { NoteVersion } from '../../api/types';
import styles from './VersionHistory.module.css';

interface VersionHistoryProps {
  noteId: string;
  currentBody: string;
  onRestore: () => void;
}

export function VersionHistory({ noteId, currentBody, onRestore }: VersionHistoryProps) {
  const [versions, setVersions] = useState<NoteVersion[]>([]);
  const [total, setTotal] = useState(0);
  const [isExpanded, setIsExpanded] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [selectedVersion, setSelectedVersion] = useState<NoteVersion | null>(null);
  const addToast = useToastStore((s) => s.addToast);

  const fetchVersions = useCallback(async () => {
    setIsLoading(true);
    try {
      const data = await listVersions(noteId);
      setVersions(data.versions);
      setTotal(data.total);
    } catch {
      // Silently fail -- versions are a secondary feature.
    } finally {
      setIsLoading(false);
    }
  }, [noteId]);

  // Reset state when note changes.
  useEffect(() => {
    setVersions([]);
    setTotal(0);
    setIsExpanded(false);
    setSelectedVersion(null);
  }, [noteId]);

  const handleToggle = useCallback(() => {
    const next = !isExpanded;
    setIsExpanded(next);
    if (next && versions.length === 0) {
      fetchVersions();
    }
    if (!next) {
      setSelectedVersion(null);
    }
  }, [isExpanded, versions.length, fetchVersions]);

  const handleVersionClick = useCallback(
    async (v: NoteVersion) => {
      if (selectedVersion?.id === v.id) {
        setSelectedVersion(null);
        return;
      }
      // Fetch full version content if body is empty (list may return truncated).
      try {
        const full = await getVersion(noteId, v.version);
        setSelectedVersion(full);
      } catch {
        addToast('Failed to load version', 'error');
      }
    },
    [noteId, selectedVersion, addToast],
  );

  const handleRestore = useCallback(
    async (version: number, e: React.MouseEvent) => {
      e.stopPropagation();
      try {
        await restoreVersion(noteId, version);
        addToast('Version restored', 'success');
        onRestore();
        fetchVersions();
        setSelectedVersion(null);
      } catch {
        addToast('Failed to restore version', 'error');
      }
    },
    [noteId, onRestore, fetchVersions, addToast],
  );

  return (
    <section className={styles.container}>
      <button className={styles.header} onClick={handleToggle} aria-expanded={isExpanded}>
        <span className={styles.headerLeft}>
          <History size={12} />
          History
          {total > 0 && <span className={styles.badge}>{total}</span>}
        </span>
        <ChevronDown
          size={14}
          className={`${styles.chevron} ${isExpanded ? styles.chevronOpen : ''}`}
        />
      </button>

      <AnimatePresence>
        {isExpanded && (
          <motion.div
            className={styles.content}
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.2, ease: [0.16, 1, 0.3, 1] }}
          >
            {isLoading && <p className={styles.loadingState}>Loading versions...</p>}

            {!isLoading && versions.length === 0 && (
              <p className={styles.emptyState}>No previous versions</p>
            )}

            {!isLoading && versions.length > 0 && (
              <div className={styles.versionList}>
                {versions.map((v) => (
                  <div
                    key={v.id}
                    className={`${styles.versionRow} ${selectedVersion?.id === v.id ? styles.versionRowSelected : ''}`}
                    onClick={() => handleVersionClick(v)}
                    role="button"
                    tabIndex={0}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        handleVersionClick(v);
                      }
                    }}
                  >
                    <div className={styles.versionInfo}>
                      <span className={styles.versionLabel}>v{v.version}</span>
                      <span className={styles.versionTime}>{timeAgo(v.created_at)}</span>
                    </div>
                    <div className={styles.versionActions}>
                      <button
                        className={styles.restoreButton}
                        onClick={(e) => handleRestore(v.version, e)}
                        title="Restore this version"
                        aria-label={`Restore version ${v.version}`}
                      >
                        <RotateCcw size={10} />
                        Restore
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}

            {selectedVersion && (
              <div className={styles.diffContainer}>
                <DiffView
                  oldText={selectedVersion.body}
                  newText={currentBody}
                  oldLabel={`v${selectedVersion.version}`}
                  newLabel="Current"
                />
              </div>
            )}
          </motion.div>
        )}
      </AnimatePresence>
    </section>
  );
}

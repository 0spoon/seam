import { useState, useCallback, useRef } from 'react';
import { X, Loader2 } from 'lucide-react';
import { synthesize } from '../../api/client';
import { renderMarkdown } from '../../lib/markdown';
import { sanitizeHtml } from '../../lib/sanitize';
import styles from './SynthesisModal.module.css';

interface SynthesisModalProps {
  scope: 'project' | 'tag';
  projectId?: string;
  tag?: string;
  title: string;
  onClose: () => void;
}

export function SynthesisModal({
  scope,
  projectId,
  tag,
  title,
  onClose,
}: SynthesisModalProps) {
  const backdropRef = useRef<HTMLDivElement>(null);
  const [prompt, setPrompt] = useState('Summarize the key themes and ideas');
  const [response, setResponse] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState('');

  const handleSynthesize = async () => {
    if (!prompt.trim()) return;
    setIsLoading(true);
    setError('');
    setResponse('');

    try {
      const result = await synthesize({
        scope,
        project_id: projectId,
        tag,
        prompt: prompt.trim(),
      });
      setResponse(result.response);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : 'Synthesis failed',
      );
    } finally {
      setIsLoading(false);
    }
  };

  // Focus trap: keep Tab cycling within the modal.
  const handleFocusTrap = useCallback((e: React.KeyboardEvent) => {
    if (e.key !== 'Tab') return;
    const modal = backdropRef.current?.querySelector('[class*="modal"]') as HTMLElement | null;
    if (!modal) return;
    const focusable = modal.querySelectorAll<HTMLElement>(
      'button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
    );
    if (focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }, []);

  const handleBackdropClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) {
      onClose();
    }
  };

  return (
    <div ref={backdropRef} className={styles.backdrop} onClick={handleBackdropClick} onKeyDown={handleFocusTrap}>
      <div className={styles.modal}>
        <div className={styles.header}>
          <h2 className={styles.title}>{title}</h2>
          <button
            className={styles.closeButton}
            onClick={onClose}
            aria-label="Close"
          >
            <X size={16} />
          </button>
        </div>

        <div className={styles.body}>
          <div className={styles.promptRow}>
            <input
              type="text"
              className={styles.promptInput}
              placeholder="What would you like to know?"
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !isLoading) {
                  handleSynthesize();
                }
              }}
              disabled={isLoading}
              autoFocus
            />
            <button
              className={styles.synthesizeButton}
              onClick={handleSynthesize}
              disabled={isLoading || !prompt.trim()}
            >
              {isLoading ? (
                <Loader2 size={14} className={styles.spinner} />
              ) : (
                'Generate'
              )}
            </button>
          </div>

          {error && <p className={styles.error}>{error}</p>}

          {response && (
            <div
              className={styles.response}
              dangerouslySetInnerHTML={{ __html: sanitizeHtml(renderMarkdown(response)) }}
            />
          )}

          {isLoading && !response && (
            <div className={styles.loadingState}>
              <Loader2 size={20} className={styles.spinner} />
              <span>Synthesizing your notes...</span>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

import { useEffect, useRef } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import styles from './ConfirmModal.module.css';

interface ConfirmModalProps {
  open: boolean;
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  destructive?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
  children?: React.ReactNode;
}

export function ConfirmModal({
  open,
  title,
  message,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  destructive = false,
  onConfirm,
  onCancel,
  children,
}: ConfirmModalProps) {
  const confirmRef = useRef<HTMLButtonElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (open) {
      previousFocusRef.current = document.activeElement as HTMLElement | null;
      setTimeout(() => confirmRef.current?.focus(), 50);
    }
  }, [open]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      onCancel();
    }
  };

  return (
    <AnimatePresence onExitComplete={() => {
      previousFocusRef.current?.focus();
      previousFocusRef.current = null;
    }}>
      {open && (
        <motion.div
          className={styles.backdrop}
          onClick={(e) => { if (e.target === e.currentTarget) onCancel(); }}
          onKeyDown={handleKeyDown}
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.25, ease: [0.4, 0, 1, 1] }}
        >
          <motion.div
            className={styles.modal}
            role="alertdialog"
            aria-modal="true"
            aria-label={title}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 4 }}
            transition={{ duration: 0.25, ease: [0.16, 1, 0.3, 1] }}
          >
            <h2 className={styles.title}>{title}</h2>
            <p className={styles.message}>{message}</p>
            {children}
            <div className={styles.actions}>
              <button className={styles.cancelButton} onClick={onCancel}>
                {cancelLabel}
              </button>
              <button
                ref={confirmRef}
                className={destructive ? styles.destructiveButton : styles.confirmButton}
                onClick={onConfirm}
              >
                {confirmLabel}
              </button>
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

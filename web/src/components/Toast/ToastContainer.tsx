import { create } from 'zustand';
import { AnimatePresence, motion } from 'motion/react';
import styles from './Toast.module.css';

interface Toast {
  id: string;
  message: string;
  type: 'info' | 'error' | 'success';
}

interface ToastState {
  toasts: Toast[];
  addToast: (message: string, type?: Toast['type']) => void;
  removeToast: (id: string) => void;
}

// Store is co-located with its consuming component; 23 imports across the app
// rely on this re-export, splitting it out is not worth the churn.
// eslint-disable-next-line react-refresh/only-export-components
export const useToastStore = create<ToastState>((set) => ({
  toasts: [],
  addToast: (message, type = 'info') => {
    const id = crypto.randomUUID();
    set((state) => ({
      toasts: [...state.toasts.slice(-2), { id, message, type }],
    }));
    // Auto-dismiss after 4 seconds
    setTimeout(() => {
      set((state) => ({
        toasts: state.toasts.filter((t) => t.id !== id),
      }));
    }, 4000);
  },
  removeToast: (id) => {
    set((state) => ({
      toasts: state.toasts.filter((t) => t.id !== id),
    }));
  },
}));

export function ToastContainer() {
  const toasts = useToastStore((s) => s.toasts);

  return (
    <div className={styles.container} role="status" aria-live="polite">
      <AnimatePresence mode="popLayout">
        {toasts.map((toast) => (
          <ToastItem key={toast.id} toast={toast} />
        ))}
      </AnimatePresence>
    </div>
  );
}

function ToastItem({ toast }: { toast: Toast }) {
  const removeToast = useToastStore((s) => s.removeToast);

  return (
    <motion.div
      layout
      className={`${styles.toast} ${styles[toast.type]}`}
      onClick={() => removeToast(toast.id)}
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: -8 }}
      transition={{ duration: 0.15, ease: [0.16, 1, 0.3, 1] }}
    >
      {toast.message}
    </motion.div>
  );
}

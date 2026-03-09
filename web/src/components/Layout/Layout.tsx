import { useMemo } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { AnimatePresence, motion } from 'motion/react';
import { Menu } from 'lucide-react';
import { Sidebar } from '../Sidebar/Sidebar';
import { CommandPalette } from '../CommandPalette/CommandPalette';
import { CaptureModal } from '../Modal/CaptureModal';
import { ToastContainer } from '../Toast/ToastContainer';
import { useUIStore } from '../../stores/uiStore';
import { useKeyboard } from '../../hooks/useKeyboard';
import styles from './Layout.module.css';

export function Layout() {
  const location = useLocation();
  const sidebarCollapsed = useUIStore((s) => s.sidebarCollapsed);
  const sidebarOpen = useUIStore((s) => s.sidebarOpen);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const setCommandPaletteOpen = useUIStore((s) => s.setCommandPaletteOpen);
  const setCaptureModalOpen = useUIStore((s) => s.setCaptureModalOpen);
  const toggleSidebar = useUIStore((s) => s.toggleSidebar);

  const keyBindings = useMemo(() => [
    {
      key: 'k',
      meta: true,
      handler: () => setCommandPaletteOpen(true),
    },
    {
      key: 'n',
      meta: true,
      handler: () => setCaptureModalOpen(true),
    },
    {
      key: '\\',
      meta: true,
      handler: () => toggleSidebar(),
    },
  ], [setCommandPaletteOpen, setCaptureModalOpen, toggleSidebar]);

  useKeyboard(keyBindings);

  return (
    <div className={styles.layout}>
      <a href="#main-content" className="skipToContent">
        Skip to content
      </a>

      {/* Hamburger button for mobile (visible <640px) */}
      <button
        className={styles.hamburger}
        onClick={() => setSidebarOpen(true)}
        aria-label="Open navigation"
      >
        <Menu size={20} />
      </button>

      {/* Mobile backdrop (visible <640px when sidebar open) */}
      {sidebarOpen && (
        <div
          className={styles.mobileBackdrop}
          onClick={() => setSidebarOpen(false)}
          aria-hidden="true"
        />
      )}

      <Sidebar />
      <main
        id="main-content"
        className={styles.main}
        style={{
          marginLeft: sidebarCollapsed
            ? 'var(--sidebar-collapsed)'
            : 'var(--sidebar-width)',
        }}
      >
        <AnimatePresence mode="wait">
          <motion.div
            key={location.pathname}
            className={styles.pageTransition}
            initial={{ opacity: 0, y: 4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 4 }}
            transition={{ duration: 0.25, ease: [0.16, 1, 0.3, 1] }}
          >
            <Outlet />
          </motion.div>
        </AnimatePresence>
      </main>
      <CommandPalette />
      <CaptureModal />
      <ToastContainer />
    </div>
  );
}

import { Outlet } from 'react-router-dom';
import { Sidebar } from '../Sidebar/Sidebar';
import { CommandPalette } from '../CommandPalette/CommandPalette';
import { CaptureModal } from '../Modal/CaptureModal';
import { ToastContainer } from '../Toast/ToastContainer';
import { useUIStore } from '../../stores/uiStore';
import { useKeyboard } from '../../hooks/useKeyboard';
import styles from './Layout.module.css';

export function Layout() {
  const sidebarCollapsed = useUIStore((s) => s.sidebarCollapsed);
  const setCommandPaletteOpen = useUIStore((s) => s.setCommandPaletteOpen);
  const setCaptureModalOpen = useUIStore((s) => s.setCaptureModalOpen);
  const toggleSidebar = useUIStore((s) => s.toggleSidebar);

  useKeyboard([
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
  ]);

  return (
    <div className={styles.layout}>
      <a href="#main-content" className="skipToContent">
        Skip to content
      </a>
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
        <Outlet />
      </main>
      <CommandPalette />
      <CaptureModal />
      <ToastContainer />
    </div>
  );
}

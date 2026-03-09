import { useEffect, useRef, useState, useCallback } from 'react';
import { X, Type } from 'lucide-react';
import styles from './ZenModeExit.module.css';

interface ZenModeExitProps {
  onExit: () => void;
  typewriterEnabled: boolean;
  onToggleTypewriter: () => void;
}

export function ZenModeExit({ onExit, typewriterEnabled, onToggleTypewriter }: ZenModeExitProps) {
  const [visible, setVisible] = useState(true);
  const timerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const resetTimer = useCallback(() => {
    setVisible(true);
    if (timerRef.current) {
      clearTimeout(timerRef.current);
    }
    timerRef.current = setTimeout(() => {
      setVisible(false);
    }, 2000);
  }, []);

  useEffect(() => {
    // Start the initial fade timer.
    timerRef.current = setTimeout(() => {
      setVisible(false);
    }, 2000);

    const handleMouseMove = () => {
      resetTimer();
    };

    document.addEventListener('mousemove', handleMouseMove);
    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }
    };
  }, [resetTimer]);

  return (
    <div
      className={styles.container}
      style={{ opacity: visible ? 1 : 0.3 }}
    >
      <button
        className={`${styles.typewriterToggle} ${typewriterEnabled ? styles.typewriterActive : ''}`}
        onClick={onToggleTypewriter}
        title="Toggle typewriter scrolling"
        aria-label="Toggle typewriter scrolling"
      >
        <Type size={12} />
      </button>
      <button className={styles.exitButton} onClick={onExit}>
        Exit
        <X size={12} />
      </button>
    </div>
  );
}

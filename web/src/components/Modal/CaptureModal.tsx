import { useState, useRef, useEffect } from 'react';
import { X } from 'lucide-react';
import { useUIStore } from '../../stores/uiStore';
import { useNoteStore } from '../../stores/noteStore';
import { useProjectStore } from '../../stores/projectStore';
import { useNavigate } from 'react-router-dom';
import styles from './Modal.module.css';

export function CaptureModal() {
  const isOpen = useUIStore((s) => s.captureModalOpen);
  const setOpen = useUIStore((s) => s.setCaptureModalOpen);
  const projects = useProjectStore((s) => s.projects);
  const createNote = useNoteStore((s) => s.createNote);
  const navigate = useNavigate();

  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [projectId, setProjectId] = useState('');
  const [tagInput, setTagInput] = useState('');
  const [isSaving, setIsSaving] = useState(false);

  const bodyRef = useRef<HTMLTextAreaElement>(null);
  const backdropRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (isOpen) {
      // Auto-focus body textarea
      setTimeout(() => bodyRef.current?.focus(), 100);
    } else {
      // Reset form
      setTitle('');
      setBody('');
      setProjectId('');
      setTagInput('');
    }
  }, [isOpen]);

  const handleSave = async () => {
    if (!body.trim() && !title.trim()) return;

    setIsSaving(true);
    try {
      const tags = tagInput
        .split(',')
        .map((t) => t.trim().replace(/^#/, ''))
        .filter(Boolean);

      const note = await createNote({
        title: title.trim() || 'Untitled',
        body: body.trim(),
        project_id: projectId || undefined,
        tags: tags.length > 0 ? tags : undefined,
      });

      setOpen(false);
      navigate(`/notes/${note.id}`);
    } catch {
      // Error is handled by the store
    } finally {
      setIsSaving(false);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
      e.preventDefault();
      handleSave();
    }
    if (e.key === 'Escape') {
      if (body.trim() || title.trim()) {
        if (!window.confirm('You have unsaved content. Discard it?')) {
          return;
        }
      }
      setOpen(false);
    }
  };

  const handleBackdropClick = (e: React.MouseEvent) => {
    if (e.target === backdropRef.current) {
      setOpen(false);
    }
  };

  if (!isOpen) return null;

  return (
    <div
      ref={backdropRef}
      className={styles.backdrop}
      onClick={handleBackdropClick}
      onKeyDown={handleKeyDown}
      role="dialog"
      aria-modal="true"
      aria-label="Quick capture"
    >
      <div className={styles.modal} style={{ maxWidth: 'var(--modal-width-sm)' }}>
        <div className={styles.modalHeader}>
          <h2 className={styles.modalTitle}>Quick Capture</h2>
          <button
            className={styles.closeButton}
            onClick={() => setOpen(false)}
            aria-label="Close"
          >
            <X size={16} />
          </button>
        </div>

        <div className={styles.modalBody}>
          <input
            type="text"
            className={styles.titleInput}
            placeholder="Title (optional)"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
          />
          <textarea
            ref={bodyRef}
            className={styles.bodyTextarea}
            placeholder="Write your thought..."
            value={body}
            onChange={(e) => setBody(e.target.value)}
          />
          <div className={styles.captureOptions}>
            <select
              className={styles.projectSelect}
              value={projectId}
              onChange={(e) => setProjectId(e.target.value)}
              aria-label="Project"
            >
              <option value="">Inbox</option>
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            <input
              type="text"
              className={styles.tagInput}
              placeholder="Tags (comma-separated)"
              value={tagInput}
              onChange={(e) => setTagInput(e.target.value)}
            />
          </div>
        </div>

        <div className={styles.modalFooter}>
          <button
            className={styles.ghostButton}
            onClick={() => setOpen(false)}
          >
            Cancel
          </button>
          <button
            className={styles.primaryButton}
            onClick={handleSave}
            disabled={isSaving || (!body.trim() && !title.trim())}
          >
            {isSaving ? 'Saving...' : 'Save to Inbox'}
          </button>
        </div>
      </div>
    </div>
  );
}

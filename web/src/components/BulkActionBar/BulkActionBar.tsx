import { useState, useEffect, useCallback, useRef } from 'react';
import { Tag, FolderInput, Trash2, X } from 'lucide-react';
import { useNoteStore } from '../../stores/noteStore';
import { useProjectStore } from '../../stores/projectStore';
import { useUIStore } from '../../stores/uiStore';
import { useToastStore } from '../Toast/ToastContainer';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import styles from './BulkActionBar.module.css';

export function BulkActionBar() {
  const selectedNoteIds = useNoteStore((s) => s.selectedNoteIds);
  const clearSelection = useNoteStore((s) => s.clearSelection);
  const bulkAction = useNoteStore((s) => s.bulkAction);
  const projects = useProjectStore((s) => s.projects);
  const tags = useUIStore((s) => s.tags);
  const addToast = useToastStore((s) => s.addToast);

  const count = selectedNoteIds.size;

  const [tagDropdownOpen, setTagDropdownOpen] = useState(false);
  const [projectDropdownOpen, setProjectDropdownOpen] = useState(false);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [newTag, setNewTag] = useState('');

  const tagDropdownRef = useRef<HTMLDivElement>(null);
  const projectDropdownRef = useRef<HTMLDivElement>(null);
  const tagInputRef = useRef<HTMLInputElement>(null);

  // Close dropdowns when clicking outside.
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (
        tagDropdownRef.current &&
        !tagDropdownRef.current.contains(e.target as Node)
      ) {
        setTagDropdownOpen(false);
      }
      if (
        projectDropdownRef.current &&
        !projectDropdownRef.current.contains(e.target as Node)
      ) {
        setProjectDropdownOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  // Focus tag input when dropdown opens.
  useEffect(() => {
    if (tagDropdownOpen) {
      setTimeout(() => tagInputRef.current?.focus(), 50);
    }
  }, [tagDropdownOpen]);

  const handleAddTag = useCallback(
    async (tag: string) => {
      if (!tag.trim()) return;
      const result = await bulkAction('add_tag', { tag: tag.trim() });
      if (result) {
        addToast(`Tag added to ${result.success} note${result.success !== 1 ? 's' : ''}`, 'success');
      }
      setTagDropdownOpen(false);
      setNewTag('');
    },
    [bulkAction, addToast],
  );

  const handleRemoveTag = useCallback(
    async (tag: string) => {
      const result = await bulkAction('remove_tag', { tag });
      if (result) {
        addToast(`Tag removed from ${result.success} note${result.success !== 1 ? 's' : ''}`, 'success');
      }
      setTagDropdownOpen(false);
    },
    [bulkAction, addToast],
  );

  const handleMoveToProject = useCallback(
    async (projectId: string) => {
      const result = await bulkAction('move', { project_id: projectId });
      if (result) {
        addToast(`${result.success} note${result.success !== 1 ? 's' : ''} moved`, 'success');
      }
      setProjectDropdownOpen(false);
    },
    [bulkAction, addToast],
  );

  const handleDelete = useCallback(async () => {
    const result = await bulkAction('delete');
    if (result) {
      addToast(`${result.success} note${result.success !== 1 ? 's' : ''} deleted`, 'success');
    }
    setDeleteConfirmOpen(false);
  }, [bulkAction, addToast]);

  if (count === 0) return null;

  return (
    <>
      <div className={styles.bar}>
        <span className={styles.count}>
          {count} note{count !== 1 ? 's' : ''} selected
        </span>

        <div className={styles.actions}>
          <div className={styles.dropdownWrapper} ref={tagDropdownRef}>
            <button
              className={styles.actionBtn}
              onClick={() => {
                setTagDropdownOpen(!tagDropdownOpen);
                setProjectDropdownOpen(false);
              }}
            >
              <Tag size={14} />
              <span>Tag</span>
            </button>
            {tagDropdownOpen && (
              <div className={styles.dropdown}>
                <div className={styles.dropdownSection}>
                  <span className={styles.dropdownLabel}>Add tag</span>
                  <input
                    ref={tagInputRef}
                    className={styles.dropdownInput}
                    placeholder="New tag..."
                    value={newTag}
                    onChange={(e) => setNewTag(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && newTag.trim()) {
                        handleAddTag(newTag);
                      }
                      if (e.key === 'Escape') {
                        setTagDropdownOpen(false);
                      }
                    }}
                  />
                  {tags.map((t) => (
                    <button
                      key={t.name}
                      className={styles.dropdownItem}
                      onClick={() => handleAddTag(t.name)}
                    >
                      + {t.name}
                      <span className={styles.dropdownCount}>{t.count}</span>
                    </button>
                  ))}
                </div>
                {tags.length > 0 && (
                  <div className={styles.dropdownSection}>
                    <span className={styles.dropdownLabel}>Remove tag</span>
                    {tags.map((t) => (
                      <button
                        key={`rm-${t.name}`}
                        className={styles.dropdownItemDanger}
                        onClick={() => handleRemoveTag(t.name)}
                      >
                        - {t.name}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>

          <div className={styles.dropdownWrapper} ref={projectDropdownRef}>
            <button
              className={styles.actionBtn}
              onClick={() => {
                setProjectDropdownOpen(!projectDropdownOpen);
                setTagDropdownOpen(false);
              }}
            >
              <FolderInput size={14} />
              <span>Move</span>
            </button>
            {projectDropdownOpen && (
              <div className={styles.dropdown}>
                <button
                  className={styles.dropdownItem}
                  onClick={() => handleMoveToProject('')}
                >
                  Inbox
                </button>
                {projects.map((p) => (
                  <button
                    key={p.id}
                    className={styles.dropdownItem}
                    onClick={() => handleMoveToProject(p.id)}
                  >
                    {p.name}
                  </button>
                ))}
              </div>
            )}
          </div>

          <button
            className={styles.deleteBtn}
            onClick={() => setDeleteConfirmOpen(true)}
          >
            <Trash2 size={14} />
            <span>Delete</span>
          </button>

          <button className={styles.cancelBtn} onClick={clearSelection}>
            <X size={14} />
            <span>Cancel</span>
          </button>
        </div>
      </div>

      <ConfirmModal
        open={deleteConfirmOpen}
        title="Delete notes"
        message={`Delete ${count} note${count !== 1 ? 's' : ''}? This cannot be undone.`}
        confirmLabel="Delete"
        destructive
        onConfirm={handleDelete}
        onCancel={() => setDeleteConfirmOpen(false)}
      />
    </>
  );
}

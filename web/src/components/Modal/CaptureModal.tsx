import { useState, useRef, useEffect, useCallback } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { X, Link, Mic, MicOff, FileText } from 'lucide-react';
import { useUIStore } from '../../stores/uiStore';
import { useNoteStore } from '../../stores/noteStore';
import { useProjectStore } from '../../stores/projectStore';
import { useToastStore } from '../../components/Toast/ToastContainer';
import { useNavigate } from 'react-router-dom';
import { captureURL, captureVoice, listTemplates, applyTemplate } from '../../api/client';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import type { TemplateMeta } from '../../api/types';
import styles from './Modal.module.css';

function isURL(text: string): boolean {
  const trimmed = text.trim();
  return /^https?:\/\//i.test(trimmed);
}

export function CaptureModal() {
  const isOpen = useUIStore((s) => s.captureModalOpen);
  const setOpen = useUIStore((s) => s.setCaptureModalOpen);
  const defaultProjectId = useUIStore((s) => s.captureDefaultProjectId);
  const projects = useProjectStore((s) => s.projects);
  const createNote = useNoteStore((s) => s.createNote);
  const addToast = useToastStore((s) => s.addToast);
  const navigate = useNavigate();
  const modalRef = useRef<HTMLDivElement>(null);

  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [projectId, setProjectId] = useState('');
  const [tagInput, setTagInput] = useState('');
  const [isSaving, setIsSaving] = useState(false);
  const [isRecording, setIsRecording] = useState(false);
  const [urlMode, setUrlMode] = useState(false);
  const [templates, setTemplates] = useState<TemplateMeta[]>([]);
  const [selectedTemplate, setSelectedTemplate] = useState('');
  const [showTemplatePicker, setShowTemplatePicker] = useState(false);
  const [confirmState, setConfirmState] = useState<{
    open: boolean;
    title: string;
    message: string;
    onConfirm: () => void;
  }>({ open: false, title: '', message: '', onConfirm: () => {} });
  const mediaRecorderRef = useRef<MediaRecorder | null>(null);
  const chunksRef = useRef<Blob[]>([]);

  const bodyRef = useRef<HTMLTextAreaElement>(null);
  const backdropRef = useRef<HTMLDivElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (isOpen) {
      // Save focus for restoration on close.
      previousFocusRef.current = document.activeElement as HTMLElement | null;
      // Pre-select the default project if provided (e.g. from ProjectPage).
      setProjectId(defaultProjectId);
      // Auto-focus body textarea
      setTimeout(() => bodyRef.current?.focus(), 100);
      // Load available templates.
      listTemplates().then(setTemplates).catch(() => setTemplates([]));
    } else {
      // Reset form
      setTitle('');
      setBody('');
      setProjectId('');
      setTagInput('');
      setUrlMode(false);
      setIsRecording(false);
      setSelectedTemplate('');
      setShowTemplatePicker(false);
      if (mediaRecorderRef.current?.state === 'recording') {
        mediaRecorderRef.current.stop();
      }
    }
  }, [isOpen, defaultProjectId]);

  // Detect URL paste in body field.
  useEffect(() => {
    setUrlMode(isURL(body));
  }, [body]);

  const handleSave = async () => {
    if (!body.trim() && !title.trim()) return;

    setIsSaving(true);
    try {
      // URL capture mode: if the body looks like a URL, use the capture endpoint.
      if (urlMode && isURL(body)) {
        const note = await captureURL(body.trim());
        addToast('URL captured', 'success');
        setOpen(false);
        navigate(`/notes/${note.id}`);
        return;
      }

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
      // Error toast is handled by the noteStore
    } finally {
      setIsSaving(false);
    }
  };

  const handleToggleRecording = async () => {
    if (isRecording) {
      // Stop recording.
      mediaRecorderRef.current?.stop();
      setIsRecording(false);
      return;
    }

    // Check browser compatibility before attempting to record.
    if (typeof MediaRecorder === 'undefined') {
      addToast('Voice recording is not supported in this browser', 'error');
      return;
    }
    if (!MediaRecorder.isTypeSupported('audio/webm')) {
      addToast('audio/webm recording is not supported in this browser', 'error');
      return;
    }

    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      const recorder = new MediaRecorder(stream, { mimeType: 'audio/webm' });
      chunksRef.current = [];

      recorder.ondataavailable = (e) => {
        if (e.data.size > 0) {
          chunksRef.current.push(e.data);
        }
      };

      recorder.onstop = async () => {
        stream.getTracks().forEach((t) => t.stop());
        const blob = new Blob(chunksRef.current, { type: 'audio/webm' });
        if (blob.size === 0) return;

        setIsSaving(true);
        try {
          const note = await captureVoice(blob, 'recording.webm');
          addToast('Voice note captured', 'success');
          setOpen(false);
          navigate(`/notes/${note.id}`);
        } catch {
          addToast('Failed to save voice recording', 'error');
        } finally {
          setIsSaving(false);
        }
      };

      mediaRecorderRef.current = recorder;
      recorder.start();
      setIsRecording(true);
    } catch (err) {
      const message =
        err instanceof DOMException && err.name === 'NotAllowedError'
          ? 'Microphone permission denied'
          : 'Failed to access microphone';
      addToast(message, 'error');
    }
  };

  const applyTemplateByName = useCallback(async (name: string) => {
    try {
      const vars: Record<string, string> = {};
      if (title.trim()) vars.title = title.trim();
      const projectName = projects.find((p) => p.id === projectId)?.name;
      if (projectName) vars.project = projectName;
      const result = await applyTemplate(name, vars);
      setBody(result.body);
    } catch {
      addToast('Failed to apply template', 'error');
    }
  }, [title, projectId, projects, addToast]);

  const handleSelectTemplate = (name: string) => {
    setSelectedTemplate(name);
    setShowTemplatePicker(false);
    if (!name) return;

    if (body.trim()) {
      setConfirmState({
        open: true,
        title: 'Replace content',
        message: 'This will replace your current text with the template. Continue?',
        onConfirm: () => {
          setConfirmState((s) => ({ ...s, open: false }));
          applyTemplateByName(name);
        },
      });
    } else {
      applyTemplateByName(name);
    }
  };

  const confirmDiscardAndClose = useCallback(() => {
    if (body.trim() || title.trim()) {
      setConfirmState({
        open: true,
        title: 'Discard changes',
        message: 'You have unsaved content. Discard it?',
        onConfirm: () => {
          setConfirmState((s) => ({ ...s, open: false }));
          setOpen(false);
        },
      });
    } else {
      setOpen(false);
    }
  }, [body, title, setOpen]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
      e.preventDefault();
      handleSave();
    }
    if (e.key === 'Escape') {
      confirmDiscardAndClose();
    }
  };

  // Focus trap: keep Tab cycling within the modal using a direct ref
  // instead of fragile CSS class name selectors.
  const handleFocusTrap = useCallback((e: React.KeyboardEvent) => {
    if (e.key !== 'Tab') return;
    const container = modalRef.current;
    if (!container) return;
    const focusable = container.querySelectorAll<HTMLElement>(
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
    if (e.target === backdropRef.current) {
      confirmDiscardAndClose();
    }
  };

  return (
    <>
    <AnimatePresence onExitComplete={() => {
      previousFocusRef.current?.focus();
      previousFocusRef.current = null;
    }}>
      {isOpen && (
        <motion.div
          ref={backdropRef}
          className={styles.backdrop}
          onClick={handleBackdropClick}
          onKeyDown={(e) => { handleFocusTrap(e); handleKeyDown(e); }}
          role="dialog"
          aria-modal="true"
          aria-label="Quick capture"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.25, ease: [0.4, 0, 1, 1] }}
        >
          <motion.div
            ref={modalRef}
            className={styles.modal}
            style={{ maxWidth: 'var(--modal-width-sm)' }}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 4 }}
            transition={{ duration: 0.25, ease: [0.16, 1, 0.3, 1] }}
          >
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
          {urlMode && (
            <div className={styles.urlModeBanner}>
              <Link size={14} />
              <span>URL detected - will fetch and save page content</span>
            </div>
          )}
          {!urlMode && (
            <input
              type="text"
              className={styles.titleInput}
              placeholder="Title (optional)"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
            />
          )}
          <textarea
            ref={bodyRef}
            className={styles.bodyTextarea}
            placeholder={urlMode ? 'Paste a URL to capture...' : 'Write your thought...'}
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
          {!urlMode && templates.length > 0 && (
            <div className={styles.templateSection}>
              <button
                className={styles.templateToggle}
                onClick={() => setShowTemplatePicker(!showTemplatePicker)}
                type="button"
              >
                <FileText size={14} />
                {selectedTemplate
                  ? `Template: ${selectedTemplate}`
                  : 'Use a template'}
              </button>
              {showTemplatePicker && (
                <div className={styles.templateList}>
                  {selectedTemplate && (
                    <button
                      className={styles.templateItem}
                      onClick={() => handleSelectTemplate('')}
                    >
                      None (blank note)
                    </button>
                  )}
                  {templates.map((t) => (
                    <button
                      key={t.name}
                      className={`${styles.templateItem} ${
                        t.name === selectedTemplate ? styles.templateItemActive : ''
                      }`}
                      onClick={() => handleSelectTemplate(t.name)}
                    >
                      <span className={styles.templateName}>{t.name}</span>
                      <span className={styles.templateDesc}>{t.description}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        <div className={styles.modalFooter}>
          <div className={styles.footerLeft}>
            <button
              className={styles.ghostButton}
              onClick={handleToggleRecording}
              title={isRecording ? 'Stop recording' : 'Record voice note'}
              aria-label={isRecording ? 'Stop recording' : 'Record voice note'}
            >
              {isRecording ? <MicOff size={16} /> : <Mic size={16} />}
            </button>
          </div>
          <div className={styles.footerRight}>
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
              {isSaving ? 'Saving...' : urlMode ? 'Capture URL' : 'Save to Inbox'}
            </button>
          </div>
        </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>

    <ConfirmModal
      open={confirmState.open}
      title={confirmState.title}
      message={confirmState.message}
      confirmLabel="Discard"
      destructive
      onConfirm={confirmState.onConfirm}
      onCancel={() => setConfirmState((s) => ({ ...s, open: false }))}
    />
    </>
  );
}

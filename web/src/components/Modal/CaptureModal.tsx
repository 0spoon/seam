import { useState, useRef, useEffect } from 'react';
import { X, Link, Mic, MicOff, FileText } from 'lucide-react';
import { useUIStore } from '../../stores/uiStore';
import { useNoteStore } from '../../stores/noteStore';
import { useProjectStore } from '../../stores/projectStore';
import { useNavigate } from 'react-router-dom';
import { captureURL, captureVoice, listTemplates, applyTemplate } from '../../api/client';
import type { TemplateMeta } from '../../api/types';
import styles from './Modal.module.css';

function isURL(text: string): boolean {
  const trimmed = text.trim();
  return /^https?:\/\//i.test(trimmed);
}

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
  const [isRecording, setIsRecording] = useState(false);
  const [urlMode, setUrlMode] = useState(false);
  const [templates, setTemplates] = useState<TemplateMeta[]>([]);
  const [selectedTemplate, setSelectedTemplate] = useState('');
  const [showTemplatePicker, setShowTemplatePicker] = useState(false);
  const mediaRecorderRef = useRef<MediaRecorder | null>(null);
  const chunksRef = useRef<Blob[]>([]);

  const bodyRef = useRef<HTMLTextAreaElement>(null);
  const backdropRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (isOpen) {
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
  }, [isOpen]);

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
      // Error is handled by the store
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

    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      const recorder = new MediaRecorder(stream);
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
          setOpen(false);
          navigate(`/notes/${note.id}`);
        } catch {
          // Error handling
        } finally {
          setIsSaving(false);
        }
      };

      mediaRecorderRef.current = recorder;
      recorder.start();
      setIsRecording(true);
    } catch {
      // Microphone permission denied or unavailable
    }
  };

  const handleSelectTemplate = async (name: string) => {
    setSelectedTemplate(name);
    setShowTemplatePicker(false);
    if (!name) return;
    try {
      const vars: Record<string, string> = {};
      if (title.trim()) vars.title = title.trim();
      const projectName = projects.find((p) => p.id === projectId)?.name;
      if (projectName) vars.project = projectName;
      const result = await applyTemplate(name, vars);
      setBody(result.body);
    } catch {
      // Failed to apply template, keep current body
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
      </div>
    </div>
  );
}

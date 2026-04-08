import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Tag, FolderOpen, Link2, X, Loader2 } from 'lucide-react';
import { suggestTags, suggestProject, updateNote, getNote } from '../../api/client';
import { useToastStore } from '../../components/Toast/ToastContainer';
import type { ReviewItem, TagSuggestion, ProjectSuggestion } from '../../api/types';
import styles from './ReviewCard.module.css';

interface ReviewCardProps {
  item: ReviewItem;
  onDismiss: () => void;
  onAction: (type: 'linked' | 'tagged' | 'moved') => void;
}

const badgeLabels: Record<string, string> = {
  orphan: 'No connections',
  untagged: 'No tags',
  inbox: 'No project',
  similar_pair: 'Similar notes',
};

const badgeStyles: Record<string, string> = {
  orphan: styles.badgeOrphan,
  untagged: styles.badgeUntagged,
  inbox: styles.badgeInbox,
  similar_pair: styles.badgeSimilar,
};

export function ReviewCard({ item, onDismiss, onAction }: ReviewCardProps) {
  const navigate = useNavigate();
  const addToast = useToastStore((s) => s.addToast);
  const [tagSuggestions, setTagSuggestions] = useState<TagSuggestion[]>([]);
  const [projectSuggestions, setProjectSuggestions] = useState<ProjectSuggestion[]>([]);
  const [isLoadingTags, setIsLoadingTags] = useState(false);
  const [isLoadingProjects, setIsLoadingProjects] = useState(false);
  const [isApplying, setIsApplying] = useState(false);

  const handleSuggestTags = async () => {
    setIsLoadingTags(true);
    try {
      const result = await suggestTags(item.note_id);
      setTagSuggestions(result.tags);
      if (result.tags.length === 0) {
        addToast('No tag suggestions available', 'info');
      }
    } catch {
      addToast('Failed to get tag suggestions', 'error');
    } finally {
      setIsLoadingTags(false);
    }
  };

  const handleSuggestProject = async () => {
    setIsLoadingProjects(true);
    try {
      const result = await suggestProject(item.note_id);
      setProjectSuggestions(result.projects);
      if (result.projects.length === 0) {
        addToast('No project suggestions available', 'info');
      }
    } catch {
      addToast('Failed to get project suggestions', 'error');
    } finally {
      setIsLoadingProjects(false);
    }
  };

  const handleApplyTag = async (tagName: string) => {
    setIsApplying(true);
    try {
      const note = await getNote(item.note_id);
      const currentTags = note.tags || [];
      if (currentTags.includes(tagName)) {
        addToast(`Tag "${tagName}" already applied`, 'info');
        setIsApplying(false);
        return;
      }
      await updateNote(item.note_id, { tags: [...currentTags, tagName] });
      addToast(`Added tag "${tagName}"`, 'success');
      onAction('tagged');
      onDismiss();
    } catch {
      addToast('Failed to apply tag', 'error');
    } finally {
      setIsApplying(false);
    }
  };

  const handleMoveToProject = async (projectId: string, projectName: string) => {
    setIsApplying(true);
    try {
      await updateNote(item.note_id, { project_id: projectId });
      addToast(`Moved to "${projectName}"`, 'success');
      onAction('moved');
      onDismiss();
    } catch {
      addToast('Failed to move note', 'error');
    } finally {
      setIsApplying(false);
    }
  };

  return (
    <div className={styles.card}>
      <div className={styles.header}>
        <button className={styles.titleLink} onClick={() => navigate(`/notes/${item.note_id}`)}>
          {item.note_title || 'Untitled'}
        </button>
        <span className={badgeStyles[item.type] || styles.badge}>
          {badgeLabels[item.type] || item.type}
        </span>
      </div>

      {item.note_snippet && <p className={styles.snippet}>{item.note_snippet}</p>}

      <div className={styles.actions}>
        {item.type === 'untagged' && (
          <button
            className={styles.actionButton}
            onClick={handleSuggestTags}
            disabled={isLoadingTags || isApplying}
          >
            {isLoadingTags ? <Loader2 size={12} /> : <Tag size={12} />}
            Suggest Tags
          </button>
        )}

        {item.type === 'inbox' && (
          <button
            className={styles.actionButton}
            onClick={handleSuggestProject}
            disabled={isLoadingProjects || isApplying}
          >
            {isLoadingProjects ? <Loader2 size={12} /> : <FolderOpen size={12} />}
            Suggest Project
          </button>
        )}

        {item.type === 'orphan' && (
          <button
            className={styles.actionButton}
            onClick={() => navigate(`/notes/${item.note_id}`)}
          >
            <Link2 size={12} />
            Review
          </button>
        )}

        {item.type === 'similar_pair' && (
          <button
            className={styles.actionButton}
            onClick={() => navigate(`/notes/${item.note_id}`)}
          >
            Review
          </button>
        )}

        <button className={styles.skipButton} onClick={onDismiss} disabled={isApplying}>
          <X size={12} />
          Skip
        </button>
      </div>

      {tagSuggestions.length > 0 && (
        <div className={styles.suggestions}>
          {tagSuggestions.map((tag) => (
            <button
              key={tag.name}
              className={styles.tagPill}
              onClick={() => handleApplyTag(tag.name)}
              disabled={isApplying}
              title={`Apply tag "${tag.name}" (${Math.round(tag.confidence * 100)}% confidence)`}
            >
              <Tag size={10} />
              {tag.name}
              <span className={styles.confidence}>{Math.round(tag.confidence * 100)}%</span>
            </button>
          ))}
        </div>
      )}

      {projectSuggestions.length > 0 && (
        <div className={styles.suggestions}>
          {projectSuggestions.map((proj) => (
            <button
              key={proj.id}
              className={styles.projectPill}
              onClick={() => handleMoveToProject(proj.id, proj.name)}
              disabled={isApplying}
              title={`Move to "${proj.name}" (${Math.round(proj.confidence * 100)}% confidence)`}
            >
              <FolderOpen size={10} />
              {proj.name}
              <span className={styles.confidence}>{Math.round(proj.confidence * 100)}%</span>
            </button>
          ))}
        </div>
      )}

      {isLoadingTags && <p className={styles.loading}>Analyzing note for tag suggestions...</p>}
      {isLoadingProjects && (
        <p className={styles.loading}>Analyzing note for project suggestions...</p>
      )}
    </div>
  );
}

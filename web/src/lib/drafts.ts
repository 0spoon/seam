const DRAFT_PREFIX = 'seam_draft_';
const MAX_DRAFTS = 20;
const MAX_DRAFT_SIZE = 100000; // 100KB per draft

interface Draft {
  noteId: string;
  title: string;
  body: string;
  savedAt: number;
}

export function saveDraft(noteId: string, title: string, body: string): void {
  const draft: Draft = { noteId, title, body, savedAt: Date.now() };
  const json = JSON.stringify(draft);
  if (json.length > MAX_DRAFT_SIZE) return;

  try {
    localStorage.setItem(`${DRAFT_PREFIX}${noteId}`, json);
    cleanupDrafts();
  } catch {
    // localStorage full or unavailable -- silently fail
  }
}

export function clearDraft(noteId: string): void {
  try {
    localStorage.removeItem(`${DRAFT_PREFIX}${noteId}`);
  } catch {
    // silently fail
  }
}

export function getDraft(
  noteId: string,
): { title: string; body: string; savedAt: number } | null {
  try {
    const raw = localStorage.getItem(`${DRAFT_PREFIX}${noteId}`);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Draft;
    return { title: parsed.title, body: parsed.body, savedAt: parsed.savedAt };
  } catch {
    return null;
  }
}

function cleanupDrafts(): void {
  try {
    const keys: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i);
      if (key && key.startsWith(DRAFT_PREFIX)) {
        keys.push(key);
      }
    }
    if (keys.length <= MAX_DRAFTS) return;

    const drafts = keys
      .map((k) => {
        try {
          const d = JSON.parse(localStorage.getItem(k) || '') as Draft;
          return { key: k, savedAt: d.savedAt || 0 };
        } catch {
          return { key: k, savedAt: 0 };
        }
      })
      .sort((a, b) => a.savedAt - b.savedAt);

    const toRemove = drafts.slice(0, drafts.length - MAX_DRAFTS);
    toRemove.forEach((d) => localStorage.removeItem(d.key));
  } catch {
    // silently fail
  }
}

// Track recently opened notes in localStorage for the command palette.

const STORAGE_KEY = 'seam_recent_notes';
const MAX_RECENT = 10;

export interface RecentNote {
  id: string;
  title: string;
  openedAt: number; // timestamp
}

export function addRecentNote(id: string, title: string): void {
  const notes = getRecentNotes();
  // Remove existing entry for same ID (dedup)
  const filtered = notes.filter((n) => n.id !== id);
  // Prepend new entry
  filtered.unshift({ id, title, openedAt: Date.now() });
  // Trim to max
  const trimmed = filtered.slice(0, MAX_RECENT);
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(trimmed));
  } catch {
    // localStorage may be full or unavailable; ignore silently.
  }
}

export function getRecentNotes(): RecentNote[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed;
  } catch {
    return [];
  }
}

export function clearRecentNotes(): void {
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Ignore
  }
}

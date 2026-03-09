import { useEffect } from 'react';
import { addRecentNote } from '../lib/recentNotes';

// Track a note as recently opened when the component mounts with a valid note.
export function useRecentNote(id: string | undefined, title: string | undefined) {
  useEffect(() => {
    if (id && title) {
      addRecentNote(id, title);
    }
  }, [id]);
}

// Helpers for the Settings "Repo -> project" map editor. The backend stores
// the mapping as a JSON string setting (`repo_project_map`) that must be a JSON
// object of string -> string (absolute repo path -> project slug).

export interface RepoMapRow {
  path: string;
  project: string;
}

// parseRepoMap converts the stored JSON string into editor rows. Invalid or
// empty input yields an empty list rather than throwing.
export function parseRepoMap(raw: string | undefined): RepoMapRow[] {
  if (!raw || !raw.trim()) return [];
  try {
    const obj = JSON.parse(raw);
    if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return [];
    return Object.entries(obj as Record<string, unknown>)
      .filter(([, v]) => typeof v === 'string')
      .map(([path, project]) => ({ path, project: project as string }));
  } catch {
    return [];
  }
}

// serializeRepoMap converts editor rows back into the JSON string the backend
// expects. Rows with a blank path or project are dropped; paths are trimmed.
export function serializeRepoMap(rows: RepoMapRow[]): string {
  const obj: Record<string, string> = {};
  for (const row of rows) {
    const path = row.path.trim();
    const project = row.project.trim();
    if (!path || !project) continue;
    obj[path] = project;
  }
  return JSON.stringify(obj);
}

// isAbsolutePath reports whether p is an absolute filesystem path (POSIX "/..."
// or Windows "C:\..."). Used to validate rows before saving.
export function isAbsolutePath(p: string): boolean {
  const path = p.trim();
  if (!path) return false;
  return path.startsWith('/') || /^[A-Za-z]:[\\/]/.test(path);
}

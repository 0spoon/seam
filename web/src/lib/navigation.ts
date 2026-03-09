// Navigation helper that works outside React components.
// Wire this in Layout.tsx via useNavigate() so that command registry actions
// can trigger route changes without needing React hooks directly.

let navigateFn: ((path: string) => void) | null = null;

export function setNavigate(fn: (path: string) => void) {
  navigateFn = fn;
}

export function navigate(path: string) {
  navigateFn?.(path);
}

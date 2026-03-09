const PROJECT_COLORS = [
  '#c4915c',
  '#6b9b7a',
  '#7b8ec4',
  '#b87ba4',
  '#c49b6b',
  '#6bc4b8',
  '#9b7bc4',
  '#c46b6b',
];

export function getTagColor(tagName: string): string {
  let hash = 0;
  for (let i = 0; i < tagName.length; i++) {
    hash = (hash * 31 + tagName.charCodeAt(i)) | 0;
  }
  return PROJECT_COLORS[Math.abs(hash) % PROJECT_COLORS.length];
}

export function getProjectColor(index: number): string {
  return PROJECT_COLORS[index % PROJECT_COLORS.length];
}

export { PROJECT_COLORS };

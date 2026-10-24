export function LevelBadge({ level }: { level: string }) {
  return <span className={`level level-${level}`}>{level}</span>;
}

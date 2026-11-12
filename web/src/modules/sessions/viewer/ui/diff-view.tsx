import { diffLines } from 'diff';

export function DiffView({ oldText, newText }: { oldText: string; newText: string }) {
  const parts = diffLines(oldText, newText);
  return (
    <pre className="bg-border/50 overflow-x-auto rounded p-2 text-xs">
      {parts.map((part, i) => {
        const prefix = part.added ? '+' : part.removed ? '-' : ' ';
        const cls = part.added
          ? 'bg-success/10 text-success dark:bg-success/20'
          : part.removed
            ? 'bg-destructive/10 text-destructive dark:bg-destructive/20'
            : 'text-muted-foreground';
        return part.value
          .split('\n')
          .filter((line, idx, arr) => !(idx === arr.length - 1 && line === ''))
          .map((line, j) => (
            <div key={`${i}-${j}`} className={cls}>
              {prefix} {line}
            </div>
          ));
      })}
    </pre>
  );
}

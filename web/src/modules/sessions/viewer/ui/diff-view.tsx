import { diffLines } from 'diff';

export function DiffView({ oldText, newText }: { oldText: string; newText: string }) {
  const parts = diffLines(oldText, newText);
  return (
    <pre className="bg-border/50 overflow-x-auto rounded p-2 text-xs">
      {parts.map((part, i) => {
        const prefix = part.added ? '+' : part.removed ? '-' : ' ';
        const cls = part.added
          ? 'bg-green-100 text-green-900 dark:bg-green-900/40 dark:text-green-100'
          : part.removed
            ? 'bg-red-100 text-red-900 dark:bg-red-900/40 dark:text-red-100'
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

import Link from 'next/link';
import { formatRelativeTime } from '@/shared/lib/format/date';
import { buildFamilyChildren, DELETED_ROOT_KEY } from '../lib/build-family-tree';
import type { FamilyNode } from '../types';

function TreeNode({
  node,
  byParent,
  depth,
}: {
  node: FamilyNode;
  byParent: Map<string, FamilyNode[]>;
  depth: number;
}) {
  const children = byParent.get(node.id) ?? [];

  return (
    <li>
      {node.accessible ? (
        <Link
          href={`/sessions/${node.id}`}
          className={`text-sm underline ${node.isCurrent ? 'font-semibold text-foreground' : 'text-muted-foreground'}`}
        >
          {node.title ?? '(untitled session)'}
          {node.ownerLogin && <span className="ml-1 text-xs">· {node.ownerLogin}</span>}
          <span className="ml-1 text-xs">· {formatRelativeTime(node.startedAt)}</span>
        </Link>
      ) : (
        <span className="text-sm text-muted-foreground italic">1 fork in a project you can&apos;t see</span>
      )}
      {children.length > 0 && (
        <ul className="mt-1 flex flex-col gap-1 border-l border-border pl-3">
          {children.map((child) => (
            <TreeNode key={child.id} node={child} byParent={byParent} depth={depth + 1} />
          ))}
        </ul>
      )}
    </li>
  );
}

export function FamilyTree({
  nodes,
  danglingParentId,
}: {
  nodes: FamilyNode[];
  danglingParentId: string | null;
}) {
  // get-session-family always includes the current session itself as
  // one node — a session with no real fork relationship (no parent, no
  // children) comes back as that single node and nothing else, so
  // "<= 1" (not "=== 0") is what actually means "nothing to show."
  if (nodes.length <= 1 && !danglingParentId) return null;

  const byParent = buildFamilyChildren(nodes, danglingParentId);
  const roots = byParent.get('root') ?? [];

  return (
    <div>
      <p className="mb-1 text-xs text-muted-foreground">Fork history</p>
      <ul className="flex flex-col gap-1">
        {danglingParentId && (
          <li>
            <span className="text-sm text-muted-foreground italic">(deleted session)</span>
            <ul className="mt-1 flex flex-col gap-1 border-l border-border pl-3">
              {(byParent.get(DELETED_ROOT_KEY) ?? []).map((child) => (
                <TreeNode key={child.id} node={child} byParent={byParent} depth={1} />
              ))}
            </ul>
          </li>
        )}
        {roots.map((root) => (
          <TreeNode key={root.id} node={root} byParent={byParent} depth={0} />
        ))}
      </ul>
    </div>
  );
}

import type { FamilyNode } from '../types';

export const DELETED_ROOT_KEY = '__deleted_root__';

// Groups nodes by parentId for O(1) child lookup while rendering the tree.
// A node whose real parentId is the dangling (deleted) id is re-parented
// under the synthetic deleted-root key, so the tree still has exactly one
// root to start rendering from.
export function buildFamilyChildren(
  nodes: FamilyNode[],
  danglingParentId: string | null,
): Map<string, FamilyNode[]> {
  const byParent = new Map<string, FamilyNode[]>();
  for (const node of nodes) {
    const key =
      node.parentId === danglingParentId && danglingParentId
        ? DELETED_ROOT_KEY
        : node.parentId ?? 'root';
    const list = byParent.get(key) ?? [];
    list.push(node);
    byParent.set(key, list);
  }
  return byParent;
}

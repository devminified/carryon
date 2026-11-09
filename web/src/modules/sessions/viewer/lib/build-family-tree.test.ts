import { describe, expect, it } from 'vitest';
import { buildFamilyChildren, DELETED_ROOT_KEY } from './build-family-tree';
import type { FamilyNode } from '../types';

function accessibleNode(id: string, parentId: string | null): FamilyNode {
  return {
    id,
    parentId,
    accessible: true,
    title: null,
    ownerLogin: null,
    startedAt: '2026-01-01T00:00:00Z',
    isCurrent: false,
  };
}

function inaccessibleNode(id: string, parentId: string | null): FamilyNode {
  return { id, parentId, accessible: false };
}

describe('buildFamilyChildren', () => {
  it('groups a branching tree by parentId, with no-parent nodes under "root"', () => {
    const nodes = [
      accessibleNode('top', null),
      accessibleNode('child-a', 'top'),
      accessibleNode('child-b', 'top'),
      accessibleNode('grandchild', 'child-a'),
    ];

    const byParent = buildFamilyChildren(nodes, null);

    expect(byParent.get('root')?.map((n) => n.id)).toEqual(['top']);
    expect(byParent.get('top')?.map((n) => n.id)).toEqual(['child-a', 'child-b']);
    expect(byParent.get('child-a')?.map((n) => n.id)).toEqual(['grandchild']);
  });

  it('separates children of the literal "root" node from parentless nodes via the same key', () => {
    const nodes = [accessibleNode('n1', null), accessibleNode('n2', null)];
    const byParent = buildFamilyChildren(nodes, null);
    expect(byParent.get('root')?.map((n) => n.id)).toEqual(['n1', 'n2']);
  });

  it('re-parents nodes whose parentId is the dangling id under the synthetic deleted-root key', () => {
    const danglingParentId = 'deleted-parent';
    const nodes = [
      accessibleNode('orphan-a', danglingParentId),
      accessibleNode('orphan-b', danglingParentId),
      accessibleNode('grandchild', 'orphan-a'),
    ];

    const byParent = buildFamilyChildren(nodes, danglingParentId);

    expect(byParent.get(danglingParentId)).toBeUndefined();
    expect(byParent.get(DELETED_ROOT_KEY)?.map((n) => n.id)).toEqual(['orphan-a', 'orphan-b']);
    expect(byParent.get('orphan-a')?.map((n) => n.id)).toEqual(['grandchild']);
  });

  it('does not special-case the deleted-root key when danglingParentId is null', () => {
    const nodes = [accessibleNode('n1', null)];
    const byParent = buildFamilyChildren(nodes, null);
    expect(byParent.get(DELETED_ROOT_KEY)).toBeUndefined();
  });

  it('preserves accessible: false nodes with no extra fields', () => {
    const nodes = [accessibleNode('top', null), inaccessibleNode('hidden', 'top')];
    const byParent = buildFamilyChildren(nodes, null);
    expect(byParent.get('top')?.[0]).toEqual(inaccessibleNode('hidden', 'top'));
  });
});

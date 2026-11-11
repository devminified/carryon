'use client';

import { startTransition, useActionState } from 'react';
import { removeMemberAction, updateMemberRoleAction } from '../data/actions';
import type { MemberWithLogin } from '../types';
import { useActionToast } from '@/shared/hooks/use-action-toast';
import { buildFormData } from '@/shared/lib/form-data';
import { Button } from '@/shared/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';

function MemberRow({ member }: { member: MemberWithLogin }) {
  const [roleState, roleAction, roleIsPending] = useActionState(updateMemberRoleAction, null);
  const [removeState, removeAction, removeIsPending] = useActionState(removeMemberAction, null);
  useActionToast(roleState, 'Role updated.');
  useActionToast(removeState, 'Member removed.');

  // Dispatched directly (not via a real <form action={roleAction}>
  // submission) — confirmed for real that React 19 calls the native
  // HTMLFormElement.reset() after a <form action={...}> submission
  // resolves, and Radix's Select listens for that same 'reset' event
  // (correctly, for native-form-reset accessibility) and reports its
  // own original mount-time value back through onValueChange — which
  // immediately re-triggered a second, reverting submission. Calling
  // the action function directly has no native form submission for
  // either mechanism to react to.
  function onRoleChange(role: string) {
    startTransition(() => {
      roleAction(buildFormData({ projectId: member.project_id, userId: member.user_id, role }));
    });
  }

  return (
    <tr className="border-border border-b">
      <td className="py-2">{member.login ?? member.email ?? member.user_id}</td>
      <td className="py-2">
        <Select value={member.role} disabled={roleIsPending} onValueChange={onRoleChange}>
          <SelectTrigger size="sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="viewer">viewer</SelectItem>
            <SelectItem value="resumer">resumer</SelectItem>
            <SelectItem value="admin">admin</SelectItem>
          </SelectContent>
        </Select>
        {roleState?.error && <span className="text-destructive ml-2 text-xs">{roleState.error}</span>}
      </td>
      <td className="text-muted-foreground py-2 text-xs">{member.source}</td>
      <td className="py-2">
        <form action={removeAction} className="inline">
          <input type="hidden" name="projectId" value={member.project_id} />
          <input type="hidden" name="userId" value={member.user_id} />
          <Button
            type="submit"
            variant="link"
            size="xs"
            disabled={removeIsPending}
            className="text-destructive h-auto p-0 text-xs"
          >
            Remove
          </Button>
        </form>
      </td>
    </tr>
  );
}

export function MembersTable({ members }: { members: MemberWithLogin[] }) {
  if (members.length === 0) {
    return <p className="text-muted-foreground mt-4 text-sm">No members on this project yet.</p>;
  }

  return (
    <table className="mt-4 w-full text-left text-sm">
      <thead>
        <tr className="border-border border-b text-xs font-medium">
          <th className="py-2">Member</th>
          <th className="py-2">Role</th>
          <th className="py-2">Source</th>
          <th className="py-2"></th>
        </tr>
      </thead>
      <tbody>
        {members.map((m) => (
          <MemberRow key={m.user_id} member={m} />
        ))}
      </tbody>
    </table>
  );
}

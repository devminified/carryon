'use client';

import { startTransition, useActionState } from 'react';
import { removeMemberAction, updateMemberRoleAction } from '../data/actions';
import type { MemberWithLogin } from '../types';
import { useActionToast } from '@/shared/hooks/use-action-toast';
import { buildFormData } from '@/shared/lib/form-data';
import { Button } from '@/shared/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/shared/ui/table';

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
    <TableRow>
      <TableCell>{member.login ?? member.email ?? member.user_id}</TableCell>
      <TableCell>
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
      </TableCell>
      <TableCell className="text-muted-foreground text-xs">{member.source}</TableCell>
      <TableCell>
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
      </TableCell>
    </TableRow>
  );
}

export function MembersTable({ members }: { members: MemberWithLogin[] }) {
  if (members.length === 0) {
    return <p className="text-muted-foreground mt-4 text-sm">No members on this project yet.</p>;
  }

  return (
    <Table className="mt-4">
      <TableHeader>
        <TableRow>
          <TableHead>Member</TableHead>
          <TableHead>Role</TableHead>
          <TableHead>Source</TableHead>
          <TableHead></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {members.map((m) => (
          <MemberRow key={m.user_id} member={m} />
        ))}
      </TableBody>
    </Table>
  );
}

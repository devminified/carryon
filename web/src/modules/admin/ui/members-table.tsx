'use client';

import { startTransition, useActionState, useState } from 'react';
import { removeMemberAction, updateMemberRoleAction } from '../data/actions';
import type { MemberWithLogin } from '../types';
import { useActionToast } from '@/shared/hooks/use-action-toast';
import { buildFormData } from '@/shared/lib/form-data';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/shared/ui/alert-dialog';
import { Button } from '@/shared/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/shared/ui/table';

function MemberRow({ member }: { member: MemberWithLogin }) {
  const [roleState, roleAction, roleIsPending] = useActionState(updateMemberRoleAction, null);
  const [removeState, removeAction, removeIsPending] = useActionState(removeMemberAction, null);
  useActionToast(roleState, 'Role updated.');
  useActionToast(removeState, 'Member removed.');
  // CO-606: only the admin→non-admin transition gets a confirming
  // step — every other role change (viewer↔resumer, promoting to
  // admin) keeps its existing no-friction auto-submit. The Select
  // itself never shows the pending value while this dialog is open
  // (it's controlled by member.role, untouched until a real
  // confirmed submission lands and revalidation updates that prop).
  const [pendingDemoteTo, setPendingDemoteTo] = useState<string | null>(null);
  const [removeConfirmOpen, setRemoveConfirmOpen] = useState(false);

  // Dispatched directly (not via a real <form action={roleAction}>
  // submission) — confirmed for real that React 19 calls the native
  // HTMLFormElement.reset() after a <form action={...}> submission
  // resolves, and Radix's Select listens for that same 'reset' event
  // (correctly, for native-form-reset accessibility) and reports its
  // own original mount-time value back through onValueChange — which
  // immediately re-triggered a second, reverting submission. Calling
  // the action function directly has no real form submission for
  // either mechanism to react to.
  function submitRoleChange(role: string) {
    startTransition(() => {
      roleAction(buildFormData({ projectId: member.project_id, userId: member.user_id, role }));
    });
  }

  function onRoleChange(role: string) {
    if (member.role === 'admin' && role !== 'admin') {
      setPendingDemoteTo(role);
      return;
    }
    submitRoleChange(role);
  }

  function submitRemove() {
    startTransition(() => {
      removeAction(buildFormData({ projectId: member.project_id, userId: member.user_id }));
    });
  }

  const name = member.login ?? member.email ?? member.user_id;

  return (
    <TableRow>
      <TableCell>{name}</TableCell>
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
        <AlertDialog open={pendingDemoteTo !== null} onOpenChange={(open) => !open && setPendingDemoteTo(null)}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Remove {name}&apos;s admin access?</AlertDialogTitle>
              <AlertDialogDescription>
                They&apos;ll move to {pendingDemoteTo} and lose every admin-only action on this project —
                managing members, redaction config, and viewing other projects&apos; audit logs.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction
                onClick={() => {
                  if (pendingDemoteTo) submitRoleChange(pendingDemoteTo);
                  setPendingDemoteTo(null);
                }}
              >
                Remove admin access
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </TableCell>
      <TableCell className="text-muted-foreground text-xs">{member.source}</TableCell>
      <TableCell>
        <Button
          type="button"
          variant="link"
          size="xs"
          disabled={removeIsPending}
          className="text-destructive h-auto p-0 text-xs"
          onClick={() => setRemoveConfirmOpen(true)}
        >
          Remove
        </Button>
        <AlertDialog open={removeConfirmOpen} onOpenChange={setRemoveConfirmOpen}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Remove {name} from this project?</AlertDialogTitle>
              <AlertDialogDescription>
                They&apos;ll lose access immediately — they won&apos;t be able to see, pull, or resume any
                session on this project until someone adds them back.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={() => {
                  submitRemove();
                  setRemoveConfirmOpen(false);
                }}
              >
                Remove
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
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

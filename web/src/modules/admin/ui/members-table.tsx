'use client';

import { useActionState } from 'react';
import { removeMemberAction, updateMemberRoleAction } from '../data/actions';
import type { MemberWithLogin } from '../types';

function MemberRow({ member }: { member: MemberWithLogin }) {
  const [roleState, roleAction, roleIsPending] = useActionState(updateMemberRoleAction, null);
  const [, removeAction, removeIsPending] = useActionState(removeMemberAction, null);

  return (
    <tr className="border-border border-b">
      <td className="py-2">{member.login ?? member.email ?? member.user_id}</td>
      <td className="py-2">
        <form action={roleAction} className="inline">
          <input type="hidden" name="projectId" value={member.project_id} />
          <input type="hidden" name="userId" value={member.user_id} />
          <select
            name="role"
            defaultValue={member.role}
            disabled={roleIsPending}
            onChange={(e) => e.currentTarget.form?.requestSubmit()}
            className="border-border rounded border px-1 py-0.5 text-sm"
          >
            <option value="viewer">viewer</option>
            <option value="resumer">resumer</option>
            <option value="admin">admin</option>
          </select>
          {roleState?.error && <span className="text-destructive ml-2 text-xs">{roleState.error}</span>}
        </form>
      </td>
      <td className="text-muted-foreground py-2 text-xs">{member.source}</td>
      <td className="py-2">
        <form action={removeAction} className="inline">
          <input type="hidden" name="projectId" value={member.project_id} />
          <input type="hidden" name="userId" value={member.user_id} />
          <button type="submit" disabled={removeIsPending} className="text-destructive text-xs underline disabled:opacity-50">
            Remove
          </button>
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

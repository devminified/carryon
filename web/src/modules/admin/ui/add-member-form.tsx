'use client';

import { useActionState } from 'react';
import { addMemberOverrideAction } from '../data/actions';

export function AddMemberForm({ projectId }: { projectId: string }) {
  const [state, action, isPending] = useActionState(addMemberOverrideAction, null);

  return (
    <form action={action} className="border-border mt-4 flex flex-wrap items-end gap-3 rounded border p-3">
      <input type="hidden" name="projectId" value={projectId} />
      <div className="flex flex-col gap-1">
        <label htmlFor="email" className="text-xs font-medium">
          Email
        </label>
        <input id="email" name="email" type="email" required className="border-border rounded border px-2 py-1 text-sm" />
      </div>
      <div className="flex flex-col gap-1">
        <label htmlFor="role" className="text-xs font-medium">
          Role
        </label>
        <select id="role" name="role" defaultValue="viewer" className="border-border rounded border px-2 py-1 text-sm">
          <option value="viewer">viewer</option>
          <option value="resumer">resumer</option>
          <option value="admin">admin</option>
        </select>
      </div>
      <button type="submit" disabled={isPending} className="bg-primary text-primary-foreground rounded px-3 py-1.5 text-sm disabled:opacity-50">
        {isPending ? 'Adding…' : 'Add manual override'}
      </button>
      {state?.error && <p className="text-destructive w-full text-sm">{state.error}</p>}
    </form>
  );
}

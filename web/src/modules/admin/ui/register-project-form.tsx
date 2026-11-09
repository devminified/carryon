'use client';

import { useActionState } from 'react';
import { registerProjectAction } from '../data/actions';

export function RegisterProjectForm() {
  const [state, action, isPending] = useActionState(registerProjectAction, null);

  return (
    <form action={action} className="border-border mb-6 flex flex-wrap items-end gap-3 rounded border p-4">
      <div className="flex flex-col gap-1">
        <label htmlFor="name" className="text-xs font-medium">
          Name
        </label>
        <input id="name" name="name" required className="border-border rounded border px-2 py-1 text-sm" placeholder="acme-api" />
      </div>
      <div className="flex flex-col gap-1">
        <label htmlFor="gitRemote" className="text-xs font-medium">
          Git remote (host/owner/repo)
        </label>
        <input
          id="gitRemote"
          name="gitRemote"
          required
          className="border-border rounded border px-2 py-1 text-sm"
          placeholder="github.com/devminified/acme-api"
        />
      </div>
      <button type="submit" disabled={isPending} className="bg-primary text-primary-foreground rounded px-3 py-1.5 text-sm disabled:opacity-50">
        {isPending ? 'Registering…' : 'Register project'}
      </button>
      {state?.error && <p className="text-destructive w-full text-sm">{state.error}</p>}
      {state?.success && <p className="w-full text-sm text-green-600">Project registered.</p>}
    </form>
  );
}

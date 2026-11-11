'use client';

import { startTransition, useActionState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { registerProjectAction } from '../data/actions';
import { registerProjectSchema, type RegisterProjectInput } from '../schemas';
import { useActionToast } from '@/shared/hooks/use-action-toast';
import { buildFormData } from '@/shared/lib/form-data';
import { Button } from '@/shared/ui/button';
import { Input } from '@/shared/ui/input';
import { Label } from '@/shared/ui/label';

export function RegisterProjectForm() {
  const [state, action, isPending] = useActionState(registerProjectAction, null);
  // Deliberately distinct wording from the inline "Project
  // registered." text below — identical text in both places is
  // ambiguous to a Playwright getByText locator (hit for real:
  // admin-pages.spec.ts's own registration test), and is also a
  // redundant double-announcement for a screen reader.
  useActionToast(state, 'New project added.');

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<RegisterProjectInput>({ resolver: zodResolver(registerProjectSchema) });

  // registerProjectSchema's shape already matches raw form input
  // exactly (two plain strings, no transforms/coercion) — it's reused
  // directly here as the client-side resolver, the same schema the
  // server action validates against.
  function onValid(data: RegisterProjectInput) {
    startTransition(() => action(buildFormData(data)));
  }

  return (
    <form
      onSubmit={handleSubmit(onValid)}
      className="border-border mb-6 flex flex-wrap items-end gap-3 rounded border p-4"
    >
      <div className="flex flex-col gap-1">
        <Label htmlFor="name">Name</Label>
        <Input id="name" placeholder="acme-api" {...register('name')} />
        {errors.name && <p className="text-destructive text-xs">{errors.name.message}</p>}
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="gitRemote">Git remote (host/owner/repo)</Label>
        <Input id="gitRemote" placeholder="github.com/devminified/acme-api" {...register('gitRemote')} />
        {errors.gitRemote && <p className="text-destructive text-xs">{errors.gitRemote.message}</p>}
      </div>
      <Button type="submit" disabled={isPending}>
        {isPending ? 'Registering…' : 'Register project'}
      </Button>
      {state?.error && <p className="text-destructive w-full text-sm">{state.error}</p>}
      {state?.success && <p className="w-full text-sm text-green-600">Project registered.</p>}
    </form>
  );
}

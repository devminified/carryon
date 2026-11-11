'use client';

import { startTransition, useActionState } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { addMemberOverrideAction } from '../data/actions';
import { addMemberOverrideSchema, type AddMemberOverrideInput } from '../schemas';
import { useActionToast } from '@/shared/hooks/use-action-toast';
import { buildFormData } from '@/shared/lib/form-data';
import { Button } from '@/shared/ui/button';
import { Input } from '@/shared/ui/input';
import { Label } from '@/shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';

export function AddMemberForm({ projectId }: { projectId: string }) {
  const [state, action, isPending] = useActionState(addMemberOverrideAction, null);
  useActionToast(state, 'Member added.');

  const {
    register,
    control,
    handleSubmit,
    formState: { errors },
  } = useForm<AddMemberOverrideInput>({
    resolver: zodResolver(addMemberOverrideSchema),
    defaultValues: { projectId, role: 'viewer' },
  });

  function onValid(data: AddMemberOverrideInput) {
    startTransition(() => action(buildFormData(data)));
  }

  return (
    <form
      onSubmit={handleSubmit(onValid)}
      className="border-border mt-4 flex flex-wrap items-end gap-3 rounded border p-3"
    >
      <input type="hidden" {...register('projectId')} />
      <div className="flex flex-col gap-1">
        <Label htmlFor="email">Email</Label>
        {/* suppressHydrationWarning — same Bitwarden-extension-injected-
            attribute mismatch as login-form.tsx: a password manager
            tags type="email" inputs for autofill matching before React
            hydrates. */}
        <Input id="email" type="email" suppressHydrationWarning {...register('email')} />
        {errors.email && <p className="text-destructive text-xs">{errors.email.message}</p>}
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="role">Role</Label>
        <Controller
          control={control}
          name="role"
          render={({ field }) => (
            <Select value={field.value} onValueChange={field.onChange} disabled={field.disabled}>
              <SelectTrigger id="role" size="sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="viewer">viewer</SelectItem>
                <SelectItem value="resumer">resumer</SelectItem>
                <SelectItem value="admin">admin</SelectItem>
              </SelectContent>
            </Select>
          )}
        />
      </div>
      <Button type="submit" disabled={isPending}>
        {isPending ? 'Adding…' : 'Add manual override'}
      </Button>
      {state?.error && <p className="text-destructive w-full text-sm">{state.error}</p>}
    </form>
  );
}

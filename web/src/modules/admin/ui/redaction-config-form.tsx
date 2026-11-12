'use client';

import { startTransition, useActionState, useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { updateRedactionConfigAction } from '../data/actions';
import type { RedactionConfigRow } from '../types';
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
import { Input } from '@/shared/ui/input';
import { Label } from '@/shared/ui/label';
import { Textarea } from '@/shared/ui/textarea';

// updateRedactionConfigAction's own updateRedactionConfigSchema
// (schemas.ts) validates disabledRuleIds/denylistAdditions as already-
// split arrays — the action does that splitting itself from each raw
// textarea string before ever calling that schema. This form's real
// fields are those two raw strings, so it gets its own lightweight,
// client-only schema matching what's actually submitted, rather than
// reusing a schema shaped for the action's post-split internal state.
const redactionConfigFormSchema = z.object({
  disabledRuleIds: z.string(),
  denylistAdditions: z.string(),
  pinnedVersion: z.string(),
});
type RedactionConfigFormValues = z.infer<typeof redactionConfigFormSchema>;

export function RedactionConfigForm({ config }: { config: RedactionConfigRow }) {
  const [state, action, isPending] = useActionState(updateRedactionConfigAction, null);
  useActionToast(state, 'Redaction config saved.');

  const {
    register,
    handleSubmit,
  } = useForm<RedactionConfigFormValues>({
    resolver: zodResolver(redactionConfigFormSchema),
    defaultValues: {
      disabledRuleIds: config.disabled_rule_ids.join('\n'),
      denylistAdditions: config.denylist_additions.join('\n'),
      pinnedVersion: config.pinned_version ?? '',
    },
  });

  // CO-606: only a genuine change to the pinned version gets a
  // confirming step — editing the other fields, or submitting with
  // pinnedVersion unchanged, saves immediately as before.
  const [pendingPinChange, setPendingPinChange] = useState<RedactionConfigFormValues | null>(null);
  const currentPinnedVersion = config.pinned_version ?? '';

  function submit(data: RedactionConfigFormValues) {
    startTransition(() => action(buildFormData(data)));
  }

  function onValid(data: RedactionConfigFormValues) {
    if (data.pinnedVersion !== currentPinnedVersion) {
      setPendingPinChange(data);
      return;
    }
    submit(data);
  }

  return (
    <form onSubmit={handleSubmit(onValid)} className="max-w-2xl space-y-4">
      <div className="flex flex-col gap-1">
        <Label htmlFor="disabledRuleIds" className="text-sm font-medium">
          Disabled rule IDs (one per line)
        </Label>
        <span className="text-muted-foreground text-xs">
          Matches internal/redact&apos;s rule IDs (e.g. generic-api-key) — a rule here is dropped from the laptop&apos;s own redaction set the next
          time it fetches get-config.
        </span>
        <Textarea id="disabledRuleIds" rows={4} {...register('disabledRuleIds')} />
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="denylistAdditions" className="text-sm font-medium">
          Extra path denylist globs (one per line)
        </Label>
        <Textarea id="denylistAdditions" rows={4} {...register('denylistAdditions')} />
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="pinnedVersion" className="text-sm font-medium">
          Pinned agent version
        </Label>
        <span className="text-muted-foreground text-xs">Blocks the daily auto-update past this version. Leave blank for no pin.</span>
        <Input id="pinnedVersion" placeholder="e.g. 1.4.0" {...register('pinnedVersion')} />
      </div>

      <div className="flex items-center gap-3">
        <Button type="submit" disabled={isPending}>
          {isPending ? 'Saving…' : 'Save config'}
        </Button>
        {state?.error && <p className="text-destructive text-sm">{state.error}</p>}
        {state?.success && <p className="text-success text-sm">Saved.</p>}
      </div>

      <AlertDialog
        open={pendingPinChange !== null}
        onOpenChange={(open) => !open && setPendingPinChange(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Change the fleet&apos;s pinned agent version?</AlertDialogTitle>
            <AlertDialogDescription>
              From <strong>{currentPinnedVersion || 'no pin'}</strong> to{' '}
              <strong>{pendingPinChange?.pinnedVersion || 'no pin'}</strong> — this changes the
              compatibility ceiling for every developer&apos;s laptop the next time it checks for an
              update.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (pendingPinChange) submit(pendingPinChange);
                setPendingPinChange(null);
              }}
            >
              Change pinned version
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </form>
  );
}

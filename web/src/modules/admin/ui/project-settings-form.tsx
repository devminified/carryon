'use client';

import { startTransition, useActionState, useState } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import type { z } from 'zod';
import { updateProjectSettingsAction } from '../data/actions';
import { updateProjectSettingsSchema } from '../schemas';
import type { ProjectWithSettings } from '../types';
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
import { Checkbox } from '@/shared/ui/checkbox';
import { Input } from '@/shared/ui/input';
import { Label } from '@/shared/ui/label';

// updateProjectSettingsSchema coerces/transforms several fields
// (retentionDaysAfterClose, dailyTokenBudget via z.coerce.number();
// bucket/teamViewer/teamResumer/teamAdmin/slackWebhookUrl/
// mattermostWebhookUrl's empty-string-to-null transforms) — its input
// type (what a real form submits) and output type (what the action
// receives) genuinely differ, so useForm needs both sides spelled out
// rather than the single z.infer this module's other, transform-free
// schemas get away with.
type FormInput = z.input<typeof updateProjectSettingsSchema>;
type FormOutput = z.output<typeof updateProjectSettingsSchema>;

export function ProjectSettingsForm({ project }: { project: ProjectWithSettings }) {
  const [state, action, isPending] = useActionState(updateProjectSettingsAction, null);
  useActionToast(state, 'Settings saved.');
  // Every project's settings form is mounted at once (inside its own
  // collapsed <details>, not conditionally rendered) — a plain literal
  // id like "bucket" would collide across every row on the page,
  // which breaks label-for/id association (confirmed for real: a
  // label's own .control resolves to the document's first matching
  // id, not necessarily the input inside its own row). Suffixing with
  // the project's id keeps every id page-wide unique.
  const fieldId = (name: string) => `${name}-${project.id}`;

  const {
    register,
    control,
    handleSubmit,
    formState: { errors },
  } = useForm<FormInput, unknown, FormOutput>({
    resolver: zodResolver(updateProjectSettingsSchema),
    defaultValues: {
      projectId: project.id,
      bucket: project.bucket ?? '',
      retentionDaysAfterClose: project.retention_days_after_close,
      closed: project.closed_at !== null,
      dailyTokenBudget: project.daily_token_budget,
      teamViewer: project.team_viewer ?? '',
      teamResumer: project.team_resumer ?? '',
      teamAdmin: project.team_admin ?? '',
      slackWebhookUrl: project.slackWebhookUrl ?? '',
      mattermostWebhookUrl: project.mattermostWebhookUrl ?? '',
      notifyDigest: project.notifyDigest,
    },
  });

  // CO-606: only newly checking "Closed" (the project wasn't already
  // closed) gets a confirming step — unchecking it, or submitting with
  // it already checked and unchanged, saves immediately as before.
  const [pendingClose, setPendingClose] = useState<FormOutput | null>(null);

  function submit(data: FormOutput) {
    startTransition(() => action(buildFormData(data)));
  }

  function onValid(data: FormOutput) {
    if (data.closed && project.closed_at === null) {
      setPendingClose(data);
      return;
    }
    submit(data);
  }

  return (
    <form
      onSubmit={handleSubmit(onValid)}
      className="grid grid-cols-2 gap-3 pt-3 text-sm md:grid-cols-3"
    >
      <input type="hidden" {...register('projectId')} />

      <div className="flex flex-col gap-1">
        <Label htmlFor={fieldId('bucket')} className="text-xs font-medium">
          Dedicated bucket
        </Label>
        <Input id={fieldId('bucket')} placeholder="shared default" {...register('bucket')} />
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor={fieldId('retentionDaysAfterClose')} className="text-xs font-medium">
          Retention (days after close)
        </Label>
        <Input
          id={fieldId('retentionDaysAfterClose')}
          type="number"
          min={0}
          {...register('retentionDaysAfterClose')}
        />
        {errors.retentionDaysAfterClose && (
          <p className="text-destructive text-xs">{errors.retentionDaysAfterClose.message}</p>
        )}
      </div>

      <Controller
        control={control}
        name="closed"
        render={({ field }) => (
          <Label htmlFor={fieldId('closed')} className="flex items-center gap-2 pt-5">
            <Checkbox
              id={fieldId('closed')}
              checked={field.value}
              onCheckedChange={field.onChange}
              disabled={field.disabled}
            />
            <span>Closed</span>
          </Label>
        )}
      />

      <div className="flex flex-col gap-1">
        <Label htmlFor={fieldId('dailyTokenBudget')} className="text-xs font-medium">
          Daily AI token budget
        </Label>
        <Input id={fieldId('dailyTokenBudget')} type="number" min={0} {...register('dailyTokenBudget')} />
        {errors.dailyTokenBudget && (
          <p className="text-destructive text-xs">{errors.dailyTokenBudget.message}</p>
        )}
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor={fieldId('teamViewer')} className="text-xs font-medium">
          GitHub team: viewer
        </Label>
        <Input id={fieldId('teamViewer')} placeholder="team ID" {...register('teamViewer')} />
        {errors.teamViewer && <p className="text-destructive text-xs">{errors.teamViewer.message}</p>}
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor={fieldId('teamResumer')} className="text-xs font-medium">
          GitHub team: resumer
        </Label>
        <Input id={fieldId('teamResumer')} placeholder="team ID" {...register('teamResumer')} />
        {errors.teamResumer && <p className="text-destructive text-xs">{errors.teamResumer.message}</p>}
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor={fieldId('teamAdmin')} className="text-xs font-medium">
          GitHub team: admin
        </Label>
        <Input id={fieldId('teamAdmin')} placeholder="team ID" {...register('teamAdmin')} />
        {errors.teamAdmin && <p className="text-destructive text-xs">{errors.teamAdmin.message}</p>}
      </div>

      <div className="col-span-2 flex flex-col gap-1">
        <Label htmlFor={fieldId('slackWebhookUrl')} className="text-xs font-medium">
          Slack webhook URL
        </Label>
        <Input
          id={fieldId('slackWebhookUrl')}
          placeholder="https://hooks.slack.com/services/…"
          {...register('slackWebhookUrl')}
        />
        {errors.slackWebhookUrl && (
          <p className="text-destructive text-xs">{errors.slackWebhookUrl.message}</p>
        )}
      </div>

      <div className="col-span-2 flex flex-col gap-1">
        <Label htmlFor={fieldId('mattermostWebhookUrl')} className="text-xs font-medium">
          Mattermost webhook URL
        </Label>
        <Input
          id={fieldId('mattermostWebhookUrl')}
          placeholder="https://your-mattermost.example/hooks/…"
          {...register('mattermostWebhookUrl')}
        />
        {errors.mattermostWebhookUrl && (
          <p className="text-destructive text-xs">{errors.mattermostWebhookUrl.message}</p>
        )}
      </div>

      <Controller
        control={control}
        name="notifyDigest"
        render={({ field }) => (
          <Label htmlFor={fieldId('notifyDigest')} className="flex items-center gap-2 pt-5">
            <Checkbox
              id={fieldId('notifyDigest')}
              checked={field.value}
              onCheckedChange={field.onChange}
              disabled={field.disabled}
            />
            <span>Daily digest</span>
          </Label>
        )}
      />

      <div className="col-span-full flex items-center gap-3 pt-2">
        <Button type="submit" disabled={isPending}>
          {isPending ? 'Saving…' : 'Save settings'}
        </Button>
        {state?.error && <p className="text-destructive">{state.error}</p>}
        {state?.success && <p className="text-green-600">Saved.</p>}
      </div>

      <AlertDialog open={pendingClose !== null} onOpenChange={(open) => !open && setPendingClose(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Close this project?</AlertDialogTitle>
            <AlertDialogDescription>
              New sessions can no longer be captured for {project.name}. Existing sessions stay
              readable until the retention window after close elapses, per this project&apos;s own
              retention setting.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (pendingClose) submit(pendingClose);
                setPendingClose(null);
              }}
            >
              Close project
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </form>
  );
}

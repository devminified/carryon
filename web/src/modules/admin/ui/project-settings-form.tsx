'use client';

import { useActionState } from 'react';
import { updateProjectSettingsAction } from '../data/actions';
import type { ProjectWithSettings } from '../types';

export function ProjectSettingsForm({ project }: { project: ProjectWithSettings }) {
  const [state, action, isPending] = useActionState(updateProjectSettingsAction, null);

  return (
    <form action={action} className="grid grid-cols-2 gap-3 pt-3 text-sm md:grid-cols-3">
      <input type="hidden" name="projectId" value={project.id} />

      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium">Dedicated bucket</span>
        <input name="bucket" defaultValue={project.bucket ?? ''} placeholder="shared default" className="border-border rounded border px-2 py-1" />
      </label>

      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium">Retention (days after close)</span>
        <input
          name="retentionDaysAfterClose"
          type="number"
          min={0}
          defaultValue={project.retention_days_after_close}
          className="border-border rounded border px-2 py-1"
        />
      </label>

      <label className="flex items-center gap-2 pt-5">
        <input type="checkbox" name="closed" defaultChecked={project.closed_at !== null} />
        <span>Closed</span>
      </label>

      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium">Daily AI token budget</span>
        <input name="dailyTokenBudget" type="number" min={0} defaultValue={project.daily_token_budget} className="border-border rounded border px-2 py-1" />
      </label>

      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium">GitHub team: viewer</span>
        <input name="teamViewer" defaultValue={project.team_viewer ?? ''} placeholder="team ID" className="border-border rounded border px-2 py-1" />
      </label>

      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium">GitHub team: resumer</span>
        <input name="teamResumer" defaultValue={project.team_resumer ?? ''} placeholder="team ID" className="border-border rounded border px-2 py-1" />
      </label>

      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium">GitHub team: admin</span>
        <input name="teamAdmin" defaultValue={project.team_admin ?? ''} placeholder="team ID" className="border-border rounded border px-2 py-1" />
      </label>

      <label className="col-span-2 flex flex-col gap-1">
        <span className="text-xs font-medium">Slack webhook URL</span>
        <input
          name="slackWebhookUrl"
          defaultValue={project.slackWebhookUrl ?? ''}
          placeholder="https://hooks.slack.com/services/…"
          className="border-border rounded border px-2 py-1"
        />
      </label>

      <label className="col-span-2 flex flex-col gap-1">
        <span className="text-xs font-medium">Mattermost webhook URL</span>
        <input
          name="mattermostWebhookUrl"
          defaultValue={project.mattermostWebhookUrl ?? ''}
          placeholder="https://your-mattermost.example/hooks/…"
          className="border-border rounded border px-2 py-1"
        />
      </label>

      <label className="flex items-center gap-2 pt-5">
        <input type="checkbox" name="notifyDigest" defaultChecked={project.notifyDigest} />
        <span>Daily digest</span>
      </label>

      <div className="col-span-full flex items-center gap-3 pt-2">
        <button type="submit" disabled={isPending} className="bg-primary text-primary-foreground rounded px-3 py-1.5 disabled:opacity-50">
          {isPending ? 'Saving…' : 'Save settings'}
        </button>
        {state?.error && <p className="text-destructive">{state.error}</p>}
        {state?.success && <p className="text-green-600">Saved.</p>}
      </div>
    </form>
  );
}

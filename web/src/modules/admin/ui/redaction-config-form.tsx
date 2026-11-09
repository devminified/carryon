'use client';

import { useActionState } from 'react';
import { updateRedactionConfigAction } from '../data/actions';
import type { RedactionConfigRow } from '../types';

export function RedactionConfigForm({ config }: { config: RedactionConfigRow }) {
  const [state, action, isPending] = useActionState(updateRedactionConfigAction, null);

  return (
    <form action={action} className="max-w-2xl space-y-4">
      <label className="flex flex-col gap-1">
        <span className="text-sm font-medium">Disabled rule IDs (one per line)</span>
        <span className="text-muted-foreground text-xs">
          Matches internal/redact&apos;s rule IDs (e.g. generic-api-key) — a rule here is dropped from the laptop&apos;s own redaction set the next
          time it fetches get-config.
        </span>
        <textarea
          name="disabledRuleIds"
          defaultValue={config.disabled_rule_ids.join('\n')}
          rows={4}
          className="border-border rounded border px-2 py-1 text-sm"
        />
      </label>

      <label className="flex flex-col gap-1">
        <span className="text-sm font-medium">Extra path denylist globs (one per line)</span>
        <textarea
          name="denylistAdditions"
          defaultValue={config.denylist_additions.join('\n')}
          rows={4}
          className="border-border rounded border px-2 py-1 text-sm"
        />
      </label>

      <label className="flex flex-col gap-1">
        <span className="text-sm font-medium">Pinned agent version</span>
        <span className="text-muted-foreground text-xs">Blocks the daily auto-update past this version. Leave blank for no pin.</span>
        <input
          name="pinnedVersion"
          defaultValue={config.pinned_version ?? ''}
          placeholder="e.g. 1.4.0"
          className="border-border rounded border px-2 py-1 text-sm"
        />
      </label>

      <div className="flex items-center gap-3">
        <button type="submit" disabled={isPending} className="bg-primary text-primary-foreground rounded px-3 py-1.5 text-sm disabled:opacity-50">
          {isPending ? 'Saving…' : 'Save config'}
        </button>
        {state?.error && <p className="text-destructive text-sm">{state.error}</p>}
        {state?.success && <p className="text-sm text-green-600">Saved.</p>}
      </div>
    </form>
  );
}

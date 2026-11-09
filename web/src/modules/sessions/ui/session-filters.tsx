'use client';

import { useRouter, usePathname, useSearchParams } from 'next/navigation';
import { useRef } from 'react';
import { SESSION_OUTCOMES, SESSION_STATUSES } from '../types';

export function SessionFilters({
  developers,
  branches,
}: {
  developers: string[];
  branches: string[];
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const qInputRef = useRef<HTMLInputElement>(null);

  function setParam(key: string, value: string) {
    const params = new URLSearchParams(searchParams.toString());
    if (value) {
      params.set(key, value);
    } else {
      params.delete(key);
    }
    params.delete('cursor'); // any filter change restarts pagination
    router.push(`${pathname}?${params.toString()}`);
  }

  function commitSearch() {
    setParam('q', qInputRef.current?.value.trim() ?? '');
  }

  return (
    <div className="border-border flex flex-wrap items-center gap-2 border-b pb-4">
      <input
        ref={qInputRef}
        type="search"
        placeholder="Search title and summary…"
        defaultValue={searchParams.get('q') ?? ''}
        onKeyDown={(e) => e.key === 'Enter' && commitSearch()}
        onBlur={commitSearch}
        className="border-border bg-background min-w-48 rounded-md border px-2 py-1 text-sm"
      />

      <select
        aria-label="Filter by developer"
        value={searchParams.get('dev') ?? ''}
        onChange={(e) => setParam('dev', e.target.value)}
        className="border-border bg-background rounded-md border px-2 py-1 text-sm"
      >
        <option value="">All developers</option>
        {developers.map((d) => (
          <option key={d} value={d}>
            {d}
          </option>
        ))}
      </select>

      <select
        aria-label="Filter by status"
        value={searchParams.get('status') ?? ''}
        onChange={(e) => setParam('status', e.target.value)}
        className="border-border bg-background rounded-md border px-2 py-1 text-sm"
      >
        <option value="">All statuses</option>
        {SESSION_STATUSES.map((s) => (
          <option key={s} value={s}>
            {s}
          </option>
        ))}
      </select>

      <select
        aria-label="Filter by outcome"
        value={searchParams.get('outcome') ?? ''}
        onChange={(e) => setParam('outcome', e.target.value)}
        className="border-border bg-background rounded-md border px-2 py-1 text-sm"
      >
        <option value="">All outcomes</option>
        {SESSION_OUTCOMES.map((o) => (
          <option key={o} value={o}>
            {o}
          </option>
        ))}
      </select>

      <select
        aria-label="Filter by branch"
        value={searchParams.get('branch') ?? ''}
        onChange={(e) => setParam('branch', e.target.value)}
        className="border-border bg-background rounded-md border px-2 py-1 text-sm"
      >
        <option value="">All branches</option>
        {branches.map((b) => (
          <option key={b} value={b}>
            {b}
          </option>
        ))}
      </select>

      <input
        aria-label="From date"
        type="date"
        value={searchParams.get('from')?.slice(0, 10) ?? ''}
        onChange={(e) => setParam('from', e.target.value ? `${e.target.value}T00:00:00Z` : '')}
        className="border-border bg-background rounded-md border px-2 py-1 text-sm"
      />
      <input
        aria-label="To date"
        type="date"
        value={searchParams.get('to')?.slice(0, 10) ?? ''}
        onChange={(e) => setParam('to', e.target.value ? `${e.target.value}T23:59:59Z` : '')}
        className="border-border bg-background rounded-md border px-2 py-1 text-sm"
      />
    </div>
  );
}

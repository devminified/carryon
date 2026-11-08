'use client';

import { useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { createClient } from '@/shared/lib/supabase/client';

export function LoginForm() {
  const searchParams = useSearchParams();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(searchParams.get('error'));

  async function signInWithGitHub() {
    setPending(true);
    setError(null);
    const supabase = createClient();
    const next = searchParams.get('next') ?? '/';
    const { error: signInError } = await supabase.auth.signInWithOAuth({
      provider: 'github',
      options: {
        redirectTo: `${window.location.origin}/auth/callback?next=${encodeURIComponent(next)}`,
      },
    });
    if (signInError) {
      setError(signInError.message);
      setPending(false);
    }
    // on success the browser is redirected to GitHub — nothing left to do here
  }

  return (
    <div className="flex flex-col items-center gap-3">
      <button
        type="button"
        onClick={signInWithGitHub}
        disabled={pending}
        className="bg-foreground text-background rounded-md px-4 py-2 text-sm font-medium disabled:opacity-60"
      >
        {pending ? 'Redirecting…' : 'Sign in with GitHub'}
      </button>
      {error && <p className="text-destructive text-sm">{error}</p>}
    </div>
  );
}

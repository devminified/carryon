'use client';

import { useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { createClient } from '@/shared/lib/supabase/client';
import { Button } from '@/shared/ui/button';

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
    // suppressHydrationWarning — same Bitwarden-extension-injected-
    // attribute mismatch root.tsx's <body> already documents; this
    // wrapper is a separate DOM node the extension also tags
    // (bis_skin_checked), confirmed from the real console error.
    <div className="flex flex-col items-center gap-3" suppressHydrationWarning>
      <Button type="button" onClick={signInWithGitHub} disabled={pending}>
        {pending ? 'Redirecting…' : 'Sign in with GitHub'}
      </Button>
      {error && <p className="text-destructive text-sm">{error}</p>}
    </div>
  );
}

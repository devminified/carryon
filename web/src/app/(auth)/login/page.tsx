import { Suspense } from 'react';
import { LoginForm } from '@/modules/auth/ui/login-form';

export default function LoginPage() {
  return (
    <main className="bg-background flex min-h-screen flex-col items-center justify-center gap-6 px-4">
      <div className="flex flex-col items-center gap-2 text-center">
        <h1 className="text-foreground text-2xl font-semibold">Carryon</h1>
        <p className="text-muted-foreground text-sm">
          Sign in with your Devminified GitHub account.
        </p>
      </div>
      <Suspense fallback={null}>
        <LoginForm />
      </Suspense>
    </main>
  );
}

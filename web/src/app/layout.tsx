import type { Metadata } from 'next';
import { Geist, Geist_Mono } from 'next/font/google';
import './globals.css';

const geistSans = Geist({
  variable: '--font-geist-sans',
  subsets: ['latin'],
});

const geistMono = Geist_Mono({
  variable: '--font-geist-mono',
  subsets: ['latin'],
});

export const metadata: Metadata = {
  title: 'Carryon',
  description: 'Capture, pull, and resume Claude Code sessions across your team.',
};

export default function RootLayout({ children }: LayoutProps<'/'>) {
  return (
    <html lang="en" className={`${geistSans.variable} ${geistMono.variable} h-full antialiased`}>
      {/* suppressHydrationWarning here only covers mismatches on <body> itself
          (it's not recursive) — this is Next.js's own documented mitigation
          for a password-manager extension (Bitwarden's bis_skin_checked/
          bis_register/__processed_<uuid>__ markers, confirmed from the real
          console errors) injecting attributes into the DOM before React
          hydrates. It never masks a real mismatch deeper in the tree. */}
      <body className="flex min-h-full flex-col" suppressHydrationWarning>
        {children}
      </body>
    </html>
  );
}

import Link from 'next/link';

const TABS = [
  { href: '/admin/projects', label: 'Projects' },
  { href: '/admin/members', label: 'Members' },
  { href: '/admin/audit', label: 'Audit' },
  { href: '/admin/agents', label: 'Agents' },
  { href: '/admin/config', label: 'Config' },
] as const;

export function AdminNav() {
  return (
    <nav className="border-border mb-6 flex gap-4 border-b pb-2 text-sm">
      {TABS.map((tab) => (
        <Link key={tab.href} href={tab.href} className="text-muted-foreground hover:text-foreground">
          {tab.label}
        </Link>
      ))}
    </nav>
  );
}

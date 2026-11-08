import { defineConfig, globalIgnores } from 'eslint/config';
import nextVitals from 'eslint-config-next/core-web-vitals';
import nextTs from 'eslint-config-next/typescript';

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  {
    rules: {
      '@typescript-eslint/no-explicit-any': 'error',
      'no-restricted-syntax': [
        'error',
        {
          selector: 'TSEnumDeclaration',
          message: 'Use an `as const` array + derived union type instead of enum.',
        },
      ],
    },
  },
  {
    // A ui/ file importing data/queries.ts directly means a Server
    // Component should have fetched and passed props instead
    // (layers.md). Importing data/actions.ts or data/mutations.ts — the
    // 'use server' functions themselves, bound as a <form action={...}>
    // or passed to useActionState — is the normal, idiomatic pattern
    // (react-19-hooks.md's own rename-session-form.tsx example), so only
    // queries/keys are restricted here, not every file under data/.
    files: ['**/modules/**/ui/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              group: ['**/data/queries', '**/data/keys'],
              message:
                'ui/ must receive query data via props from a Server Component, not import modules/*/data/queries directly — see .claude/rules/layers.md.',
            },
          ],
        },
      ],
    },
  },
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    '.next/**',
    'out/**',
    'build/**',
    'next-env.d.ts',
  ]),
]);

export default eslintConfig;

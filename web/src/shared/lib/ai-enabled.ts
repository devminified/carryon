// CO-506: whether this deployment has a real ANTHROPIC_API_KEY
// configured for its Edge Functions. The backend already degrades
// gracefully with no key (summarizeSession() is never called at all
// in that case — see edge-functions.md's own summarize-session
// section); this flag is purely about the UI not showing AI-related
// controls/tiles that would otherwise just sit empty or error on
// click for an operator who deliberately runs without a key (CO-506's
// self-hosted deployment, most commonly). Set via build-time env
// (NEXT_PUBLIC_AI_ENABLED — see deploy/selfhost/docker-compose.yml's
// `web` service and its own install.sh), defaulting to enabled so
// every existing deployment (Cloud, staging) that doesn't set this
// var keeps its current behavior unchanged.
export const AI_ENABLED = process.env.NEXT_PUBLIC_AI_ENABLED !== 'false';

// Shared response envelope for every Edge Function — see
// .claude/rules/edge-functions.md: "Response shape is stable."

export function json(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

export function errorResponse(
  status: number,
  error: string,
  message?: string,
): Response {
  return json({ error, message }, status);
}

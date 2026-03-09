// Cloudflare Worker — minimal signaling relay for poopilot.
// Stores offer/answer SDP blobs in KV with a 5-minute TTL.
// No terminal data ever touches this server.
// Static PWA files are served by CF Workers Assets (configured in wrangler.toml).

export default {
  async fetch(request, env) {
    try {
      return await handleRequest(request, env);
    } catch (e) {
      return json({ error: e.message, stack: e.stack }, 500);
    }
  },
};

async function handleRequest(request, env) {
    const url = new URL(request.url);
    const path = url.pathname;

    // CORS preflight
    if (request.method === 'OPTIONS') {
      return new Response(null, { headers: corsHeaders() });
    }

    // Only handle /relay/* paths — everything else falls through to static assets
    if (!path.startsWith('/relay/')) {
      return env.ASSETS.fetch(request);
    }

    // PUT /relay/:room/offer
    // PUT /relay/:room/answer
    const putMatch = path.match(/^\/relay\/([a-zA-Z0-9_-]+)\/(offer|answer)$/);
    if (putMatch && request.method === 'PUT') {
      const [, room, type] = putMatch;
      const body = await request.text();
      if (!body || body.length > 50000) {
        return json({ error: 'invalid body' }, 400);
      }
      await env.ROOMS.put(`${room}:${type}`, body, { expirationTtl: 300 });
      return json({ ok: true });
    }

    // DELETE /relay/:room/answer (clear stale answer)
    const delMatch = path.match(/^\/relay\/([a-zA-Z0-9_-]+)\/(offer|answer)$/);
    if (delMatch && request.method === 'DELETE') {
      const [, room, type] = delMatch;
      await env.ROOMS.delete(`${room}:${type}`);
      return json({ ok: true });
    }

    // GET /relay/:room/offer
    // GET /relay/:room/answer
    const getMatch = path.match(/^\/relay\/([a-zA-Z0-9_-]+)\/(offer|answer)$/);
    if (getMatch && request.method === 'GET') {
      const [, room, type] = getMatch;
      const value = await env.ROOMS.get(`${room}:${type}`);
      if (!value) {
        return json({ error: 'not found' }, 404);
      }
      return json({ [type]: value });
    }

    return json({ error: 'not found' }, 404);
}

function json(data, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json', ...corsHeaders() },
  });
}

function corsHeaders() {
  return {
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Allow-Methods': 'GET, PUT, DELETE, OPTIONS',
    'Access-Control-Allow-Headers': 'Content-Type',
  };
}

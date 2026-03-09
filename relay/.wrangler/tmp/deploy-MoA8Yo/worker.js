var __defProp = Object.defineProperty;
var __name = (target, value) => __defProp(target, "name", { value, configurable: true });

// worker.js
var worker_default = {
  async fetch(request, env) {
    const url = new URL(request.url);
    const path = url.pathname;
    if (request.method === "OPTIONS") {
      return new Response(null, { headers: corsHeaders() });
    }
    if (!path.startsWith("/relay/")) {
      return env.ASSETS.fetch(request);
    }
    const putMatch = path.match(/^\/relay\/([a-zA-Z0-9_-]+)\/(offer|answer)$/);
    if (putMatch && request.method === "PUT") {
      const [, room, type] = putMatch;
      const body = await request.text();
      if (!body || body.length > 5e4) {
        return json({ error: "invalid body" }, 400);
      }
      await env.ROOMS.put(`${room}:${type}`, body, { expirationTtl: 300 });
      return json({ ok: true });
    }
    const getMatch = path.match(/^\/relay\/([a-zA-Z0-9_-]+)\/(offer|answer)$/);
    if (getMatch && request.method === "GET") {
      const [, room, type] = getMatch;
      const value = await env.ROOMS.get(`${room}:${type}`);
      if (!value) {
        return json({ error: "not found" }, 404);
      }
      return json({ [type]: value });
    }
    return json({ error: "not found" }, 404);
  }
};
function json(data, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { "Content-Type": "application/json", ...corsHeaders() }
  });
}
__name(json, "json");
function corsHeaders() {
  return {
    "Access-Control-Allow-Origin": "*",
    "Access-Control-Allow-Methods": "GET, PUT, OPTIONS",
    "Access-Control-Allow-Headers": "Content-Type"
  };
}
__name(corsHeaders, "corsHeaders");
export {
  worker_default as default
};
//# sourceMappingURL=worker.js.map

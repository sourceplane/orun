interface Env {
  API_NAME?: string;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    return Response.json({
      ok: true,
      path: url.pathname,
      service: env.API_NAME ?? "api-edge",
    });
  },
};
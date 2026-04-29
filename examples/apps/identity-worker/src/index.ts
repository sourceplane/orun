interface Env {
  TENANT_HEADER?: string;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const tenantHeader = env.TENANT_HEADER ?? "x-tenant-id";
    const tenantId = request.headers.get(tenantHeader) ?? "anonymous";

    return Response.json({
      ok: true,
      service: "identity-worker",
      tenantId,
    });
  },
};
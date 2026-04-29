export default {
  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);
    const projectId = url.searchParams.get("projectId") ?? "demo-project";

    return Response.json({
      ok: true,
      projectId,
      service: "projects-worker",
      status: "active",
    });
  },
};
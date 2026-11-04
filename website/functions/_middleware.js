// The orun docs live at https://orun.dev/docs/. orun.dev's Worker (orun-cloud,
// apps/website/worker) serves /docs/* from this Pages project and marks each
// request it forwards with `x-orun-docs-proxy`. Everyone else who reaches the
// project directly — orun-docs.pages.dev, a preview URL, an old link — gets a
// 301 to the same page on orun.dev, so search engines and readers end up on
// one address.
//
// Pre-move URLs had no /docs prefix (orun-docs.pages.dev/cli/orun/); they map
// onto orun.dev/docs/cli/orun/. The header is a routing hint, not a secret:
// sending it only shows the same public docs on the pages.dev host.
export async function onRequest({ request, next }) {
  if (request.headers.get('x-orun-docs-proxy') === 'orun.dev') return next();

  const url = new URL(request.url);
  const path = url.pathname === '/docs' || url.pathname.startsWith('/docs/') ? url.pathname : `/docs${url.pathname}`;
  return new Response(null, {
    status: 301,
    headers: { Location: `https://orun.dev${path}${url.search}`, Vary: 'x-orun-docs-proxy' },
  });
}

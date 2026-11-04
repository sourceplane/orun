// orun-docs.pages.dev is retired. The orun docs moved to
// https://orun.dev/docs/ (sourceplane/orun-website), and this Pages project now
// only redirects: every path, with its query string, 301s to the same page
// there. Pre-move URLs had no /docs prefix (orun-docs.pages.dev/cli/orun/ ->
// orun.dev/docs/cli/orun/); a path that already starts with /docs keeps it.
export async function onRequest({ request }) {
  const url = new URL(request.url);
  const path = url.pathname === '/docs' || url.pathname.startsWith('/docs/') ? url.pathname : `/docs${url.pathname}`;
  return Response.redirect(`https://orun.dev${path}${url.search}`, 301);
}

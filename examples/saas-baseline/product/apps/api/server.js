const http = require('node:http');
const handler = (req, res) => res.end(JSON.stringify({ service: 'api', ok: true }));
module.exports = { handler };
if (require.main === module) http.createServer(handler).listen(process.env.PORT || 8080);

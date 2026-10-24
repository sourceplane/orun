const test = require('node:test');
const assert = require('node:assert');
const { handler } = require('./server.js');
test('api answers ok', () => {
  let body = '';
  handler({}, { end: (b) => { body = b; } });
  assert.deepStrictEqual(JSON.parse(body), { service: 'api', ok: true });
});

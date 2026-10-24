const test = require('node:test');
const assert = require('node:assert');
const { handler } = require('./server.js');
test('web answers ok', () => {
  let body = '';
  handler({}, { end: (b) => { body = b; } });
  assert.deepStrictEqual(JSON.parse(body), { service: 'web', ok: true });
});

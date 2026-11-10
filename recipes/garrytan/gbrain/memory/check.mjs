import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { spawn } from 'node:child_process';
import { createInterface } from 'node:readline';

function client() {
  const child = spawn('bun', ['/app/src/cli.ts', 'serve', '--surface', 'starter', '--advertise', 'verbs'], { stdio: ['pipe', 'pipe', 'inherit'] });
  const pending = new Map(); let id = 0;
  createInterface({ input: child.stdout }).on('line', line => {
    let reply; try { reply = JSON.parse(line); } catch { return; }
    const p = pending.get(reply.id);
    if (p) { pending.delete(reply.id); clearTimeout(p.timer); reply.error ? p.reject(new Error(JSON.stringify(reply.error))) : p.resolve(reply.result); }
  });
  child.on('exit', () => { for (const p of pending.values()) { clearTimeout(p.timer); p.reject(new Error('GBrain exited before replying')); } pending.clear(); });
  const rpc = (method, params) => new Promise((resolve, reject) => {
    const key = ++id;
    const timer = setTimeout(() => { pending.delete(key); child.kill('SIGKILL'); reject(new Error(`${method} timed out`)); }, 60000);
    pending.set(key, { resolve, reject, timer });
    child.stdin.write(JSON.stringify({ jsonrpc: '2.0', id: key, method, params }) + '\n');
  });
  return {
    async start() {
      await rpc('initialize', { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 'ugt-recipe-check', version: '1' } });
      child.stdin.write(JSON.stringify({ jsonrpc: '2.0', method: 'notifications/initialized' }) + '\n');
    },
    async tool(name, args) {
      const result = await rpc('tools/call', { name, arguments: args });
      assert(!result.isError, JSON.stringify(result.content));
      return JSON.parse(result.content.find(x => x.type === 'text').text);
    },
    close() { child.stdin.end(); child.kill(); }
  };
}

const entity = `ugt-check-${randomUUID()}`;
const ids = new Set(); const clients = [];
async function open() { const c = client(); clients.push(c); await c.start(); return c; }
try {
  const first = await open();
  const saved = await first.tool('remember', { fact: 'The greenhouse door is orange.', provenance: 'UGT synthetic setup check', entity });
  assert(saved.id && ['inserted', 'duplicate'].includes(saved.status), JSON.stringify(saved)); ids.add(saved.id);
  // Closing a process must not lose the fact; another connection uses the same database.
  if (process.argv[2] !== 'concurrent') first.close();
  const second = await open();
  let recalled = await second.tool('recall', { entity });
  assert(JSON.stringify(recalled).includes(saved.id) && JSON.stringify(recalled).includes('orange'), 'Another connection could not recall the fact');
  await second.tool('forget', { id: saved.id, reason: 'UGT synthetic correction check' });
  ids.delete(saved.id);
  const corrected = await second.tool('remember', { fact: 'The greenhouse door is violet.', provenance: 'UGT synthetic correction check', entity });
  assert(corrected.id); ids.add(corrected.id);
  recalled = await second.tool('recall', { entity });
  assert(JSON.stringify(recalled).includes('violet') && !JSON.stringify(recalled).includes('orange'), 'Correction was not reflected in current memory');
  const third = await open();
  const persisted = await third.tool('recall', { entity });
  assert(JSON.stringify(persisted).includes(corrected.id), 'Corrected fact was not persistent');
  await third.tool('forget', { id: corrected.id, reason: 'UGT synthetic check cleanup' }); ids.delete(corrected.id);
  const absent = await third.tool('recall', { entity });
  assert(!JSON.stringify(absent).includes(corrected.id), 'Withdrawn fact was still recalled');
  await assert.rejects(third.tool('forget', { id: `ugt-missing-${randomUUID()}` }));
  console.log('GBrain saved, reopened, corrected and withdrew facts through real MCP connections.');
} finally {
  if (ids.size) {
    const cleanup = await open();
    for (const id of ids) await cleanup.tool('forget', { id, reason: 'UGT synthetic check cleanup' });
  }
  for (const c of clients) c.close();
}

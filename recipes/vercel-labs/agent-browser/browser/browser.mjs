import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { createInterface } from 'node:readline';
import { inflateSync } from 'node:zlib';

const root = '/work', out = join(root, 'outputs');
const sha = file => createHash('sha256').update(readFileSync(file)).digest('hex');
const example = '<!doctype html><title>Plant desk</title><style>body{background:#faf5ec;color:#173d28;font:24px sans-serif;padding:40px}button{background:#d34837;color:white;padding:14px}</style><h1>Plant desk</h1><label>Plant name <input id="name"></label><button id="save" onclick="document.querySelector(\'#answer\').textContent=\'Saved \'+document.querySelector(\'#name\').value">Save plant</button><p id="answer">Nothing saved</p>';
const exampleURL = 'data:text/html,' + encodeURIComponent(example);

function request() {
  const value = JSON.parse(readFileSync(join(root, 'inputs/page.json')));
  assert(value && typeof value === 'object' && !Array.isArray(value), 'page.json must contain an object');
  assert(Object.keys(value).every(k => ['url', 'width', 'height', 'full_page', 'wait_selector'].includes(k)), 'Unknown capture option');
  const url = new URL(value.url);
  assert(['https:', 'http:', 'data:'].includes(url.protocol), 'Supply an http, https or data URL');
  for (const k of ['width', 'height']) assert(value[k] === undefined || (Number.isInteger(value[k]) && value[k] >= 200 && value[k] <= 3840), `${k} must be 200–3840 pixels`);
  assert(value.full_page === undefined || typeof value.full_page === 'boolean', 'full_page must be true or false');
  assert(value.wait_selector === undefined || (typeof value.wait_selector === 'string' && value.wait_selector.trim()), 'wait_selector must be a CSS selector');
  return value;
}

function connect() {
  const child = spawn('agent-browser', ['mcp', '--tools', 'all'], { stdio: ['pipe', 'pipe', 'inherit'] });
  let next = 0;
  const pending = new Map();
  createInterface({ input: child.stdout }).on('line', line => {
    let message;
    try { message = JSON.parse(line); } catch { return; }
    const p = pending.get(message.id);
    if (p) { clearTimeout(p.timer); pending.delete(message.id); message.error ? p.reject(new Error(JSON.stringify(message.error))) : p.resolve(message.result); }
  });
  child.on('exit', () => { for (const p of pending.values()) { clearTimeout(p.timer); p.reject(new Error('Browser MCP exited before replying')); } pending.clear(); });
  function rpc(method, params) {
    const id = ++next;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { pending.delete(id); reject(new Error(`${method} timed out`)); child.kill('SIGKILL'); }, 60000);
      pending.set(id, { resolve, reject, timer });
      child.stdin.write(JSON.stringify({ jsonrpc: '2.0', id, method, params }) + '\n');
    });
  }
  return {
    async start() {
      await rpc('initialize', { protocolVersion: '2025-03-26', capabilities: {}, clientInfo: { name: 'ugt-recipe-check', version: '1' } });
      child.stdin.write(JSON.stringify({ jsonrpc: '2.0', method: 'notifications/initialized' }) + '\n');
    },
    async tool(name, args = {}) {
      const result = await rpc('tools/call', { name: `agent_browser_${name}`, arguments: args });
      assert(!result.isError, JSON.stringify(result.content));
      const response = result.structuredContent?.response;
      assert(response?.success !== false, JSON.stringify(response));
      return response?.data ?? result;
    },
    async close() {
      try { await this.tool('close'); } finally { child.stdin.end(); child.kill(); }
    }
  };
}

function png(file) {
  const bytes = readFileSync(file);
  assert(bytes.subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10])), 'Not a PNG');
  const width = bytes.readUInt32BE(16), height = bytes.readUInt32BE(20), color = bytes[25];
  assert(width >= 200 && height >= 200 && width <= 3840 && height <= 100000 && bytes[24] === 8 && [2, 6].includes(color), 'Invalid screenshot dimensions or pixel format');
  let offset = 8, ended = false; const chunks = [];
  while (offset < bytes.length) {
    const n = bytes.readUInt32BE(offset), type = bytes.toString('ascii', offset + 4, offset + 8);
    assert(offset + n + 12 <= bytes.length, 'Truncated PNG');
    if (type === 'IDAT') chunks.push(bytes.subarray(offset + 8, offset + 8 + n));
    if (type === 'IEND') ended = true;
    offset += n + 12;
  }
  const pixels = inflateSync(Buffer.concat(chunks));
  assert(ended && pixels.length === (width * (color === 2 ? 3 : 4) + 1) * height, 'Incomplete PNG data');
  assert(new Set(pixels).size > 8, 'Screenshot contains no useful visual detail');
  return { width, height };
}

function check() {
  const saved = JSON.parse(readFileSync(join(out, 'page.json'))), value = request();
  assert.deepEqual(saved.request, value, 'Capture belongs to different inputs');
  assert(typeof saved.title === 'string' && typeof saved.url === 'string');
  assert(sha(join(out, 'page.png')) === saved.png_sha256 && sha(join(out, 'snapshot.txt')) === saved.snapshot_sha256, 'Capture changed');
  const size = png(join(out, 'page.png'));
  assert(size.width === (value.width || 1280));
  if (!value.full_page) assert(size.height === (value.height || 800));
  assert(readFileSync(join(out, 'snapshot.txt'), 'utf8').trim(), 'Empty accessibility snapshot');
}

async function capture() {
  const value = request(), client = connect();
  mkdirSync(out, { recursive: true }); rmSync(join(out, 'page.json'), { force: true });
  await client.start();
  try {
    await client.tool('open', { url: value.url });
    await client.tool('set_viewport', { width: value.width || 1280, height: value.height || 800 });
    if (value.wait_selector) await client.tool('wait_for_selector', { selector: value.wait_selector });
    const snapshot = await client.tool('snapshot', { interactive: false });
    assert(typeof snapshot.snapshot === 'string', 'Browser did not return an accessibility snapshot');
    writeFileSync(join(out, 'snapshot.txt'), snapshot.snapshot + '\n');
    await client.tool('screenshot', { path: join(out, 'page.png'), fullPage: value.full_page || false });
    const title = await client.tool('get_title'), url = await client.tool('get_url');
    png(join(out, 'page.png'));
    writeFileSync(join(out, 'page.json'), JSON.stringify({ request: value, title: title.title, url: url.url, png_sha256: sha(join(out, 'page.png')), snapshot_sha256: sha(join(out, 'snapshot.txt')) }, null, 2));
  } finally { await client.close(); }
}

try {
  const mode = process.argv[2];
  if (mode === 'sample') writeFileSync(join(root, 'inputs/page.json'), JSON.stringify({ url: exampleURL, width: 1280, height: 800 }));
  else if (mode === 'check') check();
  else if (mode === 'qualify') {
    check();
    assert(readFileSync(join(out, 'snapshot.txt'), 'utf8').includes('Plant desk'));
    const c = connect(); await c.start();
    try {
      await c.tool('open', { url: exampleURL });
      await c.tool('fill', { selector: '#name', text: 'Orchid' });
      await c.tool('click', { selector: '#save' });
      const text = await c.tool('get_text', { selector: '#answer' });
      assert.equal(text.text, 'Saved Orchid', 'Browser state did not survive multiple MCP calls');
      await c.tool('open', { url: 'data:text/html,' + encodeURIComponent('<title>Second page</title><h1>Different content</h1>') });
      const snapshot = await c.tool('snapshot', { interactive: false });
      assert(snapshot.snapshot.includes('Different content') && !snapshot.snapshot.includes('Plant desk'));
      await assert.rejects(c.tool('click', { selector: '#missing' }));
    } finally { await c.close(); }
    assert(existsSync(join(out, 'page.png')));
  } else await capture();
} catch (error) { console.error(error.message); process.exitCode = 1; }

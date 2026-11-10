import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { closeSync, copyFileSync, existsSync, mkdirSync, mkdtempSync, openSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import assert from 'node:assert/strict';

function call(args, stdout = 'pipe') {
  const seconds = Number(process.env.REA_TIMEOUT || 600);
  assert(Number.isInteger(seconds) && seconds >= 10 && seconds <= 1800, 'REA_TIMEOUT must be 10–1800 seconds');
  const result = spawnSync('rea', args, { stdio: ['ignore', stdout, 'pipe'], timeout: seconds * 1000, maxBuffer: 16 * 1024 * 1024 });
  if (result.error || result.status !== 0) throw new Error(`REA could not complete: ${result.error?.message || result.stderr?.toString()}`);
  return result.stdout?.toString() || '';
}

function input(root) {
  const source = join(root, 'inputs/app.asar');
  assert(existsSync(source) && (statSync(source).isDirectory() || statSync(source).isFile()), 'Supply an application folder or an ASAR archive');
  return source;
}

function inventory(path, prefix = '') {
  const info = statSync(path);
  if (info.isDirectory()) return readdirSync(path).sort().flatMap(name => inventory(join(path, name), prefix ? `${prefix}/${name}` : name));
  assert(info.isFile(), 'Only regular app files are supported');
  return [{ path: prefix || 'app.asar', sha256: createHash('sha256').update(readFileSync(path)).digest('hex') }];
}

function view(file) {
  const request = JSON.stringify({ source: { kind: 'evidence-file', path: file }, view: { kind: 'summary' } });
  const text = call(['inspect-analysis-view', request, '--json']);
  const value = JSON.parse(text);
  assert(value && typeof value === 'object' && !value.error, 'REA did not return a valid evidence view');
  return text;
}

function run(root) {
  const source = input(root), files = inventory(source), out = join(root, 'outputs');
  assert(files.length, 'The app directory is empty');
  mkdirSync(out, { recursive: true });
  const evidence = join(out, 'app-evidence.json');
  rmSync(join(out, 'inputs.json'), { force: true });
  const fd = openSync(evidence, 'w');
  try { call(['analyze-javascript-application', resolve(source), '--json'], fd); }
  finally { closeSync(fd); }
  const summary = view(evidence);
  writeFileSync(join(out, 'summary.json'), summary);
  writeFileSync(join(out, 'inputs.json'), JSON.stringify(files));
}

function check(root) {
  const out = join(root, 'outputs');
  assert.deepEqual(JSON.parse(readFileSync(join(out, 'inputs.json'))), inventory(input(root)), 'Inputs changed after analysis');
  const evidence = join(out, 'app-evidence.json');
  JSON.parse(readFileSync(evidence));
  const expected = JSON.parse(view(evidence)), saved = JSON.parse(readFileSync(join(out, 'summary.json')));
  assert.deepEqual(saved, expected, 'Summary does not describe this evidence');
}

function sample(root, word = 'orchid') {
  const app = join(root, 'inputs/app.asar');
  mkdirSync(app, { recursive: true });
  writeFileSync(join(app, 'package.json'), JSON.stringify({ name: `${word}-inventory`, version: '1.0.0', type: 'module', main: 'main.js' }));
  writeFileSync(join(app, 'main.js'), `import { count } from './counter.js';\nexport const inventory = () => count(['${word}', 'fern']);\n`);
  writeFileSync(join(app, 'counter.js'), 'export const count = plants => plants.length;\n');
}

const mode = process.argv[2], root = '/work';
try {
  if (mode === 'sample') sample(root);
  else if (mode === 'check') check(root);
  else if (mode === 'qualify') {
    check(root);
    const data = readFileSync(join(root, 'outputs/app-evidence.json'), 'utf8');
    assert(data.includes('main.js') && data.includes('counter.js'), 'Both modules must appear in the analysis');
    const temp = mkdtempSync(join(tmpdir(), 'rea-qualification-'));
    try {
      sample(temp, 'cedar');
      run(temp); check(temp);
      writeFileSync(join(temp, 'inputs/app.asar/counter.js'), 'export const count = plants => plants.length + 1;\n');
      assert.throws(() => check(temp), /Inputs changed/);
      run(temp); check(temp);
      rmSync(join(temp, 'inputs/app.asar'), { recursive: true });
      copyFileSync('/samples/app.asar', join(temp, 'inputs/app.asar'));
      run(temp); check(temp);
      const archive = readFileSync(join(temp, 'outputs/app-evidence.json'), 'utf8');
      assert(archive.includes('archive-main.js') && archive.includes('archive-counter.js'), 'ASAR contents were not analysed');
      assert.throws(() => call(['analyze-javascript-application', join(temp, 'missing'), '--json']));
    } finally { rmSync(temp, { recursive: true, force: true }); }
  } else run(root);
} catch (error) { console.error(error.message); process.exit(1); }

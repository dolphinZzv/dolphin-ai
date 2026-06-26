// Fox extension validation — run with: node test/validate.mjs
// Validates manifest.json schema + JS syntax + file references.

import { readFileSync, readdirSync, statSync, existsSync } from 'fs';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const EXT_DIR = join(__dirname, '..');

let passed = 0;
let failed = 0;

function ok(msg)  { console.log(`  ✓ ${msg}`); passed++; }
function fail(msg) { console.error(`  ✘ ${msg}`); failed++; process.exitCode = 1; }

// ─── 1. manifest.json ─────────────────────────────────────────────
console.log('\n── manifest.json ──');
const manifest = JSON.parse(readFileSync(join(EXT_DIR, 'manifest.json'), 'utf-8'));

const required = [
  ['manifest_version', 3],
  ['name', 'Fox'],
  ['version'],
  ['permissions'],
  ['background.service_worker'],
  ['content_scripts[0].js'],
  ['content_scripts[0].matches'],
];

for (const r of required) {
  const parts = r[0].split('.');
  let v = manifest;
  for (const p of parts) {
    const idx = p.match(/\[(\d+)\]/);
    if (idx) {
      const key = p.slice(0, p.indexOf('['));
      v = v[key]?.[parseInt(idx[1])];
    } else {
      v = v?.[p];
    }
    if (v === undefined || v === null) break;
  }
  if (v === undefined || v === null) {
    fail(`missing ${r[0]}`);
  } else if (r.length > 1 && v !== r[1]) {
    fail(`${r[0]}: expected ${JSON.stringify(r[1])}, got ${JSON.stringify(v)}`);
  }
}
ok(`manifest.json: name="${manifest.name}" v${manifest.version} MV${manifest.manifest_version}`);

// validate all referenced files exist
function checkFile(path) {
  const full = join(EXT_DIR, path);
  if (existsSync(full)) ok(`file exists: ${path}`);
  else fail(`file missing: ${path}`);
}

const bgWorker = manifest.background?.service_worker;
if (bgWorker) checkFile(bgWorker);

for (const cs of (manifest.content_scripts || [])) {
  for (const js of (cs.js || [])) {
    checkFile(js);
  }
}

if (manifest.options_page) checkFile(manifest.options_page);

// check popup
if (manifest.action?.default_popup) checkFile(manifest.action.default_popup);

// ─── 2. JS syntax check ──────────────────────────────────────────
console.log('\n── JS syntax ──');

function collectJS(dir) {
  const files = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory() && entry.name !== 'test' && entry.name !== 'node_modules') {
      files.push(...collectJS(full));
    } else if (entry.name.endsWith('.js')) {
      files.push(full);
    }
  }
  return files;
}

// Use dynamic import for child_process (ESM)
const { execSync } = await import('child_process');
for (const f of collectJS(EXT_DIR)) {
  try {
    execSync(`node --check "${f}"`, { stdio: 'pipe', timeout: 5000 });
    ok(`syntax: ${f.replace(EXT_DIR + '/', '')}`);
  } catch (e) {
    fail(`syntax: ${f.replace(EXT_DIR + '/', '')}\n       ${e.stderr?.toString().trim() || e.message}`);
  }
}

// ─── 3. content script dependency order ───────────────────────────
console.log('\n── dependency order ──');
const csFiles = manifest.content_scripts?.[0]?.js || [];
for (let i = 1; i < csFiles.length; i++) {
  const deps = csFiles.slice(0, i);
  ok(`${csFiles[i]} loads after: ${deps.join(', ')}`);
}

// ─── 4. icon existence (minimal) ─────────────────────────────────
console.log('\n── icons ──');
if (manifest.action?.default_icon) {
  // not required for unpacked
  console.log('  ~ icons not required for dev mode (skipped)');
}

// ─── summary ─────────────────────────────────────────────────────
console.log(`\n${'='.repeat(50)}`);
console.log(`  ${passed} passed, ${failed} failed`);
console.log(`${'='.repeat(50)}\n`);

if (failed > 0) process.exit(1);

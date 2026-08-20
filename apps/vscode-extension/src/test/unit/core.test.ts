import './vscodeStub';
import * as assert from 'node:assert';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { createArgs, parseEnvelope, CtlError } from '../../core/ctl';
import { formatCost, isActive, isFailed, runContext, stateIcon, type Run } from '../../core/model';
import { describeRule, isValidHHMM, isValidLocalDateTime, parseRule } from '../../core/schedule';
import { encodeCwd, listSessions, peekTitle, foldersForSession } from '../../core/sessions';
import { watchMarker } from '../../core/watch';

// ---------- ctl contract ----------

describe('ctl contract', () => {
  it('parses the ok envelope', () => {
    const data = parseEnvelope<{ version: string }>('{"ok":true,"data":{"version":"0.1.0"}}', ['version']);
    assert.strictEqual(data.version, '0.1.0');
  });
  it('turns an error envelope into a CtlError with the message', () => {
    assert.throws(
      () => parseEnvelope('{"ok":false,"error":"sql: no rows in result set"}', ['tarea', 'x']),
      (e: unknown) => e instanceof CtlError && /no rows/.test(e.message),
    );
  });
  it('refuses output that is not JSON', () => {
    assert.throws(() => parseEnvelope('fatal error: all goroutines are asleep', ['listar']), CtlError);
  });
  it('builds create arguments as a list, never a shell string', () => {
    const a = createArgs({
      proyecto: 'C:\\repo with spaces',
      nombre: 'Weekly "maint"',
      agente: 'claude',
      prompt: 'do it; rm -rf /',
      regla: 'weekly',
      hora: '04:00',
      dias: [1, 5],
      perfil: 'auditoria',
      presupuesto: 2,
    });
    assert.deepStrictEqual(a.slice(0, 2), ['--proyecto', 'C:\\repo with spaces']);
    assert.ok(a.includes('do it; rm -rf /'), 'the prompt travels as one argument, unquoted');
    const d = a.indexOf('--dias');
    assert.deepStrictEqual(a.slice(d, d + 2), ['--dias', '1,5']);
    const p = a.indexOf('--presupuesto');
    assert.deepStrictEqual(a.slice(p, p + 2), ['--presupuesto', '2']);
    assert.ok(!a.includes('--sesion'), 'unset optional flags are omitted');
  });
  it('omits empty optional values on edit', () => {
    assert.deepStrictEqual(createArgs({ prompt: 'new prompt' }), ['--prompt', 'new prompt']);
  });
});

// ---------- model ----------

const run = (estado: string): Run => ({
  id: 'r', tarea_id: 't', tarea: 'T', proyecto: 'P', ruta_proyecto: '', raiz_git: '', rama_base: 'main', estado,
  prevista_utc: '', inicio: '', fin: '', worktree: '', rama: '', commit_base: '', coste_usd: null, resumen: '',
  codigo_error: '', sesion_proveedor: '', ficheros: [], decision: '', cancelando: false, agente: 'claude',
});

describe('model', () => {
  it('maps every state to a context value the menus know', () => {
    assert.strictEqual(runContext(run('running')), 'run.active');
    assert.strictEqual(runContext(run('preflight')), 'run.active');
    assert.strictEqual(runContext(run('awaiting_review')), 'run.reviewEmpty', 'an audit with no files is read-and-archive');
    assert.strictEqual(runContext({ ...run('awaiting_review'), ficheros: ['a.py'] }), 'run.review');
    assert.strictEqual(runContext(run('accepted')), 'run.done');
    assert.strictEqual(runContext(run('failed_auth')), 'run.failed');
    assert.strictEqual(runContext(run('skipped')), 'run.other');
  });
  it('classifies active and failed states', () => {
    assert.ok(isActive('verifying'));
    assert.ok(!isActive('awaiting_review'));
    assert.ok(isFailed('failed_quota'));
    assert.ok(!isFailed('rejected'));
  });
  it('gives every known state a codicon, never emoji', () => {
    const states = ['queued', 'preflight', 'running', 'verifying', 'awaiting_review', 'accepted', 'rejected', 'skipped',
      'failed', 'failed_verification', 'failed_quota', 'failed_auth', 'cancelled', 'whatever'];
    for (const s of states) {
      const ic = stateIcon(s);
      assert.ok(ic.id && /^[a-z~-]+$/i.test(ic.id), `${s} → ${ic.id}`);
    }
  });
  it('formats cost', () => {
    assert.strictEqual(formatCost(0.4), '0.40 USD');
    assert.strictEqual(formatCost(null), '');
  });
});

// ---------- schedule helpers ----------

describe('schedule helpers', () => {
  it('parses what ctl stores', () => {
    assert.deepStrictEqual(parseRule('{"type":"weekly","time":"04:00","weekdays":[1,3]}'), { type: 'weekly', time: '04:00', weekdays: [1, 3] });
    assert.strictEqual(parseRule('garbage'), undefined);
    assert.strictEqual(parseRule('{"type":"monthly"}'), undefined);
  });
  it('describes rules for people', () => {
    assert.strictEqual(describeRule({ type: 'daily', time: '03:00' }), 'daily at 03:00');
    assert.strictEqual(describeRule({ type: 'weekly', time: '09:00', weekdays: [1, 5] }), 'Mon, Fri at 09:00');
    assert.strictEqual(describeRule({ type: 'once', at_local: '2026-09-01T03:00' }), 'once, 2026-09-01T03:00');
    assert.strictEqual(describeRule(undefined), 'unknown schedule');
  });
  it('validates times', () => {
    assert.ok(isValidHHMM('03:00') && isValidHHMM('23:59'));
    assert.ok(!isValidHHMM('24:00') && !isValidHHMM('3:00') && !isValidHHMM('03:60'));
    assert.ok(isValidLocalDateTime('2026-09-01T03:00'));
    assert.ok(!isValidLocalDateTime('2026-13-01T03:00') && !isValidLocalDateTime('01/09/2026 03:00'));
  });
});

// ---------- claude session discovery ----------

describe('claude session discovery', () => {
  let home: string;
  before(() => {
    home = fs.mkdtempSync(path.join(os.tmpdir(), 'tk-sess-'));
  });
  after(() => fs.rmSync(home, { recursive: true, force: true }));

  it('encodes the cwd the way Claude Code documents', () => {
    assert.strictEqual(encodeCwd('C:\\Users\\kirne'), 'C--Users-kirne');
    assert.strictEqual(encodeCwd('/home/x/proj'), '-home-x-proj');
  });

  it('lists sessions by file name and mtime, newest first, case-insensitively', () => {
    const cwd = 'C:\\Proj\\Demo';
    const dir = path.join(home, 'projects', 'c--proj-demo'); // the folder keeps the case of the cwd it was created with
    fs.mkdirSync(dir, { recursive: true });
    const a = path.join(dir, '11111111-1111-1111-1111-111111111111.jsonl');
    const b = path.join(dir, '22222222-2222-2222-2222-222222222222.jsonl');
    fs.writeFileSync(a, JSON.stringify({ type: 'queue-operation' }) + '\n' + JSON.stringify({ type: 'user', message: { role: 'user', content: 'Fix the login bug   please' } }) + '\n');
    fs.writeFileSync(b, JSON.stringify({ type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'Write   docs' }] } }) + '\n');
    fs.writeFileSync(path.join(dir, 'not-a-session.txt'), 'x');
    fs.mkdirSync(path.join(dir, '33333333-3333-3333-3333-333333333333')); // subagent folder, ignored
    const past = new Date(Date.now() - 60_000);
    fs.utimesSync(a, past, past);

    const found = listSessions(cwd, { CLAUDE_CONFIG_DIR: home });
    assert.deepStrictEqual(found.map((s) => s.id.slice(0, 8)), ['22222222', '11111111']);
    assert.strictEqual(found[0].title, 'Write docs');
    assert.strictEqual(found[1].title, 'Fix the login bug please');
  });

  it('degrades to an empty title on an unreadable transcript, never throws', () => {
    const f = path.join(home, 'weird.jsonl');
    fs.writeFileSync(f, 'this is not json\n{"type":"assistant"}\n');
    assert.strictEqual(peekTitle(f), '');
    assert.strictEqual(peekTitle(path.join(home, 'missing.jsonl')), '');
  });

  it('returns nothing when the config dir does not exist', () => {
    assert.deepStrictEqual(listSessions('C:\\x', { CLAUDE_CONFIG_DIR: path.join(home, 'nope') }), []);
  });

  it('finds the real folder(s) a session id lives in, matching cwd to the encoded dir', () => {
    const id = 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee';
    const f2 = fs.mkdtempSync(path.join(os.tmpdir(), 'tk-conv-'));
    // Same id stored under two real folders (as a cross-folder resume would do).
    for (const f of [home, f2]) {
      const dir = path.join(home, 'projects', encodeCwd(f));
      fs.mkdirSync(dir, { recursive: true });
      // First line has a stale/original cwd; the matching one appears later.
      fs.writeFileSync(
        path.join(dir, `${id}.jsonl`),
        JSON.stringify({ type: 'system', cwd: 'C:\\Somewhere\\Else' }) + '\n' + JSON.stringify({ type: 'system', cwd: f }) + '\n',
      );
    }
    const cwds = foldersForSession(id, { CLAUDE_CONFIG_DIR: home }).map((l) => l.cwd).sort();
    assert.deepStrictEqual(cwds, [home, f2].sort());
    fs.rmSync(f2, { recursive: true, force: true });
  });

  it('returns nothing for an unknown session', () => {
    assert.deepStrictEqual(foldersForSession('ffffffff-0000-0000-0000-000000000000', { CLAUDE_CONFIG_DIR: home }), []);
  });
});

// ---------- marker watcher ----------

describe('marker watcher', () => {
  it('fires (debounced) when the marker is touched', async () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'tk-watch-'));
    const marker = path.join(dir, 'cambios.marca');
    let fired = 0;
    const stop = watchMarker(marker, () => fired++, 150);
    try {
      await new Promise((r) => setTimeout(r, 150));
      fs.writeFileSync(marker, 'a');
      fs.writeFileSync(marker, 'b');
      fs.writeFileSync(marker, 'c');
      await new Promise((r) => setTimeout(r, 800));
      assert.ok(fired >= 1 && fired <= 2, `fired ${fired} times`);
    } finally {
      stop();
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
});

// ---------- l10n manifest ----------

describe('l10n manifest', () => {
  const root = path.resolve(__dirname, '..', '..', '..');
  const pkg = fs.readFileSync(path.join(root, 'package.json'), 'utf8');
  const en = JSON.parse(fs.readFileSync(path.join(root, 'package.nls.json'), 'utf8')) as Record<string, string>;
  const es = JSON.parse(fs.readFileSync(path.join(root, 'package.nls.es.json'), 'utf8')) as Record<string, string>;
  it('has every %key% in English', () => {
    const used = [...pkg.matchAll(/%([a-zA-Z0-9_.]+)%/g)].map((m) => m[1]);
    for (const k of used) assert.ok(k in en, `missing in package.nls.json: ${k}`);
  });
  it('has the same keys in Spanish', () => {
    assert.deepStrictEqual(Object.keys(es).sort(), Object.keys(en).sort());
  });
});

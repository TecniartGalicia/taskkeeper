import * as assert from 'node:assert';
import type { Run, RunEvent } from '../../core/model';
import { buildTranscript } from '../../core/transcript';

// Fixtures modelled on the real `prueba2` run (tailscale/ping), including the
// noise that must be dropped: thinking_tokens and an allowed rate_limit_event.
function ev(tipo: string, payload: unknown, seq: number): RunEvent {
  return { seq, tipo, en: '2026-08-19T15:15:00Z', payload: JSON.stringify(payload) };
}

const PRUEBA2: RunEvent[] = [
  ev('sesion_iniciada', { type: 'system', subtype: 'init', cwd: 'C:\\Users\\k\\AppData\\Roaming\\Argalla\\TaskKeeper\\worktrees\\taskkeeper_prueba2_x', session_id: '527a02' }, 1),
  ev('mensaje', { type: 'rate_limit_event', rate_limit_info: { status: 'allowed' } }, 2),
  ev('mensaje', { type: 'system', subtype: 'thinking_tokens', estimated_tokens: 50 }, 3),
  ev('mensaje', { type: 'assistant', message: { content: [{ type: 'thinking', thinking: '' }] } }, 4),
  ev('mensaje', { type: 'assistant', message: { content: [{ type: 'text', text: 'Voy a buscar el equipo en la red Tailscale y hacerle ping.' }] } }, 5),
  ev('mensaje', { type: 'assistant', message: { content: [{ type: 'tool_use', id: 't1', name: 'PowerShell', input: { command: 'tailscale status' } }] } }, 6),
  ev('mensaje', { type: 'user', message: { content: [{ tool_use_id: 't1', type: 'tool_result', content: '100.122.147.39 desktop-bhpuesg' }] } }, 7),
  ev('mensaje', { type: 'assistant', message: { content: [{ type: 'text', text: 'No hay ningún equipo cuya IP termine en 149.37. Le hago ping…' }] } }, 8),
  ev('mensaje', { type: 'assistant', message: { content: [{ type: 'tool_use', id: 't2', name: 'PowerShell', input: { command: 'ping -n 4 100.122.147.39' } }] } }, 9),
  ev('mensaje', { type: 'user', message: { content: [{ tool_use_id: 't2', type: 'tool_result', content: 'Respuesta desde 100.122.147.39: tiempo=74ms TTL=128' }] } }, 10),
  ev('resultado', { type: 'result', is_error: false, total_cost_usd: 0.290762, num_turns: 3, result: 'En Tailscale no existe ningún equipo con IP acabada en 149.37. desktop-bhpuesg responde.' }, 11),
];

function run(over: Partial<Run>): Run {
  return {
    estado: 'awaiting_review', agente: 'claude', ficheros: [], coste_usd: 0.29,
    worktree: 'C:\\...\\worktrees\\x', ...over,
  } as Run;
}

describe('buildTranscript', () => {
  it('drops thinking_tokens, allowed rate limits and empty thinking', () => {
    const { items } = buildTranscript(run({}), PRUEBA2);
    assert.ok(!items.some((i) => i.kind === 'log'), 'no raw log lines should survive');
    assert.ok(!items.some((i) => i.kind === 'thought'), 'empty thinking is dropped');
    assert.ok(!items.some((i) => (i.text ?? '').includes('thinking_tokens')));
  });

  it('extracts the assistant messages and the final answer', () => {
    const { items } = buildTranscript(run({}), PRUEBA2);
    const says = items.filter((i) => i.kind === 'say').map((i) => i.text);
    assert.strictEqual(says.length, 2);
    assert.match(says[0]!, /Tailscale y hacerle ping/);
    const final = items.find((i) => i.kind === 'final');
    assert.ok(final, 'a final item exists');
    assert.strictEqual(final!.error, false);
    assert.match(final!.text!, /desktop-bhpuesg responde/);
  });

  it('pairs each tool call with its output', () => {
    const { items } = buildTranscript(run({}), PRUEBA2);
    const tools = items.filter((i) => i.kind === 'tool');
    assert.strictEqual(tools.length, 2);
    assert.strictEqual(tools[0].tool, 'PowerShell');
    assert.strictEqual(tools[0].command, 'tailscale status');
    assert.match(tools[0].output!, /desktop-bhpuesg/);
    assert.match(tools[1].output!, /74ms/);
  });

  it('summarises cost, turns, files and isolation', () => {
    const { summary } = buildTranscript(run({ coste_usd: 0.29, ficheros: ['a.ts'] }), PRUEBA2);
    assert.strictEqual(summary.state, 'awaiting_review');
    assert.strictEqual(summary.turns, 3);
    assert.strictEqual(summary.files, 1);
    assert.strictEqual(summary.isolated, true);
    assert.ok(Math.abs((summary.costUSD ?? 0) - 0.29) < 1e-6);
  });

  it('marks a completed direct run as not isolated', () => {
    const evs: RunEvent[] = [
      ev('sesion_iniciada', { type: 'system', subtype: 'init', cwd: 'C:\\repo\\real', session_id: 's' }, 1),
      ev('resultado', { type: 'result', is_error: false, result: 'listo' }, 2),
    ];
    const { summary, items } = buildTranscript(run({ estado: 'completed', worktree: '', workspace_mode: 'direct' }), evs);
    assert.strictEqual(summary.isolated, false);
    assert.match(items[0].text!, /directly in the repository/);
  });

  it('surfaces an errored result and never throws on bad JSON', () => {
    const evs: RunEvent[] = [
      { seq: 1, tipo: 'mensaje', en: 't', payload: 'no soy json {' },
      ev('resultado', { type: 'result', is_error: true, result: '' }, 2),
    ];
    const { items } = buildTranscript(run({ estado: 'failed', codigo_error: 'exit_1' }), evs);
    assert.ok(items.some((i) => i.kind === 'log'), 'bad JSON becomes a log line');
    const final = items.find((i) => i.kind === 'final');
    assert.strictEqual(final!.error, true);
  });
});

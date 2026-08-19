// Thin, typed wrapper around `taskkeeper-ctl --json`.
//
// The extension never opens the SQLite database itself: that would need a
// native module compiled per platform and Electron version. Instead every read
// and write goes through the Go binary, which is the single place that knows
// how the data is stored. Spawning an 11 MB static binary costs a few dozen
// milliseconds, well within what a tree view needs.
import { execFile } from 'node:child_process';
import type { Agent, Environment, Run, RunEvent, Task } from './model';

export interface CtlResult<T> {
  ok: boolean;
  data?: T;
  error?: string;
}

export class CtlError extends Error {
  constructor(message: string, readonly args: string[]) {
    super(message);
    this.name = 'CtlError';
  }
}

export interface CtlOptions {
  /** Absolute path to taskkeeper-ctl(.exe). */
  exe: string;
  /** Extra environment for the child (TASKKEEPER_HOME, …). */
  env?: NodeJS.ProcessEnv;
  timeoutMs?: number;
}

/** Parses the JSON envelope; exported so the contract test can exercise it without spawning. */
export function parseEnvelope<T>(stdout: string, args: string[]): T {
  let parsed: CtlResult<T>;
  try {
    parsed = JSON.parse(stdout) as CtlResult<T>;
  } catch {
    throw new CtlError(`taskkeeper-ctl returned something that is not JSON: ${stdout.slice(0, 200)}`, args);
  }
  if (!parsed.ok) throw new CtlError(parsed.error ?? 'unknown error', args);
  return parsed.data as T;
}

export class Ctl {
  constructor(private readonly opts: CtlOptions) {}

  private call<T>(...args: string[]): Promise<T> {
    const full = ['--json', ...args];
    return new Promise<T>((resolve, reject) => {
      execFile(
        this.opts.exe,
        full,
        {
          env: { ...process.env, ...(this.opts.env ?? {}) },
          timeout: this.opts.timeoutMs ?? 60_000,
          windowsHide: true,
          maxBuffer: 16 * 1024 * 1024,
        },
        (err, stdout, stderr) => {
          // ctl exits 1 on a business error but still prints the JSON envelope;
          // prefer that over the generic "exit code 1".
          if (stdout && stdout.trim().startsWith('{')) {
            try {
              resolve(parseEnvelope<T>(stdout, full));
              return;
            } catch (e) {
              reject(e);
              return;
            }
          }
          if (err) {
            reject(new CtlError(`${err.message}${stderr ? ` · ${stderr.trim()}` : ''}`, full));
            return;
          }
          reject(new CtlError(`empty output${stderr ? ` · ${stderr.trim()}` : ''}`, full));
        },
      );
    });
  }

  version(): Promise<{ version: string }> {
    return this.call('version');
  }
  environment(): Promise<Environment> {
    return this.call('entorno');
  }
  agents(): Promise<Agent[]> {
    return this.call('agentes');
  }
  tasks(): Promise<Task[]> {
    return this.call('listar');
  }
  task(id: string): Promise<Task> {
    return this.call('tarea', id);
  }
  inbox(): Promise<Run[]> {
    return this.call('bandeja');
  }
  history(limit = 50): Promise<Run[]> {
    return this.call('historial', '--limite', String(limit));
  }
  run(id: string): Promise<Run> {
    return this.call('ejecucion', id);
  }
  events(runId: string, since = 0): Promise<RunEvent[]> {
    return this.call('eventos', runId, '--desde', String(since));
  }
  concurrency(): Promise<{ cupo: number }> {
    return this.call('cupo');
  }
  setConcurrency(n: number): Promise<{ cupo: number }> {
    return this.call('cupo', String(n));
  }

  create(p: CreateParams): Promise<{ id: string; next_run_utc: string; next_run_local: string; avisos: string[] }> {
    return this.call('crear', ...createArgs(p));
  }
  edit(id: string, p: Partial<CreateParams>): Promise<{ id: string; next_run_local: string; avisos: string[] }> {
    return this.call('editar', id, ...createArgs(p));
  }
  delete(id: string): Promise<{ id: string }> {
    return this.call('borrar', id);
  }
  pause(id: string): Promise<{ id: string; activa: boolean }> {
    return this.call('pausar', id);
  }
  resume(id: string): Promise<{ id: string; activa: boolean }> {
    return this.call('reanudar', id);
  }
  runNow(id: string): Promise<{ id: string; lanzada: boolean }> {
    return this.call('ahora', id);
  }
  cancel(runId: string): Promise<{ id: string }> {
    return this.call('cancelar', runId);
  }
  accept(runId: string): Promise<{ id: string; decision: string }> {
    return this.call('aceptar', runId);
  }
  reject(runId: string): Promise<{ id: string; decision: string }> {
    return this.call('rechazar', runId);
  }
  archive(runId: string): Promise<{ id: string; decision: string }> {
    return this.call('archivar', runId);
  }
}

export interface CreateParams {
  proyecto: string;
  nombre: string;
  agente: string;
  prompt: string;
  regla: 'daily' | 'weekly' | 'once';
  hora: string; // HH:MM, or YYYY-MM-DDTHH:MM for once
  dias?: number[];
  zona?: string;
  perfil?: string;
  rama?: string;
  modo?: string;
  sesion?: string;
  politica?: string;
  retrasoMax?: number;
  timeout?: number;
  presupuesto?: number;
  presupuestoDiario?: number;
}

/** Builds the argument list; exported for the unit test. Never a shell string. */
export function createArgs(p: Partial<CreateParams>): string[] {
  const a: string[] = [];
  const put = (flag: string, v: string | number | undefined) => {
    if (v === undefined || v === '' || v === null) return;
    a.push(flag, String(v));
  };
  put('--proyecto', p.proyecto);
  put('--nombre', p.nombre);
  put('--agente', p.agente);
  put('--prompt', p.prompt);
  put('--regla', p.regla);
  put('--hora', p.hora);
  if (p.dias && p.dias.length) a.push('--dias', p.dias.join(','));
  put('--zona', p.zona);
  put('--perfil', p.perfil);
  put('--rama', p.rama);
  put('--modo', p.modo);
  put('--sesion', p.sesion);
  put('--politica', p.politica);
  put('--retraso-max', p.retrasoMax);
  put('--timeout', p.timeout);
  put('--presupuesto', p.presupuesto);
  put('--presupuesto-diario', p.presupuestoDiario);
  return a;
}

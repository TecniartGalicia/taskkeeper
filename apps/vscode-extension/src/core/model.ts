// Types mirrored from taskkeeper-ctl --json. If ctl changes a field, this file
// and the contract test in test/unit/ctl.test.ts must change with it.

export type RunState =
  | 'scheduled'
  | 'queued'
  | 'preflight'
  | 'running'
  | 'verifying'
  | 'awaiting_review'
  | 'accepted'
  | 'rejected'
  | 'completed'
  | 'skipped'
  | 'failed'
  | 'failed_verification'
  | 'failed_quota'
  | 'failed_auth'
  | 'cancelled';

export interface Task {
  id: string;
  nombre: string;
  proyecto: string;
  ruta_proyecto: string;
  agente: 'claude' | 'codex' | string;
  activa: boolean;
  modo: 'new' | 'resume' | 'fork' | string;
  sesion_externa?: string;
  autocompact?: string;
  workspace_mode?: string;
  depende_de?: string; // §27.4 — id de la tarea padre; '' si no depende de ninguna
  disparar_en?: string; // success | failure | always
  regla: string; // JSON of the scheduler rule
  zona: string;
  perfil: 'auditoria' | 'cambios_aislados' | string;
  politica: 'skip' | 'run_if_late' | 'manual' | string;
  timeout_seg: number;
  retraso_max_seg: number;
  presupuesto_usd: number | null;
  presupuesto_diario_usd: number | null;
  proxima_utc: string;
  proxima_local: string;
  prompt: string;
  ultimo_estado: string;
  ultima_ejecucion: string;
}

export interface Run {
  id: string;
  tarea_id: string;
  tarea: string;
  proyecto: string;
  ruta_proyecto: string;
  raiz_git: string;
  rama_base: string;
  estado: RunState | string;
  prevista_utc: string;
  inicio: string;
  fin: string;
  worktree: string;
  rama: string;
  commit_base: string;
  coste_usd: number | null;
  resumen: string;
  codigo_error: string;
  sesion_proveedor: string;
  ficheros: string[];
  decision: string;
  workspace_mode?: string;
  cancelando: boolean;
  agente: string;
}

export interface Agent {
  nombre: string;
  ruta: string;
  version: string;
  instalado: boolean;
  retomar: boolean;
  derivar: boolean;
}

export interface Environment {
  raiz: string;
  db: string;
  worker: string;
  marca: string;
  aviso_reactivacion: string;
  version: string;
}

export interface RunEvent {
  seq: number;
  tipo: string;
  payload: string;
  en: string;
}

export interface ScheduleRule {
  type: 'once' | 'daily' | 'weekly';
  at_local?: string;
  time?: string;
  times?: string[];
  weekdays?: number[];
}

export const ACTIVE_STATES: ReadonlySet<string> = new Set(['queued', 'preflight', 'running', 'verifying']);
export const REVIEW_STATE = 'awaiting_review';
export const DONE_STATES: ReadonlySet<string> = new Set(['accepted', 'rejected', 'completed']);
export const FAILED_STATES: ReadonlySet<string> = new Set([
  'failed',
  'failed_verification',
  'failed_quota',
  'failed_auth',
  'cancelled',
]);

export function isActive(s: string): boolean {
  return ACTIVE_STATES.has(s);
}
export function isFailed(s: string): boolean {
  return FAILED_STATES.has(s);
}

/** Context value for tree items: drives which inline actions appear. */
export function runContext(r: Run): 'run.active' | 'run.review' | 'run.reviewEmpty' | 'run.done' | 'run.failed' | 'run.other' {
  if (isActive(r.estado)) return 'run.active';
  // An audit that finished without touching files has nothing to merge:
  // it is "read and archive", not "accept or reject".
  if (r.estado === REVIEW_STATE) return r.ficheros.length ? 'run.review' : 'run.reviewEmpty';
  if (DONE_STATES.has(r.estado)) return 'run.done';
  if (isFailed(r.estado)) return 'run.failed';
  return 'run.other';
}

/** Codicon id per state. Not emoji: VS Code's own icon font. */
export function stateIcon(s: string): { id: string; color?: string } {
  switch (s) {
    case 'queued':
    case 'preflight':
      return { id: 'clock', color: 'charts.blue' };
    case 'running':
    case 'verifying':
      return { id: 'loading~spin', color: 'charts.blue' };
    case 'awaiting_review':
      return { id: 'git-pull-request', color: 'charts.orange' };
    case 'accepted':
      return { id: 'check', color: 'charts.green' };
    case 'completed':
      return { id: 'pass-filled', color: 'charts.green' };
    case 'rejected':
      return { id: 'discard', color: 'descriptionForeground' };
    case 'skipped':
      return { id: 'debug-step-over', color: 'descriptionForeground' };
    case 'failed_quota':
      return { id: 'watch', color: 'charts.yellow' };
    case 'failed_auth':
      return { id: 'key', color: 'charts.red' };
    case 'cancelled':
      return { id: 'debug-stop', color: 'descriptionForeground' };
    case 'failed':
    case 'failed_verification':
      return { id: 'error', color: 'charts.red' };
    default:
      return { id: 'circle-outline' };
  }
}

export function formatCost(usd: number | null | undefined): string {
  if (usd === null || usd === undefined) return '';
  return `${usd.toFixed(2)} USD`;
}

// §27.1 — resumen matinal + salud del planificador.
export interface DigestSummary {
  desde_utc: string;
  terminadas: number;
  esperan_revision: number;
  fallidas: number;
  saltadas: number;
  en_curso: number;
  coste_total_usd: number;
}
export interface HealthTask {
  tarea_id: string;
  tarea: string;
  registrada: boolean; // el disparador del SO sigue registrado
  proxima_local: string;
  disparos_perdidos: number;
  pendientes: number;
}
export interface Digest {
  resumen: DigestSummary;
  salud: HealthTask[];
}

// §27.2 — gasto + tope mensual.
export interface SpendTask {
  tarea_id: string;
  tarea: string;
  runs: number;
  usd: number;
}
export interface SpendDay {
  dia: string; // YYYY-MM-DD
  usd: number;
}
export interface Spend {
  mes_usd: number;
  tope_mes_usd: number; // 0 = sin tope
  por_tarea: SpendTask[];
  por_dia: SpendDay[];
}

/** "2026-08-25T03:15" or RFC3339 → short local time for a list. */
export function shortTime(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  const hm = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
  return sameDay ? hm : `${d.toLocaleDateString(undefined, { day: '2-digit', month: 'short' })} ${hm}`;
}

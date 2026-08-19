// Small helpers around the scheduler rule: parsing what ctl stores and
// rendering it for people. The real occurrence maths lives in Go
// (packages/scheduler); this file must never try to compute "next run".
import type { ScheduleRule } from './model';

export const ISO_WEEKDAYS: ReadonlyArray<{ iso: number; short: string; long: string }> = [
  { iso: 1, short: 'Mon', long: 'Monday' },
  { iso: 2, short: 'Tue', long: 'Tuesday' },
  { iso: 3, short: 'Wed', long: 'Wednesday' },
  { iso: 4, short: 'Thu', long: 'Thursday' },
  { iso: 5, short: 'Fri', long: 'Friday' },
  { iso: 6, short: 'Sat', long: 'Saturday' },
  { iso: 7, short: 'Sun', long: 'Sunday' },
];

export function parseRule(json: string): ScheduleRule | undefined {
  try {
    const r = JSON.parse(json) as ScheduleRule;
    if (r && (r.type === 'once' || r.type === 'daily' || r.type === 'weekly')) return r;
  } catch {
    /* fall through */
  }
  return undefined;
}

/** Human summary. Uses a translator so the caller can pass vscode.l10n.t. */
export function describeRule(
  r: ScheduleRule | undefined,
  t: (s: string, ...args: (string | number)[]) => string = (s, ...a) => fmt(s, a),
): string {
  if (!r) return t('unknown schedule');
  switch (r.type) {
    case 'once':
      return t('once, {0}', r.at_local ?? '');
    case 'daily':
      return t('daily at {0}', r.time ?? '');
    case 'weekly': {
      const days = (r.weekdays ?? [])
        .map((d) => ISO_WEEKDAYS.find((w) => w.iso === d)?.short ?? String(d))
        .join(', ');
      return t('{0} at {1}', days || t('no days'), r.time ?? '');
    }
  }
}

export function isValidHHMM(s: string): boolean {
  return /^([01]\d|2[0-3]):[0-5]\d$/.test(s.trim());
}

export function isValidLocalDateTime(s: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}T([01]\d|2[0-3]):[0-5]\d$/.test(s.trim())) return false;
  const d = new Date(s.trim());
  return !Number.isNaN(d.getTime());
}

/** Best guess of the machine's IANA zone; the user can override in the wizard. */
export function systemTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

function fmt(s: string, args: (string | number)[]): string {
  return s.replace(/\{(\d+)\}/g, (_, i) => String(args[Number(i)] ?? ''));
}

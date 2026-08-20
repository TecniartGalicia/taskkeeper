// §27.3 — Catálogo de plantillas de tarea. Precargan nombre, prompt, permisos,
// workspace y horario en el panel; el usuario ajusta lo que quiera. Sin backend.
//
// Los textos se localizan con vscode.l10n.t, así que `plantillas()` debe llamarse
// EN EL HOST al construir el mensaje `init` (no a nivel de módulo: sin el bundle
// cargado, t() devolvería inglés). Comillas dobles en textos con apóstrofo; nunca
// backticks (l10n-sync no ve los template literals).
import * as vscode from 'vscode';

const t = vscode.l10n.t;

export interface Plantilla {
  id: string;
  nombre: string;
  desc: string;
  prompt: string;
  perfil: 'auditoria' | 'cambios_aislados';
  workspace: 'isolated' | 'direct';
  regla: 'daily' | 'weekly';
  dias: number[]; // obligatorio para 'weekly'; [] para 'daily'
  horas: string[];
}

export function plantillas(): Plantilla[] {
  return [
    {
      id: 'deps',
      nombre: t('Update dependencies'),
      desc: t('Weekly: open dependency updates in a worktree for you to review.'),
      prompt: t(
        "Check this repository for outdated or vulnerable dependencies. Update the safe ones (patch/minor), run the build and tests, and leave the changes in this worktree for review. Do not update anything that breaks the build. Summarise what you changed and why.",
      ),
      perfil: 'cambios_aislados',
      workspace: 'isolated',
      regla: 'weekly',
      dias: [1],
      horas: ['03:00'],
    },
    {
      id: 'changelog',
      nombre: t('Draft changelog'),
      desc: t('Weekly, read-only: summarise the week’s merged changes.'),
      prompt: t(
        "Read the commits merged in the last 7 days and draft a concise changelog entry grouped by type (features, fixes, chores). Read only — do not change any files; put the draft in your final summary.",
      ),
      perfil: 'auditoria',
      workspace: 'isolated',
      regla: 'weekly',
      dias: [5],
      horas: ['08:00'],
    },
    {
      id: 'lint',
      nombre: t('Lint sweep'),
      desc: t('Daily: auto-fix safe lint issues in a worktree for review.'),
      prompt: t(
        "Run the project's linter and formatter. Fix only the safe, mechanical issues (formatting, imports, obvious lint rules). Do not change logic. Run the tests to confirm nothing broke, and leave the changes in this worktree for review.",
      ),
      perfil: 'cambios_aislados',
      workspace: 'isolated',
      regla: 'daily',
      dias: [],
      horas: ['03:00'],
    },
    {
      id: 'fixtests',
      nombre: t('Fix failing tests'),
      desc: t('Daily: if the suite is red, fix it in a worktree for review.'),
      prompt: t(
        "Run the test suite. If everything passes, stop and say so. If anything fails, investigate the cause and fix it in this worktree, keeping changes minimal. Do not delete or skip tests to make them pass. Summarise what failed and how you fixed it.",
      ),
      perfil: 'cambios_aislados',
      workspace: 'isolated',
      regla: 'daily',
      dias: [],
      horas: ['03:00'],
    },
  ];
}

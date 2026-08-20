// The visual task panel: one Webview that replaces the seven sequential dialogs.
//
// It reimplements no logic. Everything it needs from disk or from the scheduler
// goes through the extension host: repository browsing, session discovery, the
// "next run" preview (Go, packages/scheduler) and the create/edit calls all go
// back over postMessage to code that already exists. The webview is a form.
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as vscode from 'vscode';
import type { Ctl, CreateParams } from '../core/ctl';
import type { Agent, Task } from '../core/model';
import { parseRule, systemTimezone } from '../core/schedule';
import { listSessions, foldersForSession } from '../core/sessions';
import { plantillas } from './templates';

const t = vscode.l10n.t;

const PROMPT_TEMPLATE = [
  'Treat the current repository as the source of truth and re-read the relevant files first.',
  'Goal: <what must be true when you finish>.',
  'Allowed: read the code, run the tests, change files inside this worktree.',
  'Not allowed: git push, merge, deploy, or touching anything outside this worktree.',
  'When done, summarise: what changed, what you tested, risks and anything you left undone.',
].join('\n');

/** The message shapes exchanged with the webview. Kept small and explicit. */
interface SubmitPayload {
  proyecto: string;
  nombre: string;
  agente: string;
  prompt: string;
  regla: 'daily' | 'weekly' | 'once';
  horas: string[]; // one or more HH:MM (F2), or a single YYYY-MM-DDTHH:MM for once
  dias?: number[];
  zona: string;
  perfil: string;
  modo: string;
  sesion?: string;
  politica: string;
  presupuesto?: number;
  autocompact?: string;
  workspace?: string; // 'isolated' | 'direct'
  dependeDe?: string; // §27.4
  dispararEn?: string;
  runNow: boolean;
}

let current: vscode.WebviewPanel | undefined;

export async function openTaskPanel(
  ctl: Ctl,
  agents: Agent[],
  defaultTz: string,
  existing?: Task,
): Promise<void> {
  const editing = !!existing;
  // A single panel at a time. A previous panel is disposed and replaced rather
  // than revealed: revealing would show stale content (e.g. the New-task form
  // when the user asked to edit a specific task), silently dropping the request.
  if (current) {
    current.dispose();
    current = undefined;
  }
  const panel = vscode.window.createWebviewPanel(
    'taskkeeper.taskPanel',
    editing ? t('Edit task') : t('New task'),
    vscode.ViewColumn.Active,
    { enableScripts: true, retainContextWhenHidden: true },
  );
  current = panel;
  panel.onDidDispose(() => {
    current = undefined;
  });

  const installed = agents.filter((a) => a.instalado);
  const folders = (vscode.workspace.workspaceFolders ?? []).map((f) => ({ name: f.name, path: f.uri.fsPath }));
  const nonce = makeNonce();
  panel.webview.html = renderHtml(panel.webview, nonce, strings());

  panel.webview.onDidReceiveMessage(async (msg: { type: string; [k: string]: unknown }) => {
    try {
      switch (msg.type) {
        case 'ready': {
          // §27.4 — lista de tareas para elegir la padre (excluye la propia al
          // editar). Solo id+nombre; ligera.
          let tareas: { id: string; nombre: string; directo: boolean }[] = [];
          try {
            tareas = (await ctl.tasks())
              .filter((x) => !editing || x.id !== (existing as Task).id)
              // `directo`: el encadenado por «éxito» solo funciona si el padre corre
              // en la conversación (directo); si es aislado, se avisa (§27.4 v1).
              .map((x) => ({ id: x.id, nombre: x.nombre, directo: x.workspace_mode === 'direct' }));
          } catch { /* sin tareas: el selector de dependencia quedará vacío */ }
          panel.webview.postMessage({
            type: 'init',
            editing,
            agents: installed.map((a) => ({ nombre: a.nombre, version: a.version, retomar: a.retomar, derivar: a.derivar })),
            folders,
            defaultTz: defaultTz || systemTimezone(),
            promptTemplate: PROMPT_TEMPLATE,
            // §27.3 — plantillas resueltas AQUÍ (host), con el bundle l10n cargado.
            plantillas: editing ? [] : plantillas(),
            tareas,
            task: editing ? taskToForm(existing as Task) : undefined,
          });
          break;
        }
        case 'browseRepo': {
          const sel = await vscode.window.showOpenDialog({
            canSelectFolders: true, canSelectFiles: false, canSelectMany: false,
            title: t('Choose the Git repository'),
          });
          if (!sel || !sel.length) return;
          const p = sel[0].fsPath;
          const isGit = fs.existsSync(path.join(p, '.git'));
          panel.webview.postMessage({ type: 'repoPicked', path: p, name: path.basename(p), isGit });
          break;
        }
        case 'listSessions': {
          const cwd = String(msg.cwd ?? '');
          const agent = String(msg.agent ?? '');
          const list = agent === 'claude' && cwd ? listSessions(cwd) : [];
          panel.webview.postMessage({
            type: 'sessions',
            list: list.map((s) => ({ id: s.id, title: s.title, when: s.lastModified.toLocaleString() })),
          });
          break;
        }
        case 'resolveSession': {
          // From a session id, find the folder(s) the conversation lives in, so
          // the project is set from the conversation instead of picked by hand.
          const id = String(msg.id ?? '').trim();
          const locs = id ? foldersForSession(id) : [];
          panel.webview.postMessage({
            type: 'sessionFolders',
            folders: locs.map((l) => ({
              cwd: l.cwd,
              name: path.basename(l.cwd),
              isGit: fs.existsSync(path.join(l.cwd, '.git')),
            })),
          });
          break;
        }
        case 'preview': {
          // F2: the next occurrences, computed in Go. The webview never does date maths.
          const p = msg.payload as { regla: string; horas: string[]; dias?: number[]; zona: string };
          try {
            const res = await ctl.preview({ regla: p.regla, horas: p.horas, dias: p.dias, zona: p.zona });
            panel.webview.postMessage({ type: 'previewResult', ok: true, next: res.next });
          } catch (e) {
            panel.webview.postMessage({ type: 'previewResult', ok: false, error: (e as Error).message });
          }
          break;
        }
        case 'submit': {
          const p = msg.payload as SubmitPayload;
          // «Cambios directos»: en el repo real, sin poder rechazar. Se confirma
          // con un modal explícito antes de crear o editar.
          if (p.workspace === 'direct' && p.perfil === 'cambios_aislados') {
            const yes = t('Yes, write directly');
            const ok = await vscode.window.showWarningMessage(
              t('This task will make changes directly in your working copy, with no review step to reject them. Continue?'),
              { modal: true }, yes,
            );
            if (ok !== yes) return;
          }
          const params = toCreateParams(p);
          if (editing) {
            const res = await ctl.edit((existing as Task).id, params);
            for (const a of res.avisos) vscode.window.showWarningMessage(t('TaskKeeper: {0}', a));
            // next_run_local vacío = tarea dependiente (§27.4): sin horario propio.
            vscode.window.showInformationMessage(res.next_run_local
              ? t('Task updated. Next run: {0}.', res.next_run_local)
              : t('Task updated. It runs after another task.'));
          } else {
            const created = await ctl.create(params);
            for (const a of created.avisos) vscode.window.showWarningMessage(t('TaskKeeper: {0}', a));
            if (p.runNow) await ctl.runNow(created.id);
            const msg = !created.next_run_local
              ? t('Task created. It runs after another task.')
              : p.runNow
                ? t('Task created and launched. Next scheduled run: {0}. Watch the "Last night" view.', created.next_run_local)
                : t('Task created. Next run: {0}.', created.next_run_local);
            vscode.window.showInformationMessage(msg);
          }
          panel.dispose();
          await vscode.commands.executeCommand('taskkeeper.refresh');
          break;
        }
      }
    } catch (e) {
      panel.webview.postMessage({ type: 'error', message: (e as Error).message });
    }
  });
}

/** Maps a webview submit payload to the ctl CreateParams the binary understands. */
function toCreateParams(p: SubmitPayload): CreateParams {
  const once = p.regla === 'once';
  return {
    proyecto: p.proyecto,
    nombre: p.nombre.trim(),
    agente: p.agente,
    prompt: p.prompt,
    regla: p.regla,
    // once carries a single YYYY-MM-DDTHH:MM; daily/weekly carry one or more HH:MM joined by comma.
    hora: once ? (p.horas[0] ?? '') : p.horas.filter(Boolean).join(','),
    dias: p.dias,
    zona: p.zona,
    perfil: p.perfil,
    modo: p.modo,
    sesion: p.sesion,
    politica: p.politica,
    // timeout y retraso-max no se tocan aquí: el panel no los expone, así que se
    // dejan fuera para que `crear` use su valor por defecto y `editar` conserve
    // el de la tarea, en vez de machacarlos con una constante.
    presupuesto: p.presupuesto,
    autocompact: p.autocompact,
    workspace: p.workspace,
    dependeDe: p.dependeDe,
    dispararEn: p.dispararEn,
  };
}

/** Prefills the form when editing (F3). */
function taskToForm(task: Task): Record<string, unknown> {
  const rule = parseRule(task.regla ?? '');
  const horas = rule?.type === 'once' ? [rule.at_local ?? ''] : (rule?.times ?? (rule?.time ? [rule.time] : []));
  return {
    nombre: task.nombre,
    proyecto: task.ruta_proyecto,
    proyectoNombre: task.proyecto,
    agente: task.agente,
    prompt: task.prompt,
    regla: rule?.type ?? 'daily',
    horas,
    dias: rule?.weekdays ?? [],
    zona: task.zona ?? '',
    perfil: task.perfil ?? 'cambios_aislados',
    modo: task.modo ?? 'new',
    sesion: task.sesion_externa ?? '',
    politica: task.politica ?? 'skip',
    dependeDe: task.depende_de ?? '',
    dispararEn: task.disparar_en || 'success',
    presupuesto: task.presupuesto_usd ?? undefined,
    autocompact: task.autocompact ?? '',
    workspace: task.workspace_mode ?? 'isolated',
  };
}

function makeNonce(): string {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  let s = '';
  for (let i = 0; i < 32; i++) s += chars[Math.floor(Math.random() * chars.length)];
  return s;
}

/** All the user-facing strings, resolved on the host and handed to the webview. */
function strings(): Record<string, string> {
  return {
    title_new: t('New task'),
    title_edit: t('Edit task'),
    name: t('Name'),
    name_ph: t('Weekly maintenance'),
    repo: t('Repository'),
    browse: t('Browse…'),
    not_git: t('Not a Git repository'),
    repo_locked: t('Fixed for an existing task'),
    agent: t('Agent'),
    signed_in: t('signed in'),
    template: t('Template (optional)'),
    tpl_none: t('None'),
    tpl_none_d: t('Start from the generic scaffold.'),
    what_kind: t('What kind of task?'),
    preset_conv: t('In a conversation'),
    preset_conv_d: t('Continue an existing conversation. The work stays inside its chat; the folder is detected from the id.'),
    preset_iso: t('Isolated task in a repo'),
    preset_iso_d: t('Fresh work in a worktree of a repository. You review the diff and accept or reject.'),
    advanced: t('Advanced — mix conversation and workspace'),
    repo_from_conv: t('from the conversation'),
    repo_pick_conv_first: t('Detected when you choose the conversation'),
    context: t('Conversation'),
    ctx_new: t('New'),
    ctx_resume: t('Continue'),
    ctx_fork: t('Fork'),
    ctx_new_d: t('Starts fresh. Best for recurring reports and audits.'),
    ctx_resume_d: t('Appends to its history. For a one-off follow-up.'),
    ctx_fork_d: t('Copies the context into a new conversation. Best for recurring work.'),
    which_conv: t('Which conversation?'),
    session_ph: t('Or paste a session id'),
    folder_detected: t('Folder detected from the conversation'),
    folder_multi: t('This conversation exists in several folders — pick the right one:'),
    workspace: t('Where it works'),
    ws_isolated: t('Isolated'),
    ws_isolated_d: t('Worktree from the base commit. You review before anything reaches your branch.'),
    ws_direct: t('In the conversation'),
    ws_direct_d: t('Runs in the real repository and adds its turns to the conversation. No review step.'),
    ws_direct_changes: t('Direct changes'),
    ws_direct_warn: t('Writes directly in your working copy, without a review step.'),
    prompt: t('Instruction (prompt)'),
    runs_after: t('Runs after another task'),
    dep_none: t('On its own schedule'),
    dep_when: t('When the other task…'),
    dep_success: t('succeeds'),
    dep_failure: t('fails'),
    dep_always: t('finishes (either way)'),
    dep_note: t('No schedule of its own: it runs when the task above finishes.'),
    dep_only_direct: t('“Succeeds” chaining needs the parent to run in the conversation (direct); an isolated parent chains on failure only for now.'),
    schedule: t('Schedule'),
    every_day: t('Every day'),
    some_days: t('Some days'),
    once: t('Once'),
    add_time: t('+ time'),
    time_ph: t('HH:MM'),
    datetime_ph: t('2026-09-01T03:00'),
    tz: t('Time zone'),
    next_run: t('Next run'),
    permissions: t('Permissions'),
    perm_audit: t('Audit (read only)'),
    perm_audit_d: t('Read only. Cannot run commands.'),
    perm_iso: t('Isolated changes'),
    perm_iso_d: t('Runs in its own worktree. Nothing reaches your branch until you accept.'),
    misfire: t('If the scheduled time is missed'),
    mis_skip: t('Skip'),
    mis_late: t('Run if a little late'),
    mis_wait: t('Wait for me'),
    cap: t('Spending cap per run (USD)'),
    no_cap: t('no cap'),
    err_budget: t('A number like 2.00, or empty for no cap'),
    autocompact: t('Compact long conversation'),
    ac_default: t('Default (Claude Code decides)'),
    ac_auto: t('Automatic'),
    ac_hint: t('Claude Code already compacts on its own near the limit; set a window only if you want a predictable threshold.'),
    create: t('Create'),
    create_run: t('Create and run now'),
    save: t('Save changes'),
    mon: t('Mon'), tue: t('Tue'), wed: t('Wed'), thu: t('Thu'), fri: t('Fri'), sat: t('Sat'), sun: t('Sun'),
    err_name: t('Give it at least three characters'),
    err_repo: t('Choose a repository'),
    err_time: t('Add at least one valid time (HH:MM)'),
    err_session: t('Pick or paste a conversation'),
    summary: t('Summary'),
    isolation_note: t('Runs in a worktree from the base commit. Your branch is untouched until you accept.'),
  };
}

function renderHtml(webview: vscode.Webview, nonce: string, s: Record<string, string>): string {
  const csp = [
    `default-src 'none'`,
    `style-src ${webview.cspSource} 'unsafe-inline'`,
    `script-src 'nonce-${nonce}'`,
    `img-src ${webview.cspSource} data:`,
  ].join('; ');
  const S = JSON.stringify(s);
  return `<!DOCTYPE html>
<html lang="${vscode.env.language}">
<head>
<meta charset="UTF-8">
<meta http-equiv="Content-Security-Policy" content="${csp}">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style nonce="${nonce}">${CSS}</style>
</head>
<body>
<div id="app"></div>
<script nonce="${nonce}">const STR=${S};</script>
<script nonce="${nonce}">${SCRIPT}</script>
</body>
</html>`;
}

const CSS = `
:root{color-scheme:light dark}
*{box-sizing:border-box}
body{font-family:var(--vscode-font-family);font-size:13px;color:var(--vscode-foreground);background:var(--vscode-editor-background);margin:0;padding:0}
#app{max-width:760px;margin:0 auto;padding:22px 26px 60px;display:grid;grid-template-columns:1fr 230px;gap:26px}
.main{min-width:0}
.rail{position:sticky;top:22px;align-self:start}
.field{margin-bottom:18px}
.lbl{display:block;font-size:11px;text-transform:uppercase;letter-spacing:.06em;color:var(--vscode-descriptionForeground);margin-bottom:7px;font-weight:600}
.req{color:var(--vscode-editorWarning-foreground,#c90)}
input[type=text],textarea,select{width:100%;font-family:inherit;font-size:13px;color:var(--vscode-input-foreground);background:var(--vscode-input-background);border:1px solid var(--vscode-input-border,transparent);border-radius:4px;padding:7px 9px}
input[type=text]:focus,textarea:focus{outline:1px solid var(--vscode-focusBorder);border-color:var(--vscode-focusBorder)}
textarea{min-height:120px;resize:vertical;font-family:var(--vscode-editor-font-family,monospace);line-height:1.55}
.row{display:flex;gap:9px;flex-wrap:wrap;align-items:center}
.cards{display:grid;grid-template-columns:1fr 1fr;gap:10px}
.card{border:1px solid var(--vscode-input-border,var(--vscode-panel-border));border-radius:6px;padding:10px 12px;cursor:pointer;background:var(--vscode-editor-background)}
.card:hover{border-color:var(--vscode-focusBorder)}
.card.on{border-color:var(--vscode-focusBorder);background:var(--vscode-list-activeSelectionBackground);color:var(--vscode-list-activeSelectionForeground)}
.card .h{font-weight:600;display:flex;justify-content:space-between;align-items:center}
.card .d{color:var(--vscode-descriptionForeground);font-size:11.5px;margin-top:3px;line-height:1.4}
.card.on .d{color:inherit;opacity:.85}
.seg{display:inline-flex;border:1px solid var(--vscode-input-border,var(--vscode-panel-border));border-radius:6px;overflow:hidden}
.seg button{border:0;background:transparent;color:var(--vscode-foreground);padding:6px 13px;font-size:12.5px;cursor:pointer;border-right:1px solid var(--vscode-panel-border)}
.seg button:last-child{border-right:0}
.seg button.on{background:var(--vscode-button-background);color:var(--vscode-button-foreground)}
.chip{display:inline-flex;align-items:center;gap:6px;background:var(--vscode-badge-background);color:var(--vscode-badge-foreground);border-radius:5px;padding:5px 10px;font-size:12.5px;font-variant-numeric:tabular-nums}
.chip button{border:0;background:transparent;color:inherit;cursor:pointer;font-size:13px;opacity:.7;padding:0}
.chip button:hover{opacity:1}
.chip.on{outline:2px solid var(--vscode-focusBorder);outline-offset:1px}
.time-in{width:74px !important;text-align:center;font-variant-numeric:tabular-nums}
.btn{border:0;border-radius:5px;padding:8px 15px;font-size:13px;font-weight:600;cursor:pointer;font-family:inherit}
.btn.primary{background:var(--vscode-button-background);color:var(--vscode-button-foreground)}
.btn.primary:hover{background:var(--vscode-button-hoverBackground)}
.btn.ghost{background:var(--vscode-button-secondaryBackground);color:var(--vscode-button-secondaryForeground)}
.btn.small{padding:5px 10px;font-weight:500}
.link{background:none;border:0;color:var(--vscode-textLink-foreground);cursor:pointer;font-size:12.5px;padding:0}
.days{display:flex;gap:6px;flex-wrap:wrap}
.day{border:1px solid var(--vscode-panel-border);border-radius:5px;padding:5px 9px;font-size:12px;cursor:pointer;background:transparent;color:var(--vscode-foreground)}
.day.on{background:var(--vscode-button-background);color:var(--vscode-button-foreground);border-color:var(--vscode-button-background)}
.preview{margin-top:9px;font-size:12px;color:var(--vscode-descriptionForeground);min-height:18px}
.preview b{color:var(--vscode-foreground);font-variant-numeric:tabular-nums}
.picker{margin-top:9px;border:1px solid var(--vscode-panel-border);border-radius:6px;max-height:150px;overflow:auto}
.picker .opt{padding:7px 10px;cursor:pointer;border-bottom:1px solid var(--vscode-panel-border)}
.picker .opt:last-child{border-bottom:0}
.picker .opt:hover,.picker .opt.on{background:var(--vscode-list-hoverBackground)}
.picker .opt .m{color:var(--vscode-descriptionForeground);font-size:11px}
.actions{display:flex;gap:10px;margin-top:26px;padding-top:16px;border-top:1px solid var(--vscode-panel-border)}
.err{color:var(--vscode-inputValidation-errorForeground,#f48771);font-size:12px;margin-top:5px;min-height:14px}
.rail h4{margin:0 0 6px;font-size:11px;text-transform:uppercase;letter-spacing:.06em;color:var(--vscode-descriptionForeground)}
.rail .box{border:1px solid var(--vscode-panel-border);border-radius:7px;padding:12px;font-size:12.5px;line-height:1.6}
.rail .note{margin-top:12px;font-size:11.5px;color:var(--vscode-descriptionForeground);line-height:1.5}
.hide{display:none !important}
.money{display:inline-flex;align-items:center;gap:6px}
@media(max-width:680px){#app{grid-template-columns:1fr}.rail{position:static}}
`;

// The webview script. No inline handlers (CSP): everything via addEventListener.
const SCRIPT = String.raw`
const vscode = acquireVsCodeApi();
const $ = (sel,root=document)=>root.querySelector(sel);
const el = (tag,props={},...kids)=>{const e=document.createElement(tag);Object.assign(e,props);for(const k of kids)e.append(k);return e;};
let INIT=null, state={
  proyecto:'',proyectoNombre:'',isGit:true,nombre:'',agente:'',prompt:'',
  regla:'daily',horas:['03:00'],dias:[1],zona:'',perfil:'cambios_aislados',
  modo:'new',sesion:'',politica:'skip',presupuesto:'',autocompact:'',workspace:'isolated',editing:false,
  sessFolders:[],preset:'isolated',plantilla:'',dependeDe:'',dispararEn:'success'
};
let previewTimer=null, resolveTimer=null;

vscode.postMessage({type:'ready'});
window.addEventListener('message',(ev)=>{
  const m=ev.data;
  if(m.type==='init'){ INIT=m; boot(m); }
  else if(m.type==='repoPicked'){ state.proyecto=m.path; state.proyectoNombre=m.name; state.isGit=m.isGit; render(); }
  else if(m.type==='sessions'){ renderSessions(m.list); }
  else if(m.type==='sessionFolders'){ onSessionFolders(m.folders||[]); }
  else if(m.type==='previewResult'){ showPreview(m); }
  else if(m.type==='error'){ setErr('_form', m.message); }
});

function boot(m){
  state.editing=m.editing;
  state.zona=m.defaultTz;
  state.agente=(m.agents[0]&&m.agents[0].nombre)||'claude';
  state.prompt=m.promptTemplate;
  if(m.folders&&m.folders.length){ state.proyecto=m.folders[0].path; state.proyectoNombre=m.folders[0].name; }
  if(m.task){ Object.assign(state, normalizeTask(m.task)); }
  // El preset («en una conversación» / «tarea aislada») es sólo una vista sobre
  // los dos ejes reales (modo + workspace). Al editar lo derivamos de la tarea;
  // en una tarea nueva sale 'isolated' (new + isolated), que es el valor por defecto.
  state.preset = derivePreset(state.modo, state.workspace);
  render();
  schedulePreview();
}
// Traduce el par (conversación, dónde-trabaja) al preset que lo agrupa. Las
// combinaciones que no son ninguno de los dos atajos caen en 'advanced', donde
// se muestran los dos ejes por separado.
function derivePreset(modo, ws){
  if(modo!=='new' && ws==='direct') return 'conversation';
  if(modo==='new' && ws==='isolated') return 'isolated';
  return 'advanced';
}
// §27.3 — aplica una plantilla al estado (o la restaura a los valores por defecto
// si es «Ninguna»). Todas las plantillas son nueva+aislada, así que el preset
// resultante es 'isolated'.
function aplicarPlantilla(p){
  state.plantilla = p.id||'';
  if(!p.id){
    state.prompt=INIT.promptTemplate; state.nombre='';
    state.perfil='cambios_aislados'; state.workspace='isolated'; state.modo='new';
    state.regla='daily'; state.dias=[1]; state.horas=['03:00'];
  } else {
    state.nombre=p.nombre; state.prompt=p.prompt;
    state.perfil=p.perfil; state.workspace=p.workspace; state.modo='new';
    state.regla=p.regla;
    // Respeta el contrato: daily → sin días; weekly → los de la plantilla (o lunes).
    state.dias=(p.dias&&p.dias.length)?p.dias.slice():(p.regla==='weekly'?[1]:[]);
    state.horas=(p.horas&&p.horas.length)?p.horas.slice():['03:00'];
  }
  state.preset=derivePreset(state.modo,state.workspace);
  render(); schedulePreview(); // render() ya llama a updateSummary()
}
function normalizeTask(tk){
  const o={...tk};
  o.presupuesto = (tk.presupuesto==null?'':String(tk.presupuesto));
  o.horas = (tk.horas&&tk.horas.length)?tk.horas.slice():['03:00'];
  o.dias = (tk.dias&&tk.dias.length)?tk.dias.slice():[1];
  o.sesion = tk.sesion||'';
  return o;
}

// Nunca undefined: si el panel se abriera sin agentes, un objeto inerte evita
// que la lectura de .retomar/.derivar lance en el render.
function agent(){ return (INIT.agents||[]).find(a=>a.nombre===state.agente)||(INIT.agents||[])[0]||{nombre:state.agente,version:'',retomar:false,derivar:false}; }

function render(){
  const S=STR;
  const app=$('#app'); app.innerHTML='';
  const main=el('div',{className:'main'});
  const rail=el('div',{className:'rail'});
  app.append(main,rail);

  // §27.3 — Plantillas (opcional, solo tareas nuevas): precargan el formulario y
  // el usuario ajusta lo que quiera. «Ninguna» restaura el andamio genérico.
  if(!state.editing && (INIT.plantillas||[]).length){
    const plCards=el('div',{className:'cards'});
    const opciones=[{id:'',nombre:S.tpl_none,desc:S.tpl_none_d}].concat(INIT.plantillas);
    opciones.forEach(p=>{
      const c=el('div',{className:'card'+((state.plantilla||'')===p.id?' on':'')});
      c.append(el('div',{className:'h'},p.nombre), el('div',{className:'d'},p.desc||''));
      c.addEventListener('click',()=>aplicarPlantilla(p));
      plCards.append(c);
    });
    main.append(field(S.template, plCards));
  }

  const adv = state.preset==='advanced';
  const esDependiente = !!state.dependeDe; // §27.4: se dispara por otra tarea, sin horario propio

  // Intención: dos atajos que fijan los dos ejes por el usuario. El resto del
  // formulario (repo/conversación/dónde-trabaja) se muestra según el atajo, y
  // «Avanzado» los revela por separado para poder mezclarlos.
  const presetWrap=el('div',{});
  const pcards=el('div',{className:'cards'});
  [['conversation',S.preset_conv,S.preset_conv_d,'resume','direct'],
   ['isolated',S.preset_iso,S.preset_iso_d,'new','isolated']].forEach(([p,lbl,d,mo,ws])=>{
    const c=el('div',{className:'card'+(state.preset===p?' on':'')});
    c.append(el('div',{className:'h'},lbl), el('div',{className:'d'},d));
    c.addEventListener('click',()=>{
      // Si el agente no soporta «retomar», el atajo de conversación cae a modo
      // nueva; re-derivamos el preset para no dejar un estado incoherente
      // (preset 'conversation' con modo 'new' pintaría una sección vacía).
      const canResume = mo==='new' || agent().retomar;
      state.modo = canResume?mo:'new';
      state.workspace = ws;
      state.preset = canResume ? p : derivePreset(state.modo, state.workspace);
      state.sesion=''; state.sessFolders=[];
      // Al volver a un flujo aislado, si el proyecto apunta a una carpeta que no
      // está entre las abiertas (la fijó una conversación anterior), lo
      // devolvemos al repositorio por defecto para que el selector y lo que se
      // envía coincidan. En edición el repositorio es fijo y no se toca.
      if(!state.editing && ws==='isolated' && !(INIT.folders||[]).some(f=>f.path===state.proyecto)){
        const d=(INIT.folders||[])[0];
        if(d){ state.proyecto=d.path; state.proyectoNombre=d.name; state.isGit=true; }
        else { state.proyecto=''; state.proyectoNombre=''; }
      }
      render(); updateSummary(); // render() vuelve a pedir sesiones si hace falta
    });
    pcards.append(c);
  });
  presetWrap.append(pcards);
  if(!adv){
    const advLink=el('button',{className:'link',style:'margin-top:9px'}, S.advanced+' ▸');
    advLink.addEventListener('click',()=>{ state.preset='advanced'; render(); updateSummary(); });
    presetWrap.append(advLink);
  }
  main.append(field(S.what_kind, presetWrap));

  // Name
  main.append(field(S.name+' *', (()=>{ const i=el('input',{type:'text',value:state.nombre,placeholder:S.name_ph}); i.addEventListener('input',()=>{state.nombre=i.value; setErr('nombre',''); updateSummary();}); return wrapErr('nombre',i); })()));

  // Repository — con atajo «en una conversación» el repo lo fija la propia
  // conversación (autodetección de carpeta), así que se muestra de solo lectura.
  const repoFromConv = state.preset==='conversation';
  const repoRow=el('div',{className:'row'});
  const repoChip=el('span',{className:'chip'}, state.proyectoNombre||'—');
  // El aviso de «no es un repo Git» solo importa en modo aislado: en modo
  // directo («en la conversación») cualquier carpeta vale.
  if(state.proyectoNombre && !state.isGit && state.workspace!=='direct') repoChip.append(el('span',{className:'req'},' · '+S.not_git));
  if(state.editing){
    // El repositorio de una tarea es fijo: editar no lo reasigna. Se muestra
    // bloqueado en vez de ofrecer un cambio que se ignoraría en silencio.
    repoRow.append(repoChip, el('span',{style:'font-size:11.5px;color:var(--vscode-descriptionForeground)'}, S.repo_locked));
  } else if(repoFromConv){
    // Lo elige la conversación: solo lectura, con la nota de que se autodetecta.
    const note = state.sesion ? S.repo_from_conv : S.repo_pick_conv_first;
    repoRow.append(repoChip, el('span',{style:'font-size:11.5px;color:var(--vscode-descriptionForeground)'}, '· '+note));
  } else {
    const sel=el('select');
    (INIT.folders||[]).forEach(f=>{ const o=el('option',{value:f.path},f.name); if(f.path===state.proyecto)o.selected=true; sel.append(o); });
    if((INIT.folders||[]).length){ sel.addEventListener('change',()=>{ const f=INIT.folders.find(x=>x.path===sel.value); state.proyecto=f.path; state.proyectoNombre=f.name; state.isGit=true; render(); updateSummary(); }); repoRow.append(sel); }
    const browse=el('button',{className:'btn ghost small'},S.browse); browse.addEventListener('click',()=>vscode.postMessage({type:'browseRepo'}));
    repoRow.append(browse, repoChip);
  }
  main.append(field(S.repo, wrapErr('proyecto', repoRow)));

  // Agent
  const agCards=el('div',{className:'cards'});
  (INIT.agents||[]).forEach(a=>{
    const c=el('div',{className:'card'+(a.nombre===state.agente?' on':'')});
    c.append(el('div',{className:'h'}, a.nombre==='claude'?'Claude Code':'Codex'));
    c.append(el('div',{className:'d'}, 'v'+a.version+' · '+S.signed_in));
    c.addEventListener('click',()=>{ state.agente=a.nombre; if(!agent().retomar&&state.modo!=='new'){state.modo='new'; state.preset=derivePreset(state.modo,state.workspace);} render(); updateSummary(); });
    agCards.append(c);
  });
  main.append(field(S.agent, agCards));

  // Context / conversation. En el atajo «aislado» no hay conversación que
  // elegir (modo = nueva), así que la sección entera se omite. El selector de
  // modo (nueva/continuar/derivar) sólo aparece en modo avanzado; con el atajo
  // «en una conversación» el modo está fijado a «continuar».
  const showCtx = adv || state.preset==='conversation';
  if(showCtx){
    const ctxWrap=el('div',{});
    if(adv){
      const modes=[['new',S.ctx_new,S.ctx_new_d,true]];
      if(agent().retomar) modes.push(['resume',S.ctx_resume,S.ctx_resume_d,true]);
      if(agent().derivar) modes.push(['fork',S.ctx_fork,S.ctx_fork_d,true]);
      const seg=el('div',{className:'seg'});
      modes.forEach(([mo,lbl])=>{ const b=el('button',{},lbl); if(state.modo===mo)b.classList.add('on'); b.addEventListener('click',()=>{ state.modo=mo; state.sessFolders=[]; render(); updateSummary(); }); seg.append(b); }); // render() vuelve a pedir sesiones si hace falta
      ctxWrap.append(seg);
    }
    if(state.modo!=='new'){
      const picker=el('div',{className:'picker',id:'sessPicker'}, el('div',{className:'opt'}, '…'));
      const idIn=el('input',{type:'text',placeholder:S.session_ph,value:state.sesion,style:'margin-top:9px'});
      idIn.addEventListener('input',()=>{ state.sesion=idIn.value; setErr('sesion',''); scheduleResolve(); });
      ctxWrap.append(picker, idIn);
      // Carpeta detectada a partir del id de la conversación. En el atajo «en
      // una conversación» la carpeta ya se ve en el chip del repositorio, así
      // que aquí sólo mostramos la línea de una carpeta en modo avanzado; el
      // selector de varias carpetas se muestra siempre (es una decisión).
      const sf=state.sessFolders||[];
      if(sf.length===1 && adv){
        ctxWrap.append(el('div',{style:'margin-top:8px;font-size:12px;color:var(--vscode-descriptionForeground)'}, S.folder_detected+': '+sf[0].cwd));
      } else if(sf.length>1){
        const box=el('div',{style:'margin-top:8px;font-size:12px'});
        box.append(el('div',{style:'color:var(--vscode-descriptionForeground);margin-bottom:5px'}, S.folder_multi));
        const row=el('div',{className:'row'});
        sf.forEach(f=>{ const b=el('button',{className:'chip'+(state.proyecto===f.cwd?' on':''),title:f.cwd},f.name); b.addEventListener('click',()=>{ setProyecto(f.cwd,f.name,f.isGit); render(); updateSummary(); }); row.append(b); });
        box.append(row);
        ctxWrap.append(box);
      }
    }
    main.append(field(S.context, wrapErr('sesion', ctxWrap)));
    if(state.modo!=='new') requestSessions();
  }

  // Dónde trabaja: aislada (worktree + revisión) o directa (en la conversación).
  // Los dos atajos ya fijan este eje; sólo se muestra el selector en avanzado.
  // El aviso de «cambios directos» sí se conserva fuera de avanzado, porque
  // avisa de un riesgo real (escribir sin revisión) sea cual sea la vista.
  const directoCambios = ()=> state.workspace==='direct' && state.perfil==='cambios_aislados';
  if(adv){
    const wsWrap=el('div',{});
    const wsSeg=el('div',{className:'seg'});
    [['isolated',S.ws_isolated,S.ws_isolated_d],['direct',S.ws_direct,S.ws_direct_d]].forEach(([w,lbl,d])=>{
      const b=el('button',{},lbl); if(state.workspace===w)b.classList.add('on');
      b.addEventListener('click',()=>{ state.workspace=w; render(); updateSummary(); });
      wsSeg.append(b);
    });
    wsWrap.append(wsSeg);
    const wsDesc=el('div',{style:'font-size:11.5px;color:var(--vscode-descriptionForeground);margin-top:6px'}, state.workspace==='direct'?S.ws_direct_d:S.ws_isolated_d);
    wsWrap.append(wsDesc);
    if(directoCambios()){
      wsWrap.append(el('div',{style:'margin-top:7px;font-size:12px;font-weight:600;color:var(--vscode-inputValidation-errorForeground,#f48771)'}, S.ws_direct_changes+' — '+S.ws_direct_warn));
    }
    main.append(field(S.workspace, wsWrap));
  } else if(directoCambios()){
    main.append(field(S.ws_direct_changes, el('div',{style:'font-size:12px;font-weight:600;color:var(--vscode-inputValidation-errorForeground,#f48771)'}, S.ws_direct_warn)));
  }

  // Prompt
  const ta=el('textarea',{value:state.prompt}); ta.addEventListener('input',()=>{state.prompt=ta.value;});
  main.append(field(S.prompt, ta));

  // §27.4 — «Se ejecuta después de otra tarea». En avanzado, o si ya es dependiente.
  if((adv || esDependiente) && (INIT.tareas||[]).length){
    const depWrap=el('div',{});
    const sel=el('select');
    sel.append(el('option',{value:''}, S.dep_none));
    (INIT.tareas||[]).forEach(tk=>{ const o=el('option',{value:tk.id}, tk.nombre); if(tk.id===state.dependeDe)o.selected=true; sel.append(o); });
    sel.addEventListener('change',()=>{ state.dependeDe=sel.value; if(!state.dependeDe)state.dispararEn='success'; render(); updateSummary(); });
    depWrap.append(sel);
    if(esDependiente){
      const whenRow=el('div',{className:'row',style:'margin-top:9px'});
      whenRow.append(el('span',{style:'font-size:12px;color:var(--vscode-descriptionForeground)'}, S.dep_when));
      const wseg=el('div',{className:'seg'});
      [['success',S.dep_success],['failure',S.dep_failure],['always',S.dep_always]].forEach(([v,lbl])=>{ const b=el('button',{},lbl); if(state.dispararEn===v)b.classList.add('on'); b.addEventListener('click',()=>{ state.dispararEn=v; render(); updateSummary(); }); wseg.append(b); });
      whenRow.append(wseg);
      depWrap.append(whenRow);
      depWrap.append(el('div',{style:'margin-top:7px;font-size:12px;color:var(--vscode-descriptionForeground)'}, S.dep_note));
      // El encadenado por «éxito» solo dispara si el padre corre en la conversación
      // (directo). Si el padre elegido es aislado, se avisa en amarillo.
      const padreSel=(INIT.tareas||[]).find(tk=>tk.id===state.dependeDe);
      if(padreSel && !padreSel.directo && state.dispararEn!=='failure') depWrap.append(el('div',{style:'margin-top:5px;font-size:11.5px;color:var(--vscode-editorWarning-foreground,#c90)'}, S.dep_only_direct));
    }
    main.append(field(S.runs_after, depWrap));
  }

  // Schedule (una tarea dependiente no tiene horario propio: se dispara por su padre)
  if(!esDependiente){
  const schWrap=el('div',{});
  const kseg=el('div',{className:'seg'});
  [['daily',S.every_day],['weekly',S.some_days],['once',S.once]].forEach(([k,lbl])=>{ const b=el('button',{},lbl); if(state.regla===k)b.classList.add('on'); b.addEventListener('click',()=>{ state.regla=k; if(k==='once'&&state.horas.length>1)state.horas=[state.horas[0]]; render(); schedulePreview(); }); kseg.append(b); });
  schWrap.append(kseg);
  if(state.regla==='weekly'){
    const days=el('div',{className:'days',style:'margin-top:10px'});
    [[1,S.mon],[2,S.tue],[3,S.wed],[4,S.thu],[5,S.fri],[6,S.sat],[7,S.sun]].forEach(([iso,lbl])=>{ const b=el('button',{className:'day'+(state.dias.includes(iso)?' on':'')},lbl); b.addEventListener('click',()=>{ state.dias=state.dias.includes(iso)?state.dias.filter(x=>x!==iso):[...state.dias,iso].sort(); render(); schedulePreview(); }); days.append(b); });
    schWrap.append(days);
  }
  const timesRow=el('div',{className:'row',style:'margin-top:10px'});
  if(state.regla==='once'){
    const i=el('input',{type:'text',className:'time-in',style:'width:170px !important',value:state.horas[0]||'',placeholder:STR.datetime_ph});
    i.addEventListener('input',()=>{ state.horas=[i.value]; schedulePreview(); });
    timesRow.append(i);
  } else {
    state.horas.forEach((h,idx)=>{
      const chip=el('span',{className:'chip'});
      const i=el('input',{type:'text',className:'time-in',value:h,placeholder:STR.time_ph,style:'background:transparent;border:0;color:inherit'});
      i.addEventListener('input',()=>{ state.horas[idx]=i.value; schedulePreview(); });
      chip.append(i);
      if(state.horas.length>1){ const x=el('button',{},'×'); x.addEventListener('click',()=>{ state.horas.splice(idx,1); render(); schedulePreview(); }); chip.append(x); }
      timesRow.append(chip);
    });
    const add=el('button',{className:'btn ghost small'},STR.add_time); add.addEventListener('click',()=>{ state.horas.push('12:00'); render(); schedulePreview(); });
    timesRow.append(add);
  }
  schWrap.append(timesRow);
  const tz=el('input',{type:'text',value:state.zona,placeholder:'Europe/Madrid',style:'margin-top:10px;max-width:220px'});
  tz.addEventListener('input',()=>{ state.zona=tz.value; schedulePreview(); });
  schWrap.append(tz);
  schWrap.append(el('div',{className:'preview',id:'preview'}));
  main.append(field(S.schedule, wrapErr('horas', schWrap)));
  } // fin del horario (oculto para dependientes)

  // Permissions
  const permCards=el('div',{className:'cards'});
  [['auditoria',S.perm_audit,S.perm_audit_d],['cambios_aislados',S.perm_iso,S.perm_iso_d]].forEach(([p,lbl,d])=>{
    const c=el('div',{className:'card'+(state.perfil===p?' on':'')});
    c.append(el('div',{className:'h'},lbl), el('div',{className:'d'},d));
    c.addEventListener('click',()=>{ state.perfil=p; render(); updateSummary(); });
    permCards.append(c);
  });
  main.append(field(S.permissions, permCards));

  // Misfire + budget
  const mrow=el('div',{className:'row'});
  const mseg=el('div',{className:'seg'});
  [['skip',S.mis_skip],['run_if_late',S.mis_late],['manual',S.mis_wait]].forEach(([p,lbl])=>{ const b=el('button',{},lbl); if(state.politica===p)b.classList.add('on'); b.addEventListener('click',()=>{ state.politica=p; render(); }); mseg.append(b); });
  const money=el('span',{className:'money'}, '$');
  const bin=el('input',{type:'text',className:'time-in',value:state.presupuesto,placeholder:'2.00'}); bin.addEventListener('input',()=>{ state.presupuesto=bin.value; setErr('presupuesto',''); });
  money.append(bin);
  mrow.append(mseg, money);
  main.append(field(S.misfire+' · '+S.cap, wrapErr('presupuesto', mrow)));

  // Autocompactación (solo Claude): controla la ventana de --autocompact.
  if(state.agente==='claude'){
    const acSel=el('select',{style:'max-width:240px'});
    [['',S.ac_default],['auto',S.ac_auto],['200k','200k'],['300k','300k'],['500k','500k']].forEach(([v,lbl])=>{ const o=el('option',{value:v},lbl); if(state.autocompact===v)o.selected=true; acSel.append(o); });
    acSel.addEventListener('change',()=>{ state.autocompact=acSel.value; });
    const acWrap=el('div',{}); acWrap.append(acSel); acWrap.append(el('div',{style:'font-size:11.5px;color:var(--vscode-descriptionForeground);margin-top:6px'}, S.ac_hint));
    main.append(field(S.autocompact, acWrap));
  }

  // Actions
  const actions=el('div',{className:'actions'});
  if(state.editing){
    const save=el('button',{className:'btn primary'},S.save); save.addEventListener('click',()=>submit(false)); actions.append(save);
  } else {
    const create=el('button',{className:'btn ghost'},S.create); create.addEventListener('click',()=>submit(false));
    const run=el('button',{className:'btn primary'},S.create_run); run.addEventListener('click',()=>submit(true));
    actions.append(create,run);
  }
  main.append(actions);
  main.append(el('div',{className:'err',id:'err__form'}));

  // Rail summary
  rail.append(el('h4',{},S.summary));
  const box=el('div',{className:'box',id:'summaryBox'});
  rail.append(box);
  // La nota del rail describe el modo real: en directo NO hay worktree ni
  // revisión, así que la nota de aislamiento sería falsa.
  rail.append(el('div',{className:'note'}, state.workspace==='direct'?S.ws_direct_d:S.isolation_note));
  updateSummary();
}

function field(label, node){
  const f=el('div',{className:'field'});
  f.append(el('span',{className:'lbl',innerHTML:label.replace('*','<span class=req>*</span>')}));
  f.append(node);
  return f;
}
function wrapErr(key,node){ const w=el('div',{}); w.append(node); w.append(el('div',{className:'err',id:'err_'+key})); return w; }
function setErr(key,msg){ const e=document.getElementById('err_'+key)||document.getElementById('err__form'); if(e)e.textContent=msg||''; }

function requestSessions(){ if(state.agente==='claude'&&state.proyecto) vscode.postMessage({type:'listSessions',agent:state.agente,cwd:state.proyecto}); }
function setProyecto(cwd,name,isGit){ state.proyecto=cwd; state.proyectoNombre=name; state.isGit=isGit!==false; }
function scheduleResolve(){ clearTimeout(resolveTimer); resolveTimer=setTimeout(()=>{ const id=(state.sesion||'').trim(); if(id.length>=6) vscode.postMessage({type:'resolveSession',id}); },350); }
function onSessionFolders(folders){
  state.sessFolders=folders||[];
  if(folders.length===1){ setProyecto(folders[0].cwd,folders[0].name,folders[0].isGit); }
  render(); updateSummary();
}
function renderSessions(list){
  const p=document.getElementById('sessPicker'); if(!p)return; p.innerHTML='';
  if(!list.length){ p.append(el('div',{className:'opt'}, STR.session_ph)); return; }
  // Al elegir una sesión re-renderizamos: la nota del repositorio depende de
  // state.sesion («desde la conversación» vs «se detecta al elegir…»), así que
  // un ajuste manual del DOM la dejaría obsoleta.
  list.forEach(s=>{ const o=el('div',{className:'opt'+(state.sesion===s.id?' on':'')}); o.append(el('div',{},s.title||s.id)); o.append(el('div',{className:'m'}, s.id.slice(0,8)+' · '+s.when)); o.addEventListener('click',()=>{ state.sesion=s.id; state.sessFolders=[]; setErr('sesion',''); render(); updateSummary(); }); p.append(o); });
}

function schedulePreview(){ clearTimeout(previewTimer); previewTimer=setTimeout(sendPreview,350); }
function sendPreview(){
  const horas=state.horas.filter(Boolean);
  if(!horas.length||!state.zona){ showPreview({ok:false}); return; }
  vscode.postMessage({type:'preview',payload:{regla:state.regla,horas,dias:state.dias,zona:state.zona}});
}
function showPreview(m){
  const p=document.getElementById('preview'); if(!p)return;
  if(m.ok&&m.next&&m.next.length){ p.innerHTML=STR.next_run+': '+m.next.map(x=>'<b>'+x+'</b>').join(' · '); }
  else if(m.ok){ p.textContent=''; }
  else { p.textContent=''; }
  updateSummary();
}

function updateSummary(){
  const box=document.getElementById('summaryBox'); if(!box)return;
  const ag=state.agente==='claude'?'Claude Code':'Codex';
  const perm=state.perfil==='auditoria'?STR.perm_audit:STR.perm_iso;
  const when = state.regla==='once' ? (state.horas[0]||'') : (state.horas.filter(Boolean).join(', '));
  box.innerHTML='<b>'+ag+'</b> · '+perm+'<br>'+(state.proyectoNombre||'—')+'<br>'+when;
}

function submit(runNow){
  setErr('_form','');
  const esDep = !!state.dependeDe; // §27.4: dependiente → sin horario propio
  const horas=state.horas.map(h=>h.trim()).filter(Boolean);
  const okTime = state.regla==='once' ? /^\d{4}-\d{2}-\d{2}T([01]\d|2[0-3]):[0-5]\d$/.test(horas[0]||'') : horas.every(h=>/^([01]\d|2[0-3]):[0-5]\d$/.test(h));
  let bad=false;
  if(state.nombre.trim().length<3){ setErr('nombre',STR.err_name); bad=true; }
  if(!state.proyecto){ setErr('proyecto',STR.err_repo); bad=true; }
  if(!esDep && (!horas.length||!okTime)){ setErr('horas',STR.err_time); bad=true; } // dependiente: horario ignorado
  if(state.modo!=='new'&&!state.sesion.trim()){ setErr('sesion',STR.err_session); bad=true; }
  if(bad)return;
  // Presupuesto vacío = 0 = sin tope (NullFloat trata <=0 como null): así, al
  // editar, borrar el campo SÍ quita un tope existente. Se valida el formato
  // (admitiendo coma decimal) en vez de convertir en 0 una entrada inválida, que
  // borraría el tope sin avisar.
  const presuTxt = state.presupuesto.trim().replace(',','.');
  let presupuesto = 0;
  if(presuTxt!==''){
    if(!/^\d+(\.\d{1,2})?$/.test(presuTxt)){ setErr('presupuesto', STR.err_budget); bad=true; }
    else presupuesto = Number(presuTxt);
  }
  if(bad)return;
  // Una dependiente NO registra disparador, pero conserva su horario si es válido
  // (así, si luego le quitas la dependencia, recupera el original); si no lo es o
  // era «una vez», guarda un nominal daily/03:00.
  const schedOk = horas.length && okTime && state.regla!=='once';
  const reglaEnvio = esDep ? (schedOk ? state.regla : 'daily') : state.regla;
  const horasEnvio = esDep ? (schedOk ? horas : ['03:00']) : horas;
  // dependeDe/dispararEn: al EDITAR se mandan siempre (para poder quitar la
  // dependencia con cadena vacía); al crear, solo si hay dependencia.
  const enviarDep = state.editing || esDep;
  vscode.postMessage({type:'submit',payload:{
    proyecto:state.proyecto,nombre:state.nombre,agente:state.agente,prompt:state.prompt,
    regla:reglaEnvio,horas:horasEnvio,dias:reglaEnvio==='weekly'?state.dias:undefined,zona:state.zona,
    perfil:state.perfil,modo:state.modo,sesion:state.modo==='new'?undefined:state.sesion.trim(),
    // autocompact SÍ se manda vacío (no undefined) para Claude, de modo que
    // elegir "Por defecto" al editar limpie un valor anterior.
    politica:state.politica,presupuesto,autocompact:state.agente==='claude'?state.autocompact:undefined,
    workspace:state.workspace,
    dependeDe:enviarDep?state.dependeDe:undefined, dispararEn:enviarDep?state.dispararEn:undefined,
    runNow
  }});
}
`;

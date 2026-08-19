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
import { listSessions } from '../core/sessions';

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
  // A single panel at a time: reveal the existing one instead of stacking.
  if (current) {
    current.reveal(vscode.ViewColumn.Active);
    return;
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
        case 'ready':
          panel.webview.postMessage({
            type: 'init',
            editing,
            agents: installed.map((a) => ({ nombre: a.nombre, version: a.version, retomar: a.retomar, derivar: a.derivar })),
            folders,
            defaultTz: defaultTz || systemTimezone(),
            promptTemplate: PROMPT_TEMPLATE,
            task: editing ? taskToForm(existing as Task) : undefined,
          });
          break;
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
          const params = toCreateParams(p);
          if (editing) {
            const res = await ctl.edit((existing as Task).id, params);
            for (const a of res.avisos) vscode.window.showWarningMessage(t('TaskKeeper: {0}', a));
            vscode.window.showInformationMessage(t('Task updated. Next run: {0}.', res.next_run_local));
          } else {
            const created = await ctl.create(params);
            for (const a of created.avisos) vscode.window.showWarningMessage(t('TaskKeeper: {0}', a));
            if (p.runNow) {
              await ctl.runNow(created.id);
              vscode.window.showInformationMessage(t('Task created and launched. Next scheduled run: {0}. Watch the "Last night" view.', created.next_run_local));
            } else {
              vscode.window.showInformationMessage(t('Task created. Next run: {0}.', created.next_run_local));
            }
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
    retrasoMax: 7200,
    timeout: 3600,
    presupuesto: p.presupuesto,
    autocompact: p.autocompact,
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
    presupuesto: task.presupuesto_usd ?? undefined,
    autocompact: task.autocompact ?? '',
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
    agent: t('Agent'),
    signed_in: t('signed in'),
    context: t('Conversation'),
    ctx_new: t('New'),
    ctx_resume: t('Continue'),
    ctx_fork: t('Fork'),
    ctx_new_d: t('Starts fresh. Best for recurring reports and audits.'),
    ctx_resume_d: t('Appends to its history. For a one-off follow-up.'),
    ctx_fork_d: t('Copies the context into a new conversation. Best for recurring work.'),
    which_conv: t('Which conversation?'),
    session_ph: t('Or paste a session id'),
    prompt: t('Instruction (prompt)'),
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
    mis_late: t('Run if < 2h late'),
    mis_wait: t('Wait for me'),
    cap: t('Spending cap per run (USD)'),
    no_cap: t('no cap'),
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
  modo:'new',sesion:'',politica:'skip',presupuesto:'',autocompact:'',editing:false
};
let previewTimer=null;

vscode.postMessage({type:'ready'});
window.addEventListener('message',(ev)=>{
  const m=ev.data;
  if(m.type==='init'){ INIT=m; boot(m); }
  else if(m.type==='repoPicked'){ state.proyecto=m.path; state.proyectoNombre=m.name; state.isGit=m.isGit; render(); }
  else if(m.type==='sessions'){ renderSessions(m.list); }
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
  render();
  schedulePreview();
}
function normalizeTask(tk){
  const o={...tk};
  o.presupuesto = (tk.presupuesto==null?'':String(tk.presupuesto));
  o.horas = (tk.horas&&tk.horas.length)?tk.horas.slice():['03:00'];
  o.dias = (tk.dias&&tk.dias.length)?tk.dias.slice():[1];
  o.sesion = tk.sesion||'';
  return o;
}

function agent(){ return (INIT.agents||[]).find(a=>a.nombre===state.agente)||INIT.agents[0]; }

function render(){
  const S=STR;
  const app=$('#app'); app.innerHTML='';
  const main=el('div',{className:'main'});
  const rail=el('div',{className:'rail'});
  app.append(main,rail);

  // Name
  main.append(field(S.name+' *', (()=>{ const i=el('input',{type:'text',value:state.nombre,placeholder:S.name_ph}); i.addEventListener('input',()=>{state.nombre=i.value; setErr('nombre',''); updateSummary();}); return wrapErr('nombre',i); })()));

  // Repository
  const repoRow=el('div',{className:'row'});
  const repoChip=el('span',{className:'chip'}, state.proyectoNombre||'—');
  if(state.proyectoNombre && !state.isGit) repoChip.append(el('span',{className:'req'},' · '+S.not_git));
  const sel=el('select');
  (INIT.folders||[]).forEach(f=>{ const o=el('option',{value:f.path},f.name); if(f.path===state.proyecto)o.selected=true; sel.append(o); });
  if((INIT.folders||[]).length){ sel.addEventListener('change',()=>{ const f=INIT.folders.find(x=>x.path===sel.value); state.proyecto=f.path; state.proyectoNombre=f.name; state.isGit=true; render(); updateSummary(); }); repoRow.append(sel); }
  const browse=el('button',{className:'btn ghost small'},S.browse); browse.addEventListener('click',()=>vscode.postMessage({type:'browseRepo'}));
  repoRow.append(browse, repoChip);
  main.append(field(S.repo, wrapErr('proyecto', repoRow)));

  // Agent
  const agCards=el('div',{className:'cards'});
  (INIT.agents||[]).forEach(a=>{
    const c=el('div',{className:'card'+(a.nombre===state.agente?' on':'')});
    c.append(el('div',{className:'h'}, a.nombre==='claude'?'Claude Code':'Codex'));
    c.append(el('div',{className:'d'}, 'v'+a.version+' · '+S.signed_in));
    c.addEventListener('click',()=>{ state.agente=a.nombre; if(!agent().retomar&&state.modo!=='new')state.modo='new'; render(); updateSummary(); });
    agCards.append(c);
  });
  main.append(field(S.agent, agCards));

  // Context / conversation
  const modes=[['new',S.ctx_new,S.ctx_new_d,true]];
  if(agent().retomar) modes.push(['resume',S.ctx_resume,S.ctx_resume_d,true]);
  if(agent().derivar) modes.push(['fork',S.ctx_fork,S.ctx_fork_d,true]);
  const seg=el('div',{className:'seg'});
  modes.forEach(([mo,lbl])=>{ const b=el('button',{},lbl); if(state.modo===mo)b.classList.add('on'); b.addEventListener('click',()=>{ state.modo=mo; render(); updateSummary(); if(mo!=='new')requestSessions(); }); seg.append(b); });
  const ctxWrap=el('div',{}, seg);
  if(state.modo!=='new'){
    const picker=el('div',{className:'picker',id:'sessPicker'}, el('div',{className:'opt'}, '…'));
    const idIn=el('input',{type:'text',placeholder:S.session_ph,value:state.sesion,style:'margin-top:9px'});
    idIn.addEventListener('input',()=>{ state.sesion=idIn.value; setErr('sesion',''); });
    ctxWrap.append(picker, idIn);
  }
  main.append(field(S.context, wrapErr('sesion', ctxWrap)));
  if(state.modo!=='new') requestSessions();

  // Prompt
  const ta=el('textarea',{value:state.prompt}); ta.addEventListener('input',()=>{state.prompt=ta.value;});
  main.append(field(S.prompt, ta));

  // Schedule
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
  const bin=el('input',{type:'text',className:'time-in',value:state.presupuesto,placeholder:'2.00'}); bin.addEventListener('input',()=>{ state.presupuesto=bin.value; });
  money.append(bin);
  mrow.append(mseg, money);
  main.append(field(S.misfire+' · '+S.cap, mrow));

  // Autocompactación (solo Claude): controla la ventana de --autocompact.
  if(state.agente==='claude'){
    const acSel=el('select',{style:'max-width:240px'});
    [['',S.ac_default],['auto',S.ac_auto],['200k','200k'],['300k','300k'],['500k','500k']].forEach(([v,lbl])=>{ const o=el('option',{value:v},lbl); if(state.autocompact===v)o.selected=true; acSel.append(o); });
    acSel.addEventListener('change',()=>{ state.autocompact=acSel.value; });
    const acWrap=el('div',{}); acWrap.append(acSel); acWrap.append(el('div',{className:'rail-note',style:'font-size:11.5px;color:var(--vscode-descriptionForeground);margin-top:6px'}, S.ac_hint));
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
  rail.append(el('div',{className:'note'}, '◆ '+S.isolation_note));
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
function renderSessions(list){
  const p=document.getElementById('sessPicker'); if(!p)return; p.innerHTML='';
  if(!list.length){ p.append(el('div',{className:'opt'}, STR.session_ph)); return; }
  list.forEach(s=>{ const o=el('div',{className:'opt'+(state.sesion===s.id?' on':'')}); o.append(el('div',{},s.title||s.id)); o.append(el('div',{className:'m'}, s.id.slice(0,8)+' · '+s.when)); o.addEventListener('click',()=>{ state.sesion=s.id; const inp=p.parentElement.querySelector('input'); if(inp)inp.value=s.id; [...p.children].forEach(c=>c.classList.remove('on')); o.classList.add('on'); setErr('sesion',''); }); p.append(o); });
}

function schedulePreview(){ clearTimeout(previewTimer); previewTimer=setTimeout(sendPreview,350); }
function sendPreview(){
  const horas=state.horas.filter(Boolean);
  if(!horas.length||!state.zona){ showPreview({ok:false}); return; }
  vscode.postMessage({type:'preview',payload:{regla:state.regla,horas,dias:state.dias,zona:state.zona}});
}
function showPreview(m){
  const p=document.getElementById('preview'); if(!p)return;
  if(m.ok&&m.next&&m.next.length){ p.innerHTML='🌙 '+STR.next_run+': '+m.next.map(x=>'<b>'+x+'</b>').join(' · '); }
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
  const horas=state.horas.map(h=>h.trim()).filter(Boolean);
  const okTime = state.regla==='once' ? /^\d{4}-\d{2}-\d{2}T([01]\d|2[0-3]):[0-5]\d$/.test(horas[0]||'') : horas.every(h=>/^([01]\d|2[0-3]):[0-5]\d$/.test(h));
  let bad=false;
  if(state.nombre.trim().length<3){ setErr('nombre',STR.err_name); bad=true; }
  if(!state.proyecto){ setErr('proyecto',STR.err_repo); bad=true; }
  if(!horas.length||!okTime){ setErr('horas',STR.err_time); bad=true; }
  if(state.modo!=='new'&&!state.sesion.trim()){ setErr('sesion',STR.err_session); bad=true; }
  if(bad)return;
  const presupuesto = state.presupuesto.trim()===''?undefined:Number(state.presupuesto);
  vscode.postMessage({type:'submit',payload:{
    proyecto:state.proyecto,nombre:state.nombre,agente:state.agente,prompt:state.prompt,
    regla:state.regla,horas,dias:state.regla==='weekly'?state.dias:undefined,zona:state.zona,
    perfil:state.perfil,modo:state.modo,sesion:state.modo==='new'?undefined:state.sesion.trim(),
    politica:state.politica,presupuesto,autocompact:state.agente==='claude'?(state.autocompact||undefined):undefined,runNow
  }});
}
`;

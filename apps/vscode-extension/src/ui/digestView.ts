// §27.1 — «Resumen de anoche» + salud del planificador, en un Webview.
// Solo muestra: agrega en Go (ctl `resumen`) y aquí pinta. Sin emojis (SVG
// propio). Se abre a demanda (taskkeeper.showDigest) y, una vez al día, solo si
// hubo actividad, sin robar el foco (preserveFocus).
import * as vscode from 'vscode';
import type { Ctl } from '../core/ctl';

const t = vscode.l10n.t;

let current: vscode.WebviewPanel | undefined;

export async function openDigestView(ctl: Ctl, opts?: { beside?: boolean; preserveFocus?: boolean }): Promise<void> {
  if (current) {
    current.reveal(opts?.beside ? vscode.ViewColumn.Beside : vscode.ViewColumn.Active, opts?.preserveFocus);
    await push(ctl, current);
    return;
  }
  const panel = vscode.window.createWebviewPanel(
    'taskkeeper.digest',
    t('Last night'),
    { viewColumn: opts?.beside ? vscode.ViewColumn.Beside : vscode.ViewColumn.Active, preserveFocus: opts?.preserveFocus ?? false },
    { enableScripts: true, retainContextWhenHidden: true },
  );
  current = panel;
  panel.onDidDispose(() => {
    if (current === panel) current = undefined;
  });
  const nonce = makeNonce();
  panel.webview.html = renderHtml(panel.webview, nonce, strings());
  panel.webview.onDidReceiveMessage(async (msg: { type: string }) => {
    try {
      if (msg.type === 'ready' || msg.type === 'refresh') await push(ctl, panel);
    } catch (e) {
      panel.webview.postMessage({ type: 'error', message: (e as Error).message });
    }
  });
}

/** Called by the marker watcher so an open digest updates live. */
export function refreshOpenDigest(ctl: Ctl): void {
  if (current) void push(ctl, current);
}

async function push(ctl: Ctl, panel: vscode.WebviewPanel): Promise<void> {
  const d = await ctl.digest();
  panel.webview.postMessage({ type: 'render', ...d });
}

function strings(): Record<string, string> {
  return {
    title: t('Last night'),
    refresh: t('Refresh'),
    night: t('Overnight'),
    health: t('Scheduler health'),
    done: t('Done'),
    waiting: t('Awaiting review'),
    failed: t('Failed'),
    skipped: t('Skipped'),
    running: t('In progress'),
    cost: t('Cost'),
    quiet: t('Nothing ran in this window.'),
    ok: t('Registered'),
    no_trigger: t('Trigger missing — re-save the task'),
    next: t('Next'),
    missed: t('missed'),
    pending: t('pending'),
    healthy: t('All triggers registered.'),
    no_tasks: t('No scheduled tasks.'),
  };
}

function renderHtml(webview: vscode.Webview, nonce: string, s: Record<string, string>): string {
  const csp = [`default-src 'none'`, `style-src ${webview.cspSource} 'unsafe-inline'`, `script-src 'nonce-${nonce}'`].join('; ');
  return `<!DOCTYPE html>
<html lang="${vscode.env.language}">
<head>
<meta charset="UTF-8">
<meta http-equiv="Content-Security-Policy" content="${csp}">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style nonce="${nonce}">${CSS}</style>
</head>
<body>
<div id="app"><div class="empty">…</div></div>
<script nonce="${nonce}">const STR=${JSON.stringify(s)};</script>
<script nonce="${nonce}">${SCRIPT}</script>
</body>
</html>`;
}

const CSS = `
*{box-sizing:border-box}
body{font-family:var(--vscode-font-family);font-size:13px;color:var(--vscode-foreground);background:var(--vscode-editor-background);margin:0}
#app{max-width:760px;margin:0 auto;padding:22px 24px 60px}
.empty{color:var(--vscode-descriptionForeground);padding:30px 0;text-align:center}
.errmsg{color:var(--vscode-inputValidation-errorForeground,#f48771);border:1px solid var(--vscode-inputValidation-errorBorder,#be1100);border-radius:6px;padding:12px 14px;margin:10px 0}
.head{display:flex;align-items:center;justify-content:space-between;margin-bottom:16px}
.head h1{font-size:1.2rem;font-weight:600;margin:0}
.btn{border:0;border-radius:5px;padding:6px 13px;font-size:12.5px;font-weight:600;cursor:pointer;font-family:inherit;background:var(--vscode-button-secondaryBackground);color:var(--vscode-button-secondaryForeground);display:inline-flex;gap:6px;align-items:center}
.sec{margin:22px 0 10px;font-size:11px;text-transform:uppercase;letter-spacing:.06em;color:var(--vscode-descriptionForeground);font-weight:600}
.tiles{display:grid;grid-template-columns:repeat(auto-fit,minmax(110px,1fr));gap:10px}
.tile{border:1px solid var(--vscode-panel-border);border-radius:9px;padding:12px 14px;background:var(--vscode-editorWidget-background)}
.tile .n{font-size:1.7rem;font-weight:700;font-variant-numeric:tabular-nums;line-height:1;display:flex;align-items:center;gap:7px}
.tile .l{font-size:11.5px;color:var(--vscode-descriptionForeground);margin-top:5px}
.tile .ic{width:15px;height:15px;flex:none}
.tile.done .n{color:var(--vscode-testing-iconPassed,#3fbd88)}
.tile.wait .n{color:var(--vscode-charts-orange,#d99a3a)}
.tile.fail .n{color:var(--vscode-testing-iconFailed,#e06a6a)}
.tile.zero .n{color:var(--vscode-descriptionForeground)}
.rows{display:flex;flex-direction:column;border:1px solid var(--vscode-panel-border);border-radius:9px;overflow:hidden}
.row{display:flex;align-items:center;gap:12px;padding:11px 14px;border-top:1px solid var(--vscode-panel-border)}
.row:first-child{border-top:0}
.row .name{font-weight:600;flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.row.bad{background:var(--vscode-inputValidation-errorBackground,rgba(224,106,106,.08))}
.chip{display:inline-flex;align-items:center;gap:5px;font-size:11px;font-weight:600;padding:2px 9px;border-radius:999px;background:var(--vscode-badge-background);color:var(--vscode-badge-foreground)}
.chip.ok{background:var(--vscode-testing-iconPassed,#3fbd88);color:#08130d}
.chip.err{background:var(--vscode-testing-iconFailed,#e06a6a);color:#150808}
.badge{font-size:11px;color:var(--vscode-descriptionForeground);font-variant-numeric:tabular-nums}
.badge.warn{color:var(--vscode-charts-orange,#d99a3a);font-weight:600}
.next{font-size:12px;color:var(--vscode-descriptionForeground);font-variant-numeric:tabular-nums}
`;

const SCRIPT = String.raw`
const vscode = acquireVsCodeApi();
const el=(t,p={},...k)=>{const e=document.createElement(t);Object.assign(e,p);for(const c of k)if(c!=null)e.append(c);return e;};
vscode.postMessage({type:'ready'});
window.addEventListener('message',(e)=>{ const m=e.data; if(m.type==='render') render(m); else if(m.type==='error') showError(m.message); });

// Iconos SVG inline (sin emoji).
const SVG={
  check:'<svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M3 8.5l3.2 3L13 4.5"/></svg>',
  clock:'<svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="8" cy="8" r="6"/><path d="M8 4.5V8l2.5 1.5"/></svg>',
  x:'<svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 4l8 8M12 4l-8 8"/></svg>',
  skip:'<svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M4 4l5 4-5 4zM11 4v8"/></svg>',
  run:'<svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="8" cy="8" r="6"/><path d="M8 4.5V8l2.5 1.5"/></svg>',
  coin:'<svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.4"><circle cx="8" cy="8" r="6"/><path d="M8 5v6M6.3 6.4h2.4a1.3 1.3 0 010 2.6H6.6"/></svg>',
  refresh:'<svg viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M13 8a5 5 0 11-1.5-3.5M13 3v2.5h-2.5"/></svg>',
};

function tile(cls,icon,n,label){
  const z = n===0 ? ' zero' : '';
  return el('div',{className:'tile '+cls+z},
    el('div',{className:'n'}, el('span',{className:'ic',innerHTML:icon}), String(n)),
    el('div',{className:'l'}, label));
}

function render(m){
  const app=document.getElementById('app'); app.innerHTML='';
  const r=m.resumen||{}, salud=m.salud||[];

  const head=el('div',{className:'head'}, el('h1',{},STR.title));
  const btn=el('button',{className:'btn',title:STR.refresh}); btn.innerHTML=SVG.refresh+' '+STR.refresh;
  btn.addEventListener('click',()=>vscode.postMessage({type:'refresh'}));
  head.append(btn); app.append(head);

  // Anoche
  app.append(el('div',{className:'sec'},STR.night));
  const tiles=el('div',{className:'tiles'});
  tiles.append(
    tile('done',SVG.check, r.terminadas||0, STR.done),
    tile('wait',SVG.clock, r.esperan_revision||0, STR.waiting),
    tile('fail',SVG.x,     r.fallidas||0, STR.failed),
    tile('',SVG.skip,      r.saltadas||0, STR.skipped),
    tile('',SVG.run,       r.en_curso||0, STR.running),
  );
  const coste = (r.coste_total_usd||0);
  const ctile = el('div',{className:'tile'+(coste?'':' zero')},
    el('div',{className:'n'}, el('span',{className:'ic',innerHTML:SVG.coin}), coste.toFixed(2)+' USD'),
    el('div',{className:'l'}, STR.cost));
  tiles.append(ctile);
  app.append(tiles);

  const activity = (r.terminadas||0)+(r.esperan_revision||0)+(r.fallidas||0)+(r.saltadas||0)+(r.en_curso||0);
  if(!activity){ app.append(el('div',{className:'empty'},STR.quiet)); }

  // Salud
  app.append(el('div',{className:'sec'},STR.health));
  if(!salud.length){ app.append(el('div',{className:'empty'},STR.no_tasks)); return; }
  const rows=el('div',{className:'rows'});
  let anyBad=false;
  salud.forEach(s=>{
    const bad = !s.registrada || s.disparos_perdidos>0 || s.pendientes>0;
    if(bad) anyBad=true;
    const row=el('div',{className:'row'+(bad?' bad':'')});
    row.append(el('div',{className:'name',title:s.tarea}, s.tarea));
    if(s.registrada){
      row.append(el('span',{className:'chip ok'}, STR.ok));
      if(s.proxima_local) row.append(el('span',{className:'next'}, STR.next+': '+s.proxima_local));
    } else {
      const chip=el('span',{className:'chip err'}); chip.innerHTML=SVG.x+' '+STR.no_trigger; row.append(chip);
    }
    if(s.disparos_perdidos>0) row.append(el('span',{className:'badge warn'}, s.disparos_perdidos+' '+STR.missed));
    if(s.pendientes>0) row.append(el('span',{className:'badge warn'}, s.pendientes+' '+STR.pending));
    rows.append(row);
  });
  app.append(rows);
  if(!anyBad) app.append(el('div',{className:'empty',style:'padding:12px 0'},STR.healthy));
}

function showError(msg){
  const app=document.getElementById('app'); app.innerHTML='';
  app.append(el('div',{className:'errmsg'}, msg||'error'));
}
`;

function makeNonce(): string {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  let s = '';
  for (let i = 0; i < 32; i++) s += chars[Math.floor(Math.random() * chars.length)];
  return s;
}

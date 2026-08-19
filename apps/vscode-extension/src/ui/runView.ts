// The run result view: a readable transcript with a summary header, in a
// Webview. It builds the transcript with the pure buildTranscript (unit-tested)
// and only renders it here. It refreshes live while the run is in progress and
// offers the same accept/reject/archive/diff actions as the tree.
import * as vscode from 'vscode';
import type { Ctl } from '../core/ctl';
import type { Run } from '../core/model';
import { buildTranscript, type Transcript } from '../core/transcript';

const t = vscode.l10n.t;

let current: { panel: vscode.WebviewPanel; runId: string } | undefined;

export async function openRunView(ctl: Ctl, runId: string): Promise<void> {
  if (current) {
    current.panel.dispose();
    current = undefined;
  }
  const panel = vscode.window.createWebviewPanel('taskkeeper.runView', t('Run result'), vscode.ViewColumn.Active, {
    enableScripts: true,
    retainContextWhenHidden: true,
  });
  current = { panel, runId };
  panel.onDidDispose(() => {
    if (current?.panel === panel) current = undefined;
  });

  const nonce = makeNonce();
  panel.webview.html = renderHtml(panel.webview, nonce, strings());

  panel.webview.onDidReceiveMessage(async (msg: { type: string }) => {
    try {
      switch (msg.type) {
        case 'ready':
        case 'refresh':
          await push(ctl, panel, runId);
          break;
        case 'accept':
          await ctl.accept(runId);
          await after(ctl, panel, runId);
          break;
        case 'reject':
          await ctl.reject(runId);
          await after(ctl, panel, runId);
          break;
        case 'archive':
          await ctl.archive(runId);
          await after(ctl, panel, runId);
          break;
        case 'diff':
          await vscode.commands.executeCommand('taskkeeper.openDiff', runId);
          break;
      }
    } catch (e) {
      panel.webview.postMessage({ type: 'error', message: (e as Error).message });
    }
  });
}

/** Called by the marker watcher so an open result view updates live. */
export function refreshOpenRunView(ctl: Ctl): void {
  if (current) void push(ctl, current.panel, current.runId);
}

async function after(ctl: Ctl, panel: vscode.WebviewPanel, runId: string): Promise<void> {
  await vscode.commands.executeCommand('taskkeeper.refresh');
  await push(ctl, panel, runId);
}

async function push(ctl: Ctl, panel: vscode.WebviewPanel, runId: string): Promise<void> {
  let run: Run;
  try {
    run = await ctl.run(runId);
  } catch (e) {
    panel.webview.postMessage({ type: 'error', message: (e as Error).message });
    return;
  }
  const events = await ctl.events(runId).catch(() => []);
  const transcript: Transcript = buildTranscript(run, events);
  panel.title = `${t('Run result')} · ${run.tarea}`;
  panel.webview.postMessage({ type: 'render', name: run.tarea, transcript });
}

function makeNonce(): string {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  let s = '';
  for (let i = 0; i < 32; i++) s += chars[Math.floor(Math.random() * chars.length)];
  return s;
}

function strings(): Record<string, string> {
  return {
    accept: t('Accept'),
    reject: t('Reject'),
    archive: t('Archive'),
    diff: t('Open diff'),
    refresh: t('Refresh'),
    files: t('files changed'),
    turns: t('turns'),
    session: t('conversation'),
    isolated: t('isolated worktree'),
    direct: t('in the repository'),
    ran: t('Ran'),
    result: t('Result'),
    thought: t('reasoning'),
    empty: t('No activity recorded yet.'),
    state_running: t('Running'),
    state_awaiting_review: t('Awaiting review'),
    state_accepted: t('Accepted'),
    state_rejected: t('Rejected'),
    state_completed: t('Done'),
    state_failed: t('Failed'),
    state_failed_auth: t('Sign-in failed'),
    state_failed_quota: t('Quota exhausted'),
    state_cancelled: t('Cancelled'),
    state_skipped: t('Skipped'),
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
#app{max-width:820px;margin:0 auto;padding:20px 24px 60px}
.empty{color:var(--vscode-descriptionForeground);padding:30px 0;text-align:center}
.head{position:sticky;top:0;background:var(--vscode-editor-background);padding-bottom:12px;border-bottom:1px solid var(--vscode-panel-border);margin-bottom:16px;z-index:1}
.title{font-size:1.15rem;font-weight:600;margin:0 0 8px}
.meta{display:flex;flex-wrap:wrap;gap:8px 14px;align-items:center;font-size:12px;color:var(--vscode-descriptionForeground)}
.pill{display:inline-flex;align-items:center;gap:5px;font-weight:600;font-size:11.5px;padding:2px 10px;border-radius:999px;background:var(--vscode-badge-background);color:var(--vscode-badge-foreground)}
.pill.ok{background:var(--vscode-testing-iconPassed,#3fbd88);color:#08130d}
.pill.err{background:var(--vscode-testing-iconFailed,#e06a6a);color:#150808}
.pill.run{background:var(--vscode-progressBar-background,#4b50e0);color:#fff}
.num{font-variant-numeric:tabular-nums}
.actions{display:flex;gap:8px;margin-top:12px;flex-wrap:wrap}
.btn{border:0;border-radius:5px;padding:6px 13px;font-size:12.5px;font-weight:600;cursor:pointer;font-family:inherit}
.btn.primary{background:var(--vscode-button-background);color:var(--vscode-button-foreground)}
.btn.ghost{background:var(--vscode-button-secondaryBackground);color:var(--vscode-button-secondaryForeground)}
.ev{display:flex;gap:11px;padding:8px 0}
.ic{flex:none;width:24px;height:24px;border-radius:6px;display:grid;place-items:center;font-size:12px;margin-top:1px}
.ev.say .ic{background:var(--vscode-textBlockQuote-background);color:var(--vscode-textLink-foreground)}
.ev.tool .ic,.ev.thought .ic{background:var(--vscode-editorWidget-background);color:var(--vscode-descriptionForeground)}
.ev.system .ic,.ev.notice .ic,.ev.log .ic{background:transparent;color:var(--vscode-descriptionForeground)}
.ev.final .ic{background:var(--vscode-testing-iconPassed,#3fbd88);color:#08130d}
.ev.final.err .ic{background:var(--vscode-testing-iconFailed,#e06a6a);color:#150808}
.who{font-size:10.5px;text-transform:uppercase;letter-spacing:.05em;color:var(--vscode-descriptionForeground);margin-bottom:2px}
.txt{white-space:pre-wrap;word-wrap:break-word}
.ev.final .txt{font-weight:500}
.ev.system .txt,.ev.notice .txt,.ev.log .txt{color:var(--vscode-descriptionForeground);font-size:12px}
.cmd{font-family:var(--vscode-editor-font-family,monospace);font-size:11.5px;background:var(--vscode-textCodeBlock-background,var(--vscode-editorWidget-background));border:1px solid var(--vscode-panel-border);border-radius:5px;padding:5px 8px;margin-top:2px;white-space:pre-wrap;word-break:break-word}
details.out{margin-top:4px}
details.out summary{cursor:pointer;font-size:11px;color:var(--vscode-descriptionForeground)}
details.out pre{margin:5px 0 0;font-family:var(--vscode-editor-font-family,monospace);font-size:11px;color:var(--vscode-descriptionForeground);white-space:pre-wrap;word-break:break-word;padding-left:10px;border-left:2px solid var(--vscode-panel-border)}
`;

const SCRIPT = String.raw`
const vscode = acquireVsCodeApi();
const el=(t,p={},...k)=>{const e=document.createElement(t);Object.assign(e,p);for(const c of k)if(c!=null)e.append(c);return e;};
vscode.postMessage({type:'ready'});
window.addEventListener('message',(e)=>{ const m=e.data; if(m.type==='render') render(m); else if(m.type==='error') showError(m.message); });

// Inline SVG icons (no emoji, per the project's icon convention).
const SVG={
  say:'<svg viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.4"><path d="M2.5 3.5h11v6H6.5l-3 2.5V9.5H2.5z"/></svg>',
  tool:'<svg viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M3 4l3 4-3 4M8.5 12H13"/></svg>',
  thought:'<svg viewBox="0 0 16 16" width="13" height="13" fill="currentColor"><circle cx="4" cy="8" r="1.1"/><circle cx="8" cy="8" r="1.1"/><circle cx="12" cy="8" r="1.1"/></svg>',
  final:'<svg viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.7"><path d="M3 8.5l3.2 3L13 4.5"/></svg>',
  dot:'<svg viewBox="0 0 16 16" width="9" height="9" fill="currentColor"><circle cx="8" cy="8" r="3"/></svg>',
};
const ICON={say:SVG.say,tool:SVG.tool,thought:SVG.thought,final:SVG.final,system:SVG.dot,notice:SVG.dot,log:SVG.dot};

function stateInfo(st){
  const key='state_'+st;
  const label=STR[key]||st;
  let cls='pill'; if(['completed','accepted'].includes(st))cls='pill ok';
  else if(String(st).startsWith('failed')||st==='rejected'||st==='cancelled')cls='pill err';
  else if(['running','preflight','queued','verifying'].includes(st))cls='pill run';
  return {label,cls};
}

function render(m){
  const app=document.getElementById('app'); app.innerHTML='';
  const tr=m.transcript, sm=tr.summary;
  const head=el('div',{className:'head'});
  head.append(el('div',{className:'title'},m.name||''));
  const si=stateInfo(sm.state);
  const meta=el('div',{className:'meta'});
  meta.append(el('span',{className:si.cls},si.label));
  if(sm.costUSD!=null) meta.append(el('span',{className:'num'},'$'+Number(sm.costUSD).toFixed(2)));
  if(sm.turns!=null) meta.append(el('span',{className:'num'},sm.turns+' '+STR.turns));
  if(sm.files>0) meta.append(el('span',{className:'num'},sm.files+' '+STR.files));
  if(sm.isolated===true) meta.append(el('span',{},STR.isolated));
  else if(sm.isolated===false) meta.append(el('span',{},STR.direct));
  if(sm.errorCode) meta.append(el('span',{},'· '+sm.errorCode));
  head.append(meta);

  const actions=el('div',{className:'actions'});
  if(sm.state==='awaiting_review'){
    if(sm.files>0){ const d=el('button',{className:'btn ghost'},STR.diff); d.addEventListener('click',()=>vscode.postMessage({type:'diff'})); actions.append(d); }
    const a=el('button',{className:'btn primary'},STR.accept); a.addEventListener('click',()=>vscode.postMessage({type:'accept'}));
    const r=el('button',{className:'btn ghost'},STR.reject); r.addEventListener('click',()=>vscode.postMessage({type:'reject'}));
    actions.append(a,r);
    if(sm.files===0){ const ar=el('button',{className:'btn ghost'},STR.archive); ar.addEventListener('click',()=>vscode.postMessage({type:'archive'})); actions.append(ar); }
  } else if(String(sm.state).startsWith('failed')){
    const ar=el('button',{className:'btn ghost'},STR.archive); ar.addEventListener('click',()=>vscode.postMessage({type:'archive'})); actions.append(ar);
  }
  const rf=el('button',{className:'btn ghost'},STR.refresh); rf.addEventListener('click',()=>vscode.postMessage({type:'refresh'})); actions.append(rf);
  head.append(actions);
  app.append(head);

  if(!tr.items.length){ app.append(el('div',{className:'empty'},STR.empty)); return; }
  for(const it of tr.items){
    const row=el('div',{className:'ev '+it.kind+(it.error?' err':'')});
    row.append(el('div',{className:'ic',innerHTML:ICON[it.kind]||SVG.dot}));
    const body=el('div',{});
    if(it.kind==='say'||it.kind==='final'||it.kind==='thought'){
      const who = it.kind==='final'?STR.result : it.kind==='thought'?STR.thought : (it.who||'');
      if(who) body.append(el('div',{className:'who'},who));
      body.append(el('div',{className:'txt'},it.text||''));
    } else if(it.kind==='tool'){
      body.append(el('div',{className:'who'},STR.ran+' · '+(it.tool||'')));
      if(it.command) body.append(el('div',{className:'cmd'},it.command));
      if(it.output){ const d=el('details',{className:'out'}); d.append(el('summary',{},'output')); d.append(el('pre',{},it.output)); body.append(d); }
    } else {
      body.append(el('div',{className:'txt'},it.text||''));
    }
    row.append(body);
    app.append(row);
  }
}

function showError(msg){ const app=document.getElementById('app'); app.append(el('div',{className:'empty'},msg)); }
`;

// §27.2 — Gasto + tope mensual, en un Webview. Muestra el gasto informado del
// mes frente al tope, el desglose por día (barras SVG propias) y por tarea, y deja
// fijar el tope. Sin emojis. Aviso: el coste solo lo informa Claude; Codex no.
import * as vscode from 'vscode';
import type { Ctl } from '../core/ctl';

const t = vscode.l10n.t;

let current: vscode.WebviewPanel | undefined;

export async function openSpendView(ctl: Ctl): Promise<void> {
  if (current) {
    current.reveal(vscode.ViewColumn.Active);
    await push(ctl, current);
    return;
  }
  const panel = vscode.window.createWebviewPanel('taskkeeper.spend', t('Spending'), vscode.ViewColumn.Active, {
    enableScripts: true,
    retainContextWhenHidden: true,
  });
  current = panel;
  panel.onDidDispose(() => {
    if (current === panel) current = undefined;
  });
  const nonce = makeNonce();
  panel.webview.html = renderHtml(panel.webview, nonce, strings());
  panel.webview.onDidReceiveMessage(async (msg: { type: string; usd?: number }) => {
    try {
      if (msg.type === 'ready' || msg.type === 'refresh') {
        await push(ctl, panel);
      } else if (msg.type === 'setCap') {
        const usd = Number(msg.usd);
        if (!Number.isFinite(usd) || usd < 0) throw new Error(t('Enter an amount like 20, or 0 for no cap.'));
        await ctl.setMonthlyCap(usd);
        await push(ctl, panel);
      }
    } catch (e) {
      panel.webview.postMessage({ type: 'error', message: (e as Error).message });
    }
  });
}

/** Called by the marker watcher so an open spend view updates live. */
export function refreshOpenSpend(ctl: Ctl): void {
  if (current) void push(ctl, current).catch(() => { /* refresco best-effort */ });
}

async function push(ctl: Ctl, panel: vscode.WebviewPanel): Promise<void> {
  const s = await ctl.spend();
  panel.webview.postMessage({ type: 'render', ...s });
}

function strings(): Record<string, string> {
  return {
    title: t('Spending'),
    refresh: t('Refresh'),
    this_month: t('This month'),
    cap: t('Monthly cap'),
    no_cap: t('no cap'),
    save: t('Save'),
    over: t('over the cap'),
    near: t('near the cap'),
    codex: t('Only reported cost counts — Codex does not report it, so its runs are not included and its cap has no effect.'),
    per_day: t('By day (14 days)'),
    per_task: t('By task (30 days)'),
    runs: t('runs'),
    no_spend: t('No spend recorded yet.'),
    cap_ph: t('e.g. 20 (0 = no cap)'),
    bad_cap: t('Enter an amount like 20, or 0 for no cap.'),
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
#app{max-width:720px;margin:0 auto;padding:22px 24px 60px}
.empty{color:var(--vscode-descriptionForeground);padding:20px 0;text-align:center}
.errmsg{color:var(--vscode-inputValidation-errorForeground,#f48771);border:1px solid var(--vscode-inputValidation-errorBorder,#be1100);border-radius:6px;padding:12px 14px;margin:10px 0}
.head{display:flex;align-items:center;justify-content:space-between;margin-bottom:18px}
.head h1{font-size:1.2rem;font-weight:600;margin:0}
.btn{border:0;border-radius:5px;padding:6px 13px;font-size:12.5px;font-weight:600;cursor:pointer;font-family:inherit;background:var(--vscode-button-secondaryBackground);color:var(--vscode-button-secondaryForeground);display:inline-flex;gap:6px;align-items:center}
.btn.primary{background:var(--vscode-button-background);color:var(--vscode-button-foreground)}
.sec{margin:22px 0 10px;font-size:11px;text-transform:uppercase;letter-spacing:.06em;color:var(--vscode-descriptionForeground);font-weight:600}
.month{display:flex;align-items:baseline;gap:10px}
.month .big{font-size:2.2rem;font-weight:700;font-variant-numeric:tabular-nums;line-height:1}
.month .cap{color:var(--vscode-descriptionForeground);font-size:13px}
.bar{height:10px;border-radius:999px;background:var(--vscode-editorWidget-background);border:1px solid var(--vscode-panel-border);overflow:hidden;margin-top:10px}
.bar > i{display:block;height:100%;background:var(--vscode-progressBar-background,#4b50e0)}
.bar.warn > i{background:var(--vscode-charts-orange,#d99a3a)}
.bar.over > i{background:var(--vscode-testing-iconFailed,#e06a6a)}
.capform{display:flex;gap:8px;align-items:center;margin-top:12px}
.capform label{color:var(--vscode-descriptionForeground);font-size:12px}
.capform input{width:120px;font-family:inherit;font-size:13px;color:var(--vscode-input-foreground);background:var(--vscode-input-background);border:1px solid var(--vscode-input-border,transparent);border-radius:4px;padding:5px 8px}
.note{margin-top:14px;font-size:12px;color:var(--vscode-descriptionForeground);border-left:2px solid var(--vscode-panel-border);padding-left:10px;line-height:1.5}
.days{display:flex;align-items:flex-end;gap:5px;height:110px;padding-top:6px}
.day{flex:1;display:flex;flex-direction:column;align-items:center;gap:5px;min-width:0}
.day .col{width:100%;max-width:26px;background:var(--vscode-progressBar-background,#4b50e0);border-radius:3px 3px 0 0;min-height:2px}
.day .lbl{font-size:9.5px;color:var(--vscode-descriptionForeground);font-variant-numeric:tabular-nums;white-space:nowrap}
.rows{display:flex;flex-direction:column;border:1px solid var(--vscode-panel-border);border-radius:9px;overflow:hidden}
.row{display:flex;align-items:center;gap:12px;padding:10px 14px;border-top:1px solid var(--vscode-panel-border)}
.row:first-child{border-top:0}
.row .name{font-weight:600;flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.row .n{color:var(--vscode-descriptionForeground);font-size:12px;font-variant-numeric:tabular-nums}
.row .usd{font-variant-numeric:tabular-nums;font-weight:600;min-width:80px;text-align:right}
`;

const SCRIPT = String.raw`
const vscode = acquireVsCodeApi();
const el=(t,p={},...k)=>{const e=document.createElement(t);Object.assign(e,p);for(const c of k)if(c!=null)e.append(c);return e;};
vscode.postMessage({type:'ready'});
window.addEventListener('message',(e)=>{ const m=e.data; if(m.type==='render') render(m); else if(m.type==='error') showError(m.message); });

const SVG_R='<svg viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M13 8a5 5 0 11-1.5-3.5M13 3v2.5h-2.5"/></svg>';

function money(v){ return (v||0).toFixed(2)+' USD'; }
function shortDay(iso){ const p=(iso||'').split('-'); return p.length===3 ? p[2]+'/'+p[1] : iso; }

function render(m){
  const app=document.getElementById('app'); app.innerHTML='';
  const mes=m.mes_usd||0, tope=m.tope_mes_usd||0, porTarea=m.por_tarea||[], porDia=m.por_dia||[];

  const head=el('div',{className:'head'}, el('h1',{},STR.title));
  const rb=el('button',{className:'btn',title:STR.refresh}); rb.innerHTML=SVG_R+' '+STR.refresh;
  rb.addEventListener('click',()=>vscode.postMessage({type:'refresh'}));
  head.append(rb); app.append(head);

  // Este mes
  app.append(el('div',{className:'sec'},STR.this_month));
  const monthRow=el('div',{className:'month'}, el('span',{className:'big'}, money(mes)));
  if(tope>0) monthRow.append(el('span',{className:'cap'}, '/ '+money(tope)+' '+STR.cap.toLowerCase()));
  else monthRow.append(el('span',{className:'cap'}, STR.no_cap));
  app.append(monthRow);

  if(tope>0){
    const pct=Math.min(100, Math.round(mes/tope*100));
    let cls='bar'; if(mes>=tope)cls='bar over'; else if(pct>=80)cls='bar warn';
    const bar=el('div',{className:cls}); bar.append(el('i',{style:'width:'+pct+'%'}));
    app.append(bar);
    if(mes>=tope) app.append(el('div',{style:'margin-top:6px;font-size:12px;color:var(--vscode-testing-iconFailed,#e06a6a);font-weight:600'}, pct+'% · '+STR.over));
    else if(pct>=80) app.append(el('div',{style:'margin-top:6px;font-size:12px;color:var(--vscode-charts-orange,#d99a3a)'}, pct+'% · '+STR.near));
  }

  // Control del tope
  const form=el('div',{className:'capform'});
  form.append(el('label',{},STR.cap+':'));
  const inp=el('input',{type:'text',value:tope>0?String(tope):'',placeholder:STR.cap_ph});
  const save=el('button',{className:'btn primary'},STR.save);
  const commit=()=>{
    const raw=(inp.value||'').trim().replace(',','.');
    if(raw===''){ vscode.postMessage({type:'setCap',usd:0}); return; } // vacío = sin tope
    const v=Number(raw);
    if(!isFinite(v)||v<0){ showError(STR.bad_cap); return; } // texto no numérico ya no fija 0 en silencio
    vscode.postMessage({type:'setCap',usd:v});
  };
  save.addEventListener('click',commit);
  inp.addEventListener('keydown',(ev)=>{ if(ev.key==='Enter')commit(); });
  form.append(inp,save); app.append(form);

  app.append(el('div',{className:'note'}, STR.codex));

  // Por día
  app.append(el('div',{className:'sec'},STR.per_day));
  if(!porDia.length){ app.append(el('div',{className:'empty'},STR.no_spend)); }
  else {
    const max=Math.max.apply(null, porDia.map(d=>d.usd).concat([0.0001]));
    const days=el('div',{className:'days'});
    porDia.forEach(d=>{
      const h=Math.max(2, Math.round(d.usd/max*90));
      const c=el('div',{className:'day'});
      c.append(el('div',{className:'col',style:'height:'+h+'px',title:money(d.usd)}));
      c.append(el('div',{className:'lbl'}, shortDay(d.dia)));
      days.append(c);
    });
    app.append(days);
  }

  // Por tarea
  app.append(el('div',{className:'sec'},STR.per_task));
  if(!porTarea.length){ app.append(el('div',{className:'empty'},STR.no_spend)); return; }
  const rows=el('div',{className:'rows'});
  porTarea.forEach(g=>{
    const row=el('div',{className:'row'});
    row.append(el('div',{className:'name',title:g.tarea}, g.tarea));
    row.append(el('span',{className:'n'}, g.runs+' '+STR.runs));
    row.append(el('span',{className:'usd'}, money(g.usd)));
    rows.append(row);
  });
  app.append(rows);
}

function showError(msg){
  const app=document.getElementById('app');
  let box=document.getElementById('err'); if(!box){ box=el('div',{className:'errmsg',id:'err'}); app.prepend(box); }
  box.textContent=msg||'error';
}
`;

function makeNonce(): string {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  let s = '';
  for (let i = 0; i < 32; i++) s += chars[Math.floor(Math.random() * chars.length)];
  return s;
}

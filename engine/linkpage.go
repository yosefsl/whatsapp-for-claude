package main

// linkPage is the one screen the user sees: a live QR code, then a green "connected".
const linkPage = `<!doctype html>
<html lang="he" dir="rtl"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>חיבור WhatsApp ל-Claude</title>
<style>
:root{--bg:#f6f5f1;--card:#fff;--ink:#1d1d1b;--muted:#6b6a65;--ok:#1f8a4c;--line:#e4e2db}
@media (prefers-color-scheme:dark){:root{--bg:#161615;--card:#21211f;--ink:#f1f0ec;--muted:#a3a29c;--ok:#3fbf73;--line:#34332f}}
body{margin:0;background:var(--bg);color:var(--ink);font:17px/1.6 -apple-system,"Segoe UI",Arial,sans-serif;display:grid;place-items:center;min-height:100vh}
.card{background:var(--card);border:1px solid var(--line);border-radius:16px;padding:32px 36px;max-width:440px;width:calc(100% - 32px);box-sizing:border-box;text-align:center}
h1{font-size:22px;margin:0 0 6px}p{margin:6px 0;color:var(--muted)}
ol{text-align:right;margin:18px 0;padding-inline-start:22px}li{margin:6px 0}
#qr{width:280px;height:280px;margin:12px auto;display:grid;place-items:center;background:#fff;border-radius:12px}
#qr img{width:264px;height:264px}
.ok{font-size:64px;color:var(--ok);line-height:1}
button{font:inherit;padding:10px 20px;border-radius:10px;border:1px solid var(--line);background:var(--bg);color:var(--ink);cursor:pointer}
</style></head><body><div class="card" id="c"><p>טוען…</p></div>
<script>
const c=document.getElementById('c');
let last='';
function render(s){
  const key=s.state+(s.qr||'');
  if(key===last)return; last=key;
  if(s.state==='connected'||s.state==='linked_reconnecting'){
    c.innerHTML='<div class="ok">✓</div><h1>WhatsApp מחובר</h1><p>אפשר לסגור את החלון ולחזור ל-Claude.</p>';return;}
  if(s.qr){
    c.innerHTML='<h1>חיבור WhatsApp ל-Claude</h1><ol><li>פותחים WhatsApp בטלפון</li><li>הגדרות ← מכשירים מקושרים ← קישור מכשיר</li><li>מכוונים את המצלמה לקוד הזה</li></ol><div id="qr"><img src="'+s.qr+'" alt="QR"></div><p>הקוד מתחלף לבד כל כמה שניות. זה תקין.</p>';return;}
  c.innerHTML='<h1>חיבור WhatsApp ל-Claude</h1><p>הקוד פג תוקף.</p><p><button onclick="again()">להציג קוד חדש</button></p>';
}
async function poll(method){
  try{const r=await fetch('/link.json',{method:method||'GET'});render(await r.json());}catch(e){}
}
function again(){c.innerHTML='<p>מכין קוד…</p>';last='';poll('POST');}
poll('POST');setInterval(poll,2000);
</script></body></html>`

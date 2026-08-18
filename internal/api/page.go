package api

import (
	"fmt"
	"net/http"
)

// operationPage is the embedded ceremonies operations page. It drives the JSON
// API with a small client so an operator can lock, gather witnesses, sign,
// review and seal a real ceremony.
const operationPage = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>QuorumForge 根密钥签名仪式</title>
<style>
  body { font-family: system-ui, sans-serif; max-width: 760px; margin: 2rem auto; padding: 0 1rem; color: #1a1a2e; }
  h1 { font-size: 1.4rem; } h2 { font-size: 1.05rem; margin-top: 1.4rem; }
  label { display: block; margin-top: .5rem; font-size: .85rem; }
  input, button { padding: .4rem .6rem; margin-top: .2rem; }
  button { margin-right: .4rem; margin-top: .4rem; cursor: pointer; }
  #state { background: #f0f4ff; border-radius: 6px; padding: .8rem; white-space: pre-wrap; font-size: .85rem; }
  #error { color: #b00020; font-size: .85rem; min-height: 1rem; }
  .row { display: flex; gap: 1rem; flex-wrap: wrap; }
</style>
</head>
<body>
<h1>QuorumForge 根密钥签名仪式操作台</h1>
<div class="row">
  <label>仪式 ID <input id="id" placeholder="ceremony-1" value="ceremony-1"></label>
  <label>操作号 <input id="op" placeholder="op-1" value="op-1"></label>
</div>
<h2>状态</h2>
<pre id="state">（尚未加载）</pre>
<div id="error"></div>
<h2>操作</h2>
<button onclick="create()">创建</button>
<button onclick="lock()">锁定</button>
<button onclick="witness('alice')">alice 见证</button>
<button onclick="witness('bob')">bob 见证</button>
<button onclick="witness('carol')">carol 见证</button>
<button onclick="begin()">开始签名</button>
<button onclick="receipt()">登记回执</button>
<button onclick="artifact()">登记产物</button>
<button onclick="review('approved')">复核通过</button>
<button onclick="review('rejected')">复核拒绝</button>
<button onclick="seal()">封存</button>
<button onclick="quarantine()">安全隔离</button>
<button onclick="cancel()">取消</button>
<button onclick="reload()">刷新</button>

<script>
const base = '/api/v1/ceremonies';
const id = () => document.getElementById('id').value;
const op = () => document.getElementById('op').value;
function setErr(e) { document.getElementById('error').textContent = e || ''; }
async function call(method, suffix, body) {
  setErr('');
  const url = suffix ? base + '/' + id() + suffix : base;
  const res = await fetch(url, {
    method,
    headers: {'Content-Type': 'application/json'},
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) { setErr(data.error || ('HTTP ' + res.status)); return; }
  await reload();
  return data;
}
async function reload() {
  try {
    const res = await fetch(base + '/' + id());
    const data = await res.json();
    document.getElementById('state').textContent = JSON.stringify(data, null, 2);
  } catch (e) { setErr(String(e)); }
}
async function create() { await call('POST', '', {id: id()}); }
async function lock() {
  await call('POST', '/lock', {
    operation: op(), revision: 0, digest: 'sha256:root-req-1',
    key_version: 'key-v1', policy_version: 'policy-v1',
    participants: ['alice', 'bob', 'carol'],
  });
}
async function witness(person) {
  await call('POST', '/witness', {
    operation: op() + '-' + person, revision: await rev(), person_id: person,
    credential: person + '-cred', identity_revision: 1,
  });
}
async function begin() {
  await call('POST', '/begin', {
    operation: op() + '-begin', revision: await rev(), token: 'tok-' + Date.now(), session_id: 'session-1',
  });
}
async function receipt() {
  await call('POST', '/receipt', {
    operation: op() + '-receipt', revision: await rev(), session_id: 'session-1', receipt: 'hsm-receipt-1',
  });
}
async function artifact() {
  await call('POST', '/artifact', {
    operation: op() + '-artifact', revision: await rev(), digest: 'sha256:root-req-1',
  });
}
async function review(conclusion) {
  await call('POST', '/review', {
    operation: op() + '-review', revision: await rev(), conclusion, review_digest: 'review-digest-1',
  });
}
async function seal() {
  await call('POST', '/seal', {operation: op() + '-seal', revision: await rev()});
}
async function quarantine() {
  await call('POST', '/quarantine', {operation: op() + '-quarantine', revision: await rev()});
}
async function cancel() {
  await call('POST', '/cancel', {operation: op() + '-cancel', revision: await rev()});
}
async function rev() {
  const res = await fetch(base + '/' + id());
  if (!res.ok) return 0;
  const data = await res.json();
  return data.revision;
}
reload();
</script>
</body>
</html>`

// handlePage serves the operations page.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, operationPage)
}

/* ── State ──────────────────────────────────────────── */
const state = {
  currentJobID: null,
  currentFile: null,
  eventSource: null,
  agents: {
    architect: { name: 'Architect', icon: '🏗', status: 'pending' },
    planner:   { name: 'Planner',   icon: '📐', status: 'pending' },
    builder:   { name: 'Builder',   icon: '🧩', status: 'pending' },
    reviewer:  { name: 'Reviewer',  icon: '🔍', status: 'pending' },
    tester:    { name: 'Tester',    icon: '🧪', status: 'pending' },
    iterator:  { name: 'Iterator',  icon: '🔄', status: 'pending' },
  },
  fileCount: 0,
  thoughtCount: 0,
};

/* ── DOM helpers ────────────────────────────────────── */
const $ = id => document.getElementById(id);
const el = (tag, cls, html) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (html) e.innerHTML = html;
  return e;
};

/* ── Screen navigation ──────────────────────────────── */
function showScreen(name) {
  document.querySelectorAll('.screen').forEach(s => s.classList.remove('active'));
  $(`screen-${name}`).classList.add('active');
}

/* ── Agent status helpers ───────────────────────────── */
function setAgentStatus(agent, status) {
  if (!state.agents[agent]) return;
  state.agents[agent].status = status;
  renderAgentList();
}

function renderAgentList() {
  const list = $('agent-list');
  list.innerHTML = '';
  for (const [key, a] of Object.entries(state.agents)) {
    const item = el('div', `agent-item ${a.status}`);
    let badgeHtml = '';
    if (a.status === 'running') {
      badgeHtml = `<span class="badge badge-running">running</span><span class="spinner"></span>`;
    } else if (a.status === 'complete') {
      badgeHtml = `<span class="badge badge-complete">✓ done</span>`;
    } else if (a.status === 'error') {
      badgeHtml = `<span class="badge badge-error">✗ error</span>`;
    } else {
      badgeHtml = `<span class="badge badge-pending">pending</span>`;
    }
    item.innerHTML = `
      <span class="agent-icon">${a.icon}</span>
      <span class="agent-name">${a.name}</span>
      ${badgeHtml}
    `;
    list.appendChild(item);
  }
}

/* ── Log helpers ────────────────────────────────────── */
function addLog(type, agent, message) {
  const logEl = $('log-output');
  const now = new Date().toLocaleTimeString('en-US', { hour12: false });
  const line = el('div', `log-line log-type-${type}`);
  const agentPart = agent ? `<span class="log-agent ${agent}">[${agent}]</span>` : '';
  line.innerHTML = `<span class="log-time">${now}</span>${agentPart}<span>${escHtml(message)}</span>`;
  logEl.appendChild(line);
  logEl.scrollTop = logEl.scrollHeight;
}

function escHtml(str) {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

/* ── SSE connection ─────────────────────────────────── */
function connectSSE(jobID) {
  if (state.eventSource) state.eventSource.close();

  const es = new EventSource(`/api/events/${jobID}`);
  state.eventSource = es;

  const handle = type => es.addEventListener(type, e => handleEvent(type, JSON.parse(e.data)));

  ['agent_start', 'agent_complete', 'thought', 'file_created',
   'review_note', 'pipeline_complete', 'error'].forEach(handle);

  es.onerror = () => addLog('error', '', 'SSE connection lost');
}

function handleEvent(type, data) {
  const agent = (data.agent || '').toLowerCase();
  const msg = data.message || '';

  addLog(type, agent, msg);

  switch (type) {
    case 'agent_start':
      setAgentStatus(agent, 'running');
      break;

    case 'agent_complete':
      setAgentStatus(agent, 'complete');
      break;

    case 'file_created':
      state.fileCount++;
      $('stat-files').textContent = state.fileCount;
      break;

    case 'thought':
      state.thoughtCount++;
      $('stat-thoughts').textContent = state.thoughtCount;
      break;

    case 'review_note':
      if (data.payload) addReviewNoteToPanel(data.payload);
      break;

    case 'pipeline_complete':
      if (state.eventSource) state.eventSource.close();
      setTimeout(() => loadResults(state.currentJobID), 300);
      break;

    case 'error':
      Object.keys(state.agents).forEach(k => {
        if (state.agents[k].status === 'running') setAgentStatus(k, 'error');
      });
      showToast('Pipeline error: ' + msg, 'error');
      break;
  }
}

/* ── Generate form ──────────────────────────────────── */
$('generate-form').addEventListener('submit', async e => {
  e.preventDefault();
  const prompt = $('prompt-input').value.trim();
  if (!prompt) return;

  const body = {
    prompt,
    provider: $('provider-select').value,
    model: $('model-input').value,
    output_dir: $('output-dir').value || './output',
  };

  const btn = $('generate-btn');
  btn.disabled = true;
  btn.textContent = 'Starting…';

  try {
    const res = await fetch('/api/generate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });

    if (!res.ok) {
      const text = await res.text();
      throw new Error(text);
    }

    const { job_id } = await res.json();
    state.currentJobID = job_id;

    // Reset pipeline state
    Object.keys(state.agents).forEach(k => state.agents[k].status = 'pending');
    state.fileCount = 0;
    state.thoughtCount = 0;
    $('log-output').innerHTML = '';
    renderAgentList();
    $('stat-files').textContent = '0';
    $('stat-thoughts').textContent = '0';
    $('pipeline-prompt-label').textContent = prompt;

    showScreen('pipeline');
    connectSSE(job_id);
  } catch (err) {
    showToast('Error: ' + err.message, 'error');
  } finally {
    btn.disabled = false;
    btn.textContent = '✨ Generate';
  }
});

/* ── Results screen ─────────────────────────────────── */
async function loadResults(jobID) {
  showScreen('results');
  await Promise.all([loadFileTree(jobID), loadReviewNotes(jobID), loadCoT(jobID)]);
}

async function loadFileTree(jobID) {
  try {
    const res = await fetch(`/api/jobs/${jobID}/files`);
    const files = await res.json();
    renderFileTree(files);
    if (files.length > 0) loadFileContent(jobID, files[0].path);
  } catch (err) {
    showToast('Could not load files: ' + err.message, 'error');
  }
}

function renderFileTree(files) {
  const tree = $('file-tree');
  tree.innerHTML = '';
  if (!files || files.length === 0) {
    tree.innerHTML = '<div class="empty-state"><span class="icon">📭</span>No files generated</div>';
    return;
  }
  files.forEach(f => {
    const item = el('div', 'file-item');
    const ext = f.path.split('.').pop();
    item.innerHTML = `<span class="file-icon">${fileIcon(ext)}</span><span>${escHtml(f.path)}</span>`;
    item.title = f.path;
    item.addEventListener('click', () => {
      document.querySelectorAll('.file-item').forEach(i => i.classList.remove('active'));
      item.classList.add('active');
      loadFileContent(state.currentJobID, f.path);
    });
    tree.appendChild(item);
  });
}

async function loadFileContent(jobID, path) {
  state.currentFile = path;
  $('viewer-path').textContent = path;
  try {
    const res = await fetch(`/api/jobs/${jobID}/files/${path}`);
    const text = await res.text();
    const codeEl = $('code-content');
    codeEl.textContent = text;
    codeEl.className = `language-${langFromPath(path)}`;
    if (window.Prism) Prism.highlightElement(codeEl);
  } catch (err) {
    $('code-content').textContent = 'Error loading file: ' + err.message;
  }
}

async function loadReviewNotes(jobID) {
  try {
    const res = await fetch(`/api/jobs/${jobID}/context`);
    const ctx = await res.json();
    const notes = ctx.review_notes || [];
    const list = $('review-list');
    list.innerHTML = '';
    if (notes.length === 0) {
      list.innerHTML = '<div class="empty-state" style="padding:12px;font-size:12px;color:var(--accent2)">✅ No issues found</div>';
      return;
    }
    notes.forEach(n => addReviewNoteToPanel(n));
  } catch {}
}

function addReviewNoteToPanel(note) {
  const list = $('review-list');
  const item = el('div', `review-note ${note.severity}`);
  item.innerHTML = `
    <div class="note-header">
      <span class="note-severity ${note.severity}">${note.severity}</span>
      <span class="note-file" title="${escHtml(note.file)}">${escHtml(note.file)}</span>
    </div>
    <div class="note-message">${escHtml(note.message)}</div>
    ${note.suggestion ? `<div class="note-message" style="color:var(--text-muted);margin-top:4px">${escHtml(note.suggestion)}</div>` : ''}
  `;
  list.appendChild(item);
}

async function loadCoT(jobID) {
  try {
    const res = await fetch(`/api/jobs/${jobID}/context`);
    const ctx = await res.json();
    const steps = ctx.chain_of_thought || [];
    const list = $('cot-list');
    list.innerHTML = '';
    steps.forEach(s => {
      const item = el('div', 'cot-step');
      item.innerHTML = `<span class="cot-agent ${s.agent}">[${s.agent}]</span>${escHtml(s.thought)}`;
      list.appendChild(item);
    });
  } catch {}
}

/* ── Iteration ──────────────────────────────────────── */
$('iterate-btn').addEventListener('click', async () => {
  const feedback = $('iterate-input').value.trim();
  if (!feedback || !state.currentJobID) return;

  try {
    const res = await fetch(`/api/jobs/${state.currentJobID}/iterate`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ feedback }),
    });
    if (!res.ok) throw new Error(await res.text());

    $('iterate-input').value = '';
    $('review-list').innerHTML = '';
    $('cot-list').innerHTML = '';

    // Show pipeline screen again for live updates
    Object.keys(state.agents).forEach(k => {
      if (k === 'iterator' || k === 'reviewer') state.agents[k].status = 'pending';
    });
    state.fileCount = 0;
    state.thoughtCount = 0;
    $('log-output').innerHTML = '';
    renderAgentList();
    $('stat-files').textContent = '0';
    $('stat-thoughts').textContent = '0';
    $('pipeline-prompt-label').textContent = 'Iterating: ' + feedback;

    showScreen('pipeline');
    connectSSE(state.currentJobID);
  } catch (err) {
    showToast('Iterate error: ' + err.message, 'error');
  }
});

/* ── Toasts ─────────────────────────────────────────── */
function showToast(msg, type = 'success') {
  const t = el('div', `toast ${type}`, escHtml(msg));
  $('toast-container').appendChild(t);
  setTimeout(() => t.remove(), 4000);
}

/* ── Utility ────────────────────────────────────────── */
function fileIcon(ext) {
  const map = {
    go: '🔵', ts: '🔷', tsx: '⚛️', js: '🟡', jsx: '⚛️',
    py: '🐍', rs: '🦀', java: '☕', cs: '🎯',
    html: '🌐', css: '🎨', scss: '🎨', json: '📋',
    yaml: '⚙️', yml: '⚙️', md: '📝', sh: '🐚',
    sql: '🗄️', dockerfile: '🐳', toml: '⚙️', env: '🔑',
  };
  return map[ext.toLowerCase()] || '📄';
}

function langFromPath(path) {
  const ext = path.split('.').pop().toLowerCase();
  const map = {
    go: 'go', ts: 'typescript', tsx: 'tsx', js: 'javascript', jsx: 'jsx',
    py: 'python', rs: 'rust', java: 'java', cs: 'csharp',
    html: 'html', css: 'css', scss: 'scss', json: 'json',
    yaml: 'yaml', yml: 'yaml', md: 'markdown', sh: 'bash',
    sql: 'sql', toml: 'toml',
  };
  return map[ext] || 'plaintext';
}

/* ── Init ───────────────────────────────────────────── */
renderAgentList();
showScreen('prompt');

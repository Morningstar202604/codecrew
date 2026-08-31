/* ===== CodeCrew Web 前端逻辑 ===== */

// ---------- 状态 ----------
const state = {
  sessionId: null,
  currentRole: 'developer',
  currentModel: '',
  isStreaming: false,
  abortController: null,
};

// ---------- 工具函数 ----------
const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => document.querySelectorAll(sel);

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

function showToast(msg, type = '') {
  const toast = $('#toast');
  toast.textContent = msg;
  toast.className = 'toast show ' + type;
  setTimeout(() => { toast.className = 'toast ' + type; }, 2500);
}

function formatTime(iso) {
  try {
    const d = new Date(iso);
    return d.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' }) + ' ' +
           d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
  } catch { return iso; }
}

// ---------- API 调用 ----------
async function api(path, options = {}) {
  const opts = {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  };
  if (opts.body && typeof opts.body === 'object') {
    opts.body = JSON.stringify(opts.body);
  }
  const res = await fetch(path, opts);
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.error || `HTTP ${res.status}`);
  }
  return data;
}

// SSE 流式对话
async function streamChat(message) {
  if (state.isStreaming) return;
  state.isStreaming = true;
  $('#sendBtn').disabled = true;

  // 添加用户消息
  addMessage('user', message);
  hideWelcome();

  // 创建 AI 消息占位
  const aiMsgEl = addMessage('ai', '');
  const contentEl = aiMsgEl.querySelector('.message-content');
  contentEl.innerHTML = '<span class="streaming-cursor"></span>';

  let fullText = '';
  let toolBuffer = '';

  try {
    state.abortController = new AbortController();
    const res = await fetch('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session: state.sessionId, message }),
      signal: state.abortController.signal,
    });

    if (!res.ok) {
      const err = await res.json().catch(() => ({}));
      throw new Error(err.error || `HTTP ${res.status}`);
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });

      // 解析 SSE 事件
      const events = buffer.split('\n\n');
      buffer = events.pop() || '';

      for (const event of events) {
        const lines = event.split('\n');
        for (const line of lines) {
          if (!line.startsWith('data: ')) continue;
          const dataStr = line.slice(6);
          if (dataStr === '[DONE]') continue;
          try {
            const data = JSON.parse(dataStr);
            if (data.type === 'session') {
              state.sessionId = data.id;
            } else if (data.type === 'output') {
              fullText += data.content;
              renderMessageContent(contentEl, fullText);
              // 滚动到底部
              scrollToBottom();
            } else if (data.type === 'done') {
              // 完成
            }
          } catch (e) { /* 忽略解析错误 */ }
        }
      }
    }

    // 移除光标
    const cursor = contentEl.querySelector('.streaming-cursor');
    if (cursor) cursor.remove();

    // 如果最终内容为空，显示提示
    if (!fullText.trim()) {
      contentEl.innerHTML = '<span style="color:var(--text-muted)">（无输出）</span>';
    }

    // 刷新状态
    refreshAll();

  } catch (err) {
    if (err.name === 'AbortError') {
      contentEl.innerHTML = '<span style="color:var(--warning)">（已中断）</span>';
    } else {
      contentEl.innerHTML = `<span style="color:var(--danger)">错误: ${escapeHtml(err.message)}</span>`;
      showToast(err.message, 'error');
    }
  } finally {
    state.isStreaming = false;
    state.abortController = null;
    $('#sendBtn').disabled = false;
    // 保存消息到本地（用于刷新后恢复，简单实现）
    saveMessagesToLocal();
  }
}

// ---------- 消息渲染 ----------
function addMessage(role, content) {
  const messages = $('#messages');
  const msg = document.createElement('div');
  msg.className = 'message ' + role;

  const avatar = document.createElement('div');
  avatar.className = 'message-avatar';
  avatar.textContent = role === 'user' ? '我' : state.currentRole.charAt(0).toUpperCase();

  const body = document.createElement('div');
  body.className = 'message-body';

  const roleLabel = document.createElement('div');
  roleLabel.className = 'message-role';
  roleLabel.textContent = role === 'user' ? '你' : state.currentRole;

  const contentEl = document.createElement('div');
  contentEl.className = 'message-content';
  if (content) renderMessageContent(contentEl, content);

  body.appendChild(roleLabel);
  body.appendChild(contentEl);
  msg.appendChild(avatar);
  msg.appendChild(body);
  messages.appendChild(msg);
  scrollToBottom();
  return msg;
}

function renderMessageContent(el, text) {
  // 先移除光标
  const cursor = el.querySelector('.streaming-cursor');
  el.innerHTML = renderMarkdown(text);
  if (cursor && !text.trim()) {
    el.innerHTML = '<span class="streaming-cursor"></span>';
  }
}

// 简易 Markdown 渲染
function renderMarkdown(text) {
  if (!text) return '';
  let html = escapeHtml(text);

  // 代码块 ```lang ... ```
  html = html.replace(/```(\w*)\n([\s\S]*?)```/g, (m, lang, code) => {
    return `<pre><code class="lang-${lang}">${code.trim()}</code></pre>`;
  });

  // 行内代码 `code`
  html = html.replace(/`([^`]+)`/g, '<code>$1</code>');

  // 标题
  html = html.replace(/^### (.+)$/gm, '<h3>$1</h3>');
  html = html.replace(/^## (.+)$/gm, '<h2>$1</h2>');
  html = html.replace(/^# (.+)$/gm, '<h1>$1</h1>');

  // 粗体 **text**
  html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');

  // 斜体 *text*
  html = html.replace(/(^|\s)\*([^*]+)\*(?=\s|$)/g, '$1<em>$2</em>');

  // 链接 [text](url)
  html = html.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank">$1</a>');

  // 引用 > text
  html = html.replace(/^&gt; (.+)$/gm, '<blockquote>$1</blockquote>');

  // 无序列表 - item
  html = html.replace(/(?:^|\n)([-*]) (.+)/g, (m, bullet, item) => {
    return '\n<li>' + item + '</li>';
  });
  // 包裹连续的 li
  html = html.replace(/(<li>.*<\/li>\n?)+/g, (m) => '<ul>' + m + '</ul>');

  // 有序列表 1. item
  html = html.replace(/(?:^|\n)(\d+)\. (.+)/g, (m, num, item) => {
    return '\n<li>' + item + '</li>';
  });

  // 段落（空行分隔）
  html = html.replace(/\n{2,}/g, '</p><p>');
  html = '<p>' + html + '</p>';
  // 清理块级元素外的 p 标签
  html = html.replace(/<p>\s*<(h[1-6]|pre|ul|ol|blockquote)/g, '<$1');
  html = html.replace(/<\/(h[1-6]|pre|ul|ol|blockquote)>\s*<\/p>/g, '</$1>');
  html = html.replace(/<p><\/p>/g, '');

  // 工具调用标记（REPL 输出中的 🔧 和 →）
  html = html.replace(/🔧\s*(\w+)(.*?)(?=\n|$)/g, (m, tool, args) => {
    return `<div class="tool-call"><div class="tool-call-header"><span>🔧</span><span class="tool-call-name">${tool}</span><span class="tool-call-args">${escapeHtml(args.trim())}</span></div></div>`;
  });

  // diff 标记
  if (html.includes('变更预览') || (html.includes('--- ') && html.includes('+++ ') && html.includes('@@'))) {
    html = highlightDiff(html);
  }

  return html;
}

function highlightDiff(html) {
  // 简单的 diff 高亮：把以 - 开头的行标红，+ 开头的行标绿
  return html.replace(/^(-.+)$/gm, '<span class="diff-line diff-del">$1</span>')
             .replace(/^(\+.+)$/gm, '<span class="diff-line diff-add">$1</span>')
             .replace(/^(@@.+@@)$/gm, '<span class="diff-hunk">$1</span>');
}

function scrollToBottom() {
  const container = $('#chatContainer');
  container.scrollTop = container.scrollHeight;
}

function hideWelcome() {
  const welcome = $('#welcomeScreen');
  if (welcome) welcome.style.display = 'none';
}

function showWelcome() {
  const welcome = $('#welcomeScreen');
  if (welcome) welcome.style.display = 'flex';
  $('#messages').innerHTML = '';
}

// 简单的本地消息存储（刷新页面后恢复）
function saveMessagesToLocal() {
  try {
    const messages = $('#messages').innerHTML;
    localStorage.setItem('codecrew_messages_' + (state.sessionId || 'default'), messages);
    localStorage.setItem('codecrew_session', state.sessionId || '');
    localStorage.setItem('codecrew_role', state.currentRole);
  } catch (e) {}
}

function loadMessagesFromLocal() {
  try {
    const sid = localStorage.getItem('codecrew_session');
    const role = localStorage.getItem('codecrew_role');
    if (sid) state.sessionId = sid;
    if (role) state.currentRole = role;
    const messages = localStorage.getItem('codecrew_messages_' + (sid || 'default'));
    if (messages && $('#messages')) {
      $('#messages').innerHTML = messages;
      hideWelcome();
    }
  } catch (e) {}
}

// ---------- 数据刷新 ----------
async function refreshAll() {
  await Promise.all([
    refreshRoles(),
    refreshModel(),
    refreshSessions(),
    refreshTools(),
    refreshMemory(),
    refreshPlan(),
    refreshConfig(),
    refreshStats(),
  ]);
}

async function refreshRoles() {
  try {
    const data = await api('/api/roles?session=' + encodeURIComponent(state.sessionId || ''));
    const selector = $('#roleSelector');
    const memSelector = $('#memoryRoleSelector');
    selector.innerHTML = '';
    memSelector.innerHTML = '';
    for (const r of data.roles) {
      const opt = document.createElement('option');
      opt.value = r.name;
      opt.textContent = r.name + ' — ' + r.description;
      selector.appendChild(opt);
      const opt2 = opt.cloneNode(true);
      memSelector.appendChild(opt2);
    }
    selector.value = data.current;
    memSelector.value = data.current;
    state.currentRole = data.current;
    $('#currentRoleLabel').textContent = data.current;
  } catch (e) {}
}

async function refreshModel() {
  try {
    const data = await api('/api/model?session=' + encodeURIComponent(state.sessionId || ''));
    state.currentModel = data.current;
    $('#modelName').textContent = data.current || '未配置';
    $('#currentModelLabel').textContent = data.current || '未配置模型';
    const dot = document.querySelector('.model-dot');
    if (dot) dot.classList.toggle('online', !!data.current);

    // 模型选择器
    const selector = $('#modelSelector');
    selector.innerHTML = '';
    for (const p of data.providers || []) {
      for (const m of p.models || []) {
        const opt = document.createElement('option');
        opt.value = p.name + '/' + m;
        opt.textContent = p.name + '/' + m;
        selector.appendChild(opt);
      }
    }
    if (data.current) selector.value = data.current;
  } catch (e) {}
}

async function refreshSessions() {
  try {
    const data = await api('/api/sessions?session=' + encodeURIComponent(state.sessionId || ''));
    const list = $('#sessionList');
    if (!data.sessions || data.sessions.length === 0) {
      list.innerHTML = '<div class="session-empty">暂无历史会话</div>';
      return;
    }
    list.innerHTML = '';
    for (const s of data.sessions.slice(0, 30)) {
      const item = document.createElement('div');
      item.className = 'session-item' + (s.id === state.sessionId ? ' active' : '');
      item.innerHTML = `<span style="flex:1;overflow:hidden;text-overflow:ellipsis">${escapeHtml(s.preview || s.id)}</span><span class="session-time">${formatTime(s.created_at)}</span>`;
      item.onclick = () => resumeSession(s.id);
      list.appendChild(item);
    }
  } catch (e) {}
}

async function refreshTools() {
  try {
    const data = await api('/api/tools?session=' + encodeURIComponent(state.sessionId || ''));
    const list = $('#toolsList');
    list.innerHTML = '';
    for (const t of data.tools || []) {
      const item = document.createElement('div');
      item.className = 'tool-item';
      item.innerHTML = `
        <span class="tool-name">${escapeHtml(t.name)}</span>
        <span class="tool-perm ${t.permission}">${t.permission}</span>
      `;
      item.title = t.description || '';
      list.appendChild(item);
    }
  } catch (e) {}
}

async function refreshMemory() {
  try {
    const role = $('#memoryRoleSelector').value || state.currentRole;
    const data = await api('/api/memory?session=' + encodeURIComponent(state.sessionId || '') + '&role=' + encodeURIComponent(role));
    $('#memoryContent').textContent = data.content || '（暂无记忆，用下方输入框添加）';
  } catch (e) {
    $('#memoryContent').textContent = '加载失败: ' + e.message;
  }
}

async function refreshPlan() {
  try {
    const data = await api('/api/plan?session=' + encodeURIComponent(state.sessionId || ''));
    const list = $('#planList');
    if (!data.tasks || data.tasks.length === 0) {
      list.innerHTML = '<div class="empty-text">暂无计划任务</div>';
      return;
    }
    list.innerHTML = '';
    for (const t of data.tasks) {
      const item = document.createElement('div');
      item.className = 'plan-item';
      const statusMap = { todo: '待办', doing: '进行中', done: '完成', blocked: '阻塞' };
      item.innerHTML = `<span class="plan-status ${t.status}">${t.status === 'done' ? '✓' : (t.status === 'doing' ? '>' : (t.status === 'blocked' ? '!' : ''))}</span><span class="plan-title">#${t.id} ${escapeHtml(t.title)}</span>`;
      list.appendChild(item);
    }
  } catch (e) {}
}

async function refreshConfig() {
  try {
    const data = await api('/api/config?session=' + encodeURIComponent(state.sessionId || ''));
    // 供应商
    const providers = $('#configProviders');
    providers.innerHTML = '';
    for (const [name, p] of Object.entries(data.providers || {})) {
      const div = document.createElement('div');
      div.className = 'config-provider';
      div.innerHTML = `
        <div class="config-provider-name">${escapeHtml(name)} ${p.api_key ? '<span style="color:var(--success);font-size:10px">●已配置</span>' : '<span style="color:var(--danger);font-size:10px">●无密钥</span>'}</div>
        <div class="config-provider-url">${escapeHtml(p.base_url)}</div>
        ${p.models && p.models.length ? `<div class="config-provider-models">模型: ${p.models.map(escapeHtml).join(', ')}</div>` : ''}
      `;
      providers.appendChild(div);
    }
    // 参数
    const params = $('#configParams');
    params.innerHTML = `
      <div class="config-param"><span class="config-param-key">当前模型</span><span class="config-param-val">${escapeHtml(data.model || '-')}</span></div>
      <div class="config-param"><span class="config-param-key">工作目录</span><span class="config-param-val">${escapeHtml(data.working_dir || '-')}</span></div>
      <div class="config-param"><span class="config-param-key">上下文预算</span><span class="config-param-val">${data.max_context_tokens || '-'}</span></div>
      <div class="config-param"><span class="config-param-key">工具轮数上限</span><span class="config-param-val">${data.max_tool_rounds || '-'}</span></div>
      <div class="config-param"><span class="config-param-key">配置文件</span><span class="config-param-val">${escapeHtml(data.source || '-')}</span></div>
    `;
  } catch (e) {}
}

async function refreshStats() {
  try {
    const [ctx, cost] = await Promise.all([
      api('/api/context?session=' + encodeURIComponent(state.sessionId || '')),
      api('/api/cost?session=' + encodeURIComponent(state.sessionId || '')),
    ]);
    $('#statContextUsed').textContent = ctx.used_tokens + ' tokens';
    $('#statContextLimit').textContent = ctx.limit_tokens + ' tokens';
    $('#statMessages').textContent = ctx.messages;
    $('#statCompactions').textContent = ctx.compactions;
    $('#statTurns').textContent = cost.turns;
    $('#statPrompt').textContent = cost.prompt_tokens;
    $('#statCompletion').textContent = cost.completion_tokens;
    $('#statTotal').textContent = cost.total_tokens;
    $('#statElapsed').textContent = cost.elapsed_seconds + 's';
    $('#contextHint').textContent = '上下文: ' + ctx.used_tokens + '/' + ctx.limit_tokens;
  } catch (e) {}
}

// ---------- 操作 ----------
async function resumeSession(id) {
  try {
    await api('/api/sessions/resume', { method: 'POST', body: { session: state.sessionId, id } });
    state.sessionId = id;
    // 清空当前消息，重新加载
    $('#messages').innerHTML = '';
    showWelcome();
    // 尝试从本地恢复
    loadMessagesFromLocal();
    refreshAll();
    showToast('已恢复会话', 'success');
  } catch (e) {
    showToast(e.message, 'error');
  }
}

async function newSession() {
  state.sessionId = null;
  $('#messages').innerHTML = '';
  showWelcome();
  localStorage.removeItem('codecrew_messages_default');
  localStorage.removeItem('codecrew_session');
  refreshAll();
  showToast('已新建对话', 'success');
}

async function switchRole(role) {
  try {
    await api('/api/roles/switch', { method: 'POST', body: { session: state.sessionId, role } });
    state.currentRole = role;
    $('#currentRoleLabel').textContent = role;
    refreshRoles();
    showToast('已切换到 ' + role, 'success');
  } catch (e) {
    showToast(e.message, 'error');
  }
}

async function switchModel() {
  const model = $('#modelSelector').value;
  if (!model) return;
  try {
    await api('/api/model/switch', { method: 'POST', body: { session: state.sessionId, model } });
    refreshModel();
    showToast('已切换模型: ' + model, 'success');
  } catch (e) {
    showToast(e.message, 'error');
  }
}

async function performAction(action) {
  try {
    switch (action) {
      case 'clear':
        await api('/api/history/clear', { method: 'POST', body: { session: state.sessionId } });
        $('#messages').innerHTML = '';
        showWelcome();
        showToast('已清空对话', 'success');
        break;
      case 'undo':
        await api('/api/history/undo', { method: 'POST', body: { session: state.sessionId } });
        showToast('已撤销上轮', 'success');
        break;
      case 'compact':
        await api('/api/context/compact', { method: 'POST', body: { session: state.sessionId } });
        showToast('已压缩上下文', 'success');
        break;
      case 'reload':
        await api('/api/config/reload', { method: 'POST', body: { session: state.sessionId } });
        showToast('配置已重载', 'success');
        break;
    }
    refreshStats();
  } catch (e) {
    showToast(e.message, 'error');
  }
}

async function addMemory() {
  const note = $('#memoryInput').value.trim();
  if (!note) { showToast('笔记内容不能为空', 'error'); return; }
  const role = $('#memoryRoleSelector').value;
  try {
    await api('/api/memory/add', { method: 'POST', body: { session: state.sessionId, role, note } });
    $('#memoryInput').value = '';
    refreshMemory();
    showToast('已添加记忆', 'success');
  } catch (e) {
    showToast(e.message, 'error');
  }
}

async function clearMemory() {
  const role = $('#memoryRoleSelector').value;
  if (!confirm('确定要清空 ' + role + ' 的所有记忆吗？')) return;
  try {
    await api('/api/memory/clear', { method: 'POST', body: { session: state.sessionId, role } });
    refreshMemory();
    showToast('已清空记忆', 'success');
  } catch (e) {
    showToast(e.message, 'error');
  }
}

// ---------- 事件绑定 ----------
function bindEvents() {
  // 发送消息
  $('#sendBtn').onclick = () => sendMessage();
  $('#messageInput').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  });

  // 自动调整输入框高度
  $('#messageInput').addEventListener('input', function() {
    this.style.height = 'auto';
    this.style.height = Math.min(this.scrollHeight, 200) + 'px';
  });

  // 新建对话
  $('#newChatBtn').onclick = newSession;

  // 角色切换
  $('#roleSelector').onchange = (e) => switchRole(e.target.value);

  // 模型切换
  $('#modelSwitchBtn').onclick = switchModel;

  // 侧边栏
  $('#sidebarOpen').onclick = () => $('#sidebar').classList.add('open');
  $('#sidebarClose').onclick = () => $('#sidebar').classList.remove('open');
  $('#sidebarOverlay').onclick = () => $('#sidebar').classList.remove('open');

  // 右侧面板
  $('#panelToggle').onclick = () => $('#rightPanel').classList.toggle('open');
  $('#panelClose').onclick = () => $('#rightPanel').classList.remove('open');
  $('#panelOverlay').onclick = () => $('#rightPanel').classList.remove('open');

  // 面板标签
  $$('.panel-tab').forEach(tab => {
    tab.onclick = () => {
      $$('.panel-tab').forEach(t => t.classList.remove('active'));
      $$('.panel-pane').forEach(p => p.classList.remove('active'));
      tab.classList.add('active');
      $('#pane-' + tab.dataset.tab).classList.add('active');
      // 切换到对应标签时刷新数据
      if (tab.dataset.tab === 'memory') refreshMemory();
      if (tab.dataset.tab === 'plan') refreshPlan();
      if (tab.dataset.tab === 'config') refreshConfig();
      if (tab.dataset.tab === 'stats') refreshStats();
      if (tab.dataset.tab === 'tools') refreshTools();
    };
  });

  // 快捷操作
  $$('.action-btn').forEach(btn => {
    btn.onclick = () => performAction(btn.dataset.action);
  });

  // 记忆
  $('#memoryAddBtn').onclick = addMemory;
  $('#memoryClearBtn').onclick = clearMemory;
  $('#memoryRoleSelector').onchange = refreshMemory;

  // 统计刷新
  $('#refreshStatsBtn').onclick = refreshStats;

  // 欢迎卡片点击
  $$('.welcome-card').forEach(card => {
    card.onclick = () => {
      const prompt = card.dataset.prompt;
      $('#messageInput').value = prompt;
      sendMessage();
    };
  });
}

function sendMessage() {
  const input = $('#messageInput');
  const msg = input.value.trim();
  if (!msg || state.isStreaming) return;
  input.value = '';
  input.style.height = 'auto';
  streamChat(msg);
}

// ---------- 初始化 ----------
async function init() {
  bindEvents();
  loadMessagesFromLocal();
  // 先检查健康状态
  try {
    const health = await api('/api/health');
    console.log('CodeCrew server:', health);
  } catch (e) {
    showToast('无法连接到服务器', 'error');
  }
  await refreshAll();
  // 定期刷新统计
  setInterval(refreshStats, 10000);
}

document.addEventListener('DOMContentLoaded', init);

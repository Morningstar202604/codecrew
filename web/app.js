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

// Toast 图标映射
const TOAST_ICONS = {
  success: '✓',
  error: '✕',
  info: 'ℹ',
  warning: '⚠',
};

function showToast(msg, type = 'info', duration = 3000) {
  // 确保 toast 容器存在
  let container = $('.toast-container');
  if (!container) {
    container = document.createElement('div');
    container.className = 'toast-container';
    document.body.appendChild(container);
  }

  const toast = document.createElement('div');
  toast.className = 'toast ' + type;
  
  const icon = document.createElement('span');
  icon.className = 'toast-icon';
  icon.textContent = TOAST_ICONS[type] || TOAST_ICONS.info;
  
  const message = document.createElement('span');
  message.className = 'toast-message';
  message.textContent = msg;
  
  const closeBtn = document.createElement('span');
  closeBtn.className = 'toast-close';
  closeBtn.textContent = '×';
  closeBtn.onclick = () => dismissToast(toast);
  
  toast.appendChild(icon);
  toast.appendChild(message);
  toast.appendChild(closeBtn);
  container.appendChild(toast);
  
  // 自动消失
  const timer = setTimeout(() => dismissToast(toast), duration);
  toast.dataset.timer = timer;
  
  return toast;
}

function dismissToast(toast) {
  if (!toast || toast.classList.contains('hide')) return;
  clearTimeout(toast.dataset.timer);
  toast.classList.add('hide');
  setTimeout(() => toast.remove(), 300);
}

function formatTime(iso) {
  try {
    const d = new Date(iso);
    return d.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' }) + ' ' +
           d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
  } catch { return iso; }
}

// ---------- API 调用 ----------
// API 配置
const API_CONFIG = {
  timeout: 30000,      // 30 秒超时
  maxRetries: 2,       // 最多重试 2 次
  retryDelay: 1000,    // 重试间隔 1 秒
};

// 带超时的 fetch
function fetchWithTimeout(url, options, timeout) {
  return Promise.race([
    fetch(url, options),
    new Promise((_, reject) =>
      setTimeout(() => reject(new Error('请求超时，请检查网络连接')), timeout)
    ),
  ]);
}

async function api(path, options = {}) {
  const opts = {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  };
  if (opts.body && typeof opts.body === 'object') {
    opts.body = JSON.stringify(opts.body);
  }

  let lastError;
  for (let attempt = 0; attempt <= API_CONFIG.maxRetries; attempt++) {
    try {
      const res = await fetchWithTimeout(path, opts, API_CONFIG.timeout);
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        // 4xx 错误不重试
        if (res.status >= 400 && res.status < 500) {
          throw new Error(data.error || `请求失败 (${res.status})`);
        }
        throw new Error(data.error || `服务器错误 (${res.status})`);
      }
      return data;
    } catch (err) {
      lastError = err;
      // 超时或 5xx 错误才重试
      if (attempt < API_CONFIG.maxRetries && 
          (err.message.includes('超时') || err.message.includes('5'))) {
        await new Promise(r => setTimeout(r, API_CONFIG.retryDelay * (attempt + 1)));
        continue;
      }
      break;
    }
  }

  // 全局错误提示
  if (lastError && !options.silent) {
    showToast(lastError.message, 'error');
  }
  throw lastError;
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
  const roleName = document.createElement('span');
  roleName.textContent = role === 'user' ? '你' : state.currentRole;
  roleLabel.appendChild(roleName);
  
  // 角色徽章
  if (role !== 'user') {
    const badge = document.createElement('span');
    badge.className = 'role-badge';
    badge.textContent = getRoleDisplayName(state.currentRole);
    roleLabel.appendChild(badge);
  }
  
  // 时间戳
  const time = document.createElement('span');
  time.style.marginLeft = 'auto';
  time.style.color = 'var(--text-muted)';
  time.style.fontSize = '11px';
  time.textContent = new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
  roleLabel.appendChild(time);

  const contentEl = document.createElement('div');
  contentEl.className = 'message-content';
  if (content) renderMessageContent(contentEl, content);

  body.appendChild(roleLabel);
  body.appendChild(contentEl);
  msg.appendChild(avatar);
  msg.appendChild(body);
  messages.appendChild(msg);
  
  // 平滑滚动到底部
  requestAnimationFrame(() => scrollToBottom());
  return msg;
}

// 获取角色显示名称
function getRoleDisplayName(role) {
  const names = {
    developer: '开发',
    reviewer: '审查',
    architect: '架构',
    tester: '测试',
    docs: '文档',
  };
  return names[role] || role;
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



// 导出配置
async function exportConfig() {
  try {
    const data = await api('/api/config?session=' + state.sessionId);
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'codecrew-config.json';
    a.click();
    URL.revokeObjectURL(url);
    showToast('配置已导出', 'success');
  } catch (err) {
    // 错误已由 api 函数处理
  }
}

// 导入配置
async function importConfig() {
  const input = document.createElement('input');
  input.type = 'file';
  input.accept = '.json';
  input.onchange = async (e) => {
    const file = e.target.files[0];
    if (!file) return;
    const text = await file.text();
    try {
      JSON.parse(text);
      // 这里可以调用 API 导入配置，目前只做展示
      showToast('配置文件已读取（导入功能需后端支持）', 'info');
    } catch {
      showToast('无效的 JSON 配置文件', 'error');
    }
  };
  input.click();
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
      let allowBtn = '';
      if (t.permission === 'ask' && !t.allowed) {
        allowBtn = `<button class="tool-allow-btn" data-tool="${escapeHtml(t.name)}">放行</button>`;
      }
      item.innerHTML = `
        <span class="tool-name">${escapeHtml(t.name)}</span>
        <span class="tool-perm ${t.permission}">${t.permission}</span>
        ${allowBtn}
      `;
      item.title = t.description || '';
      list.appendChild(item);
    }
    // 绑定放行按钮
    list.querySelectorAll('.tool-allow-btn').forEach(btn => {
      btn.onclick = async (e) => {
        e.stopPropagation();
        const tool = btn.dataset.tool;
        try {
          await api('/api/permissions/allow', { method: 'POST', body: { session: state.sessionId || '', tool } });
          showToast('已放行 ' + tool, 'success');
          refreshTools();
        } catch (err) {
          showToast('放行失败: ' + err.message, 'error');
        }
      };
    });
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
    $('#statProvider').textContent = cost.provider || '-';
    if (cost.has_price) {
      $('#statCost').textContent = '$' + cost.cost_usd.toFixed(6);
      $('#statCost').style.color = 'var(--accent)';
      $('#statCost').style.fontWeight = '600';
    } else {
      $('#statCost').textContent = '未配置单价';
      $('#statCost').style.color = 'var(--text-dim)';
      $('#statCost').style.fontWeight = '400';
    }
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

  // 推理范式
  $$('.mode-btn').forEach(btn => {
    btn.onclick = () => setReasoningMode(btn.dataset.mode);
  });
  $('#clearFailuresBtn').onclick = async () => {
    await api('/api/failures?session=' + state.sessionId, { method: 'POST', body: { action: 'clear' } });
    showToast('失败经验已清空', 'success');
    refreshFailures();
  };

  // 验证
  $('#runVerifyBtn').onclick = () => runVerify(false);
  $('#runVerifyRepairBtn').onclick = () => runVerify(true);

  // 索引
  $('#buildIndexBtn').onclick = buildIndex;
  $('#codeSearchBtn').onclick = searchCode;
  $('#codeSearchInput').addEventListener('keydown', e => { if (e.key === 'Enter') searchCode(); });

  // Supervisor
  $('#supervisorOnBtn').onclick = () => supervisorAction('on');
  $('#supervisorOffBtn').onclick = () => supervisorAction('off');
  $('#assignTaskBtn').onclick = () => {
    const worker = $('#supervisorWorkerSelect').value;
    const task = $('#supervisorTaskInput').value.trim();
    if (!task) { showToast('请输入任务描述', 'error'); return; }
    supervisorAction('assign', worker, task);
    $('#supervisorTaskInput').value = '';
    showToast('任务已分配', 'success');
  };
  // 任务完成按钮（事件委托）
  document.addEventListener('click', e => {
    if (e.target.classList.contains('task-done-btn')) {
      const id = parseInt(e.target.dataset.id);
      const result = e.target.dataset.result || '已完成';
      supervisorAction('done', null, null, id, result);
    }
  });

  // 评估
  $('#runEvalBtn').onclick = runEval;

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
  input.blur(); // 移动端收起键盘
  streamChat(msg);
}

// ---------- 推理范式 ----------
async function refreshReasoning() {
  try {
    const data = await api('/api/reasoning?session=' + state.sessionId);
    $('#currentReasoningMode').textContent = data.mode;
    $('#showThoughts').textContent = data.show_thoughts;
    $('#autoReflect').textContent = data.auto_reflect;
    // 更新按钮状态
    document.querySelectorAll('.mode-btn').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.mode === data.mode);
    });
  } catch (e) { console.error('refreshReasoning:', e); }
}

async function setReasoningMode(mode) {
  try {
    await api('/api/reasoning?session=' + state.sessionId, {
      method: 'POST',
      body: { mode }
    });
    showToast('推理模式已切换为: ' + mode, 'success');
    refreshReasoning();
  } catch (e) {
    showToast('切换失败: ' + e.message, 'error');
  }
}

async function refreshFailures() {
  try {
    const data = await api('/api/failures?session=' + state.sessionId);
    const list = $('#failuresList');
    if (!data.failures || data.failures.length === 0) {
      list.innerHTML = '<div class="empty-state">暂无失败经验</div>';
      return;
    }
    list.innerHTML = data.failures.slice(0, 10).map(f => `
      <div class="failure-item">
        <div class="failure-task">${escapeHtml(f.task || '')}</div>
        <div class="failure-error">${escapeHtml(f.error || '')}</div>
        <div class="failure-time">${f.timestamp || ''}</div>
      </div>
    `).join('');
  } catch (e) { console.error('refreshFailures:', e); }
}

// ---------- 验证与自愈 ----------
async function runVerify(withRepair) {
  try {
    $('#verifyResult').innerHTML = '<div class="loading">正在验证...</div>';
    const result = await api('/api/verify?session=' + state.sessionId, {
      method: 'POST',
      body: { action: withRepair ? 'repair' : 'run' }
    });
    let html = `<div class="verify-summary ${result.passed ? 'success' : 'error'}">
      ${result.passed ? '✓' : '✗'} ${result.passed ? '全部验证通过' : result.failed_count + '/' + result.total + ' 项失败'}
    </div>`;
    if (result.commands) {
      html += '<div class="verify-commands">';
      result.commands.forEach(c => {
        html += `<div class="verify-command ${c.passed ? 'passed' : 'failed'}">
          <span>${c.passed ? '✓' : '✗'}</span>
          <span class="cmd-name">${escapeHtml(c.command)}</span>
          <span class="cmd-duration">${c.duration_ms}ms</span>
        </div>`;
        if (!c.passed && c.output) {
          html += `<pre class="cmd-output">${escapeHtml(c.output.substring(0, 500))}</pre>`;
        }
      });
      html += '</div>';
    }
    $('#verifyResult').innerHTML = html;
  } catch (e) {
    $('#verifyResult').innerHTML = '<div class="error">验证失败: ' + escapeHtml(e.message) + '</div>';
  }
}

// ---------- 代码库索引 ----------
async function refreshIndex() {
  try {
    const data = await api('/api/index?session=' + state.sessionId);
    if (data.enabled) {
      $('#indexFileCount').textContent = data.file_count;
      $('#indexSymbolCount').textContent = data.symbol_count;
      $('#indexUpdatedAt').textContent = data.updated_at ? new Date(data.updated_at).toLocaleString() : '-';
    }
  } catch (e) { console.error('refreshIndex:', e); }
}

async function buildIndex() {
  try {
    await api('/api/index/build?session=' + state.sessionId, { method: 'POST' });
    showToast('正在构建索引...', 'success');
    setTimeout(refreshIndex, 3000);
  } catch (e) {
    showToast('构建失败: ' + e.message, 'error');
  }
}

async function searchCode() {
  const query = $('#codeSearchInput').value.trim();
  if (!query) { showToast('请输入搜索关键词', 'error'); return; }
  try {
    const data = await api('/api/index/search?q=' + encodeURIComponent(query) + '&session=' + state.sessionId);
    const results = data.results || [];
    if (results.length === 0) {
      $('#codeSearchResults').innerHTML = '<div class="empty-state">没有找到匹配的结果</div>';
      return;
    }
    $('#codeSearchResults').innerHTML = results.map(r => `
      <div class="search-result-item">
        <div class="result-header">
          <span class="result-file">${escapeHtml(r.file)}</span>
          <span class="result-line">:${r.line}</span>
          <span class="result-score">相关性: ${r.score.toFixed(2)}</span>
        </div>
        <pre class="result-content">${escapeHtml(r.content)}</pre>
      </div>
    `).join('');
  } catch (e) {
    showToast('搜索失败: ' + e.message, 'error');
  }
}

// ---------- Supervisor 编排 ----------
async function refreshSupervisor() {
  try {
    const data = await api('/api/supervisor?session=' + state.sessionId);
    $('#supervisorEnabled').textContent = data.enabled ? '开启' : '关闭';
    if (data.progress) {
      $('#supervisorProgress').textContent = data.progress.done + '/' + data.progress.total;
    }
    // 任务列表
    const tasksEl = $('#supervisorTasks');
    if (data.tasks && data.tasks.length > 0) {
      tasksEl.innerHTML = data.tasks.map(t => `
        <div class="task-item ${t.status}">
          <span class="task-status">${t.status === 'done' ? '✓' : t.status === 'running' ? '▶' : '○'}</span>
          <span class="task-worker">[${t.worker}]</span>
          <span class="task-title">${escapeHtml(t.task)}</span>
          ${t.result ? `<button class="task-done-btn" data-id="${t.id}" data-result="${escapeHtml(t.result)}">完成</button>` : `<button class="task-done-btn" data-id="${t.id}">标记完成</button>`}
        </div>
      `).join('');
    } else {
      tasksEl.innerHTML = '<div class="empty-state">暂无任务</div>';
    }
  } catch (e) { console.error('refreshSupervisor:', e); }
}

async function supervisorAction(action, worker, task, id, result) {
  try {
    await api('/api/supervisor?session=' + state.sessionId, {
      method: 'POST',
      body: { action, worker, task, id, result }
    });
    refreshSupervisor();
  } catch (e) {
    showToast('操作失败: ' + e.message, 'error');
  }
}

// ---------- 评估 ----------
async function runEval() {
  try {
    $('#evalResult').innerHTML = '<div class="loading">正在运行评估...</div>';
    await api('/api/eval?session=' + state.sessionId, { method: 'POST' });
    showToast('评估正在运行，完成后刷新查看', 'success');
    setTimeout(refreshEvalReports, 10000);
  } catch (e) {
    $('#evalResult').innerHTML = '<div class="error">评估失败: ' + escapeHtml(e.message) + '</div>';
  }
}

async function refreshEvalReports() {
  try {
    const data = await api('/api/eval?session=' + state.sessionId);
    const reports = data.reports || [];
    const el = $('#evalReports');
    if (reports.length === 0) {
      el.innerHTML = '<div class="empty-state">暂无评估报告</div>';
      return;
    }
    el.innerHTML = reports.slice(0, 5).map(r => `
      <div class="eval-report-item">
        <div class="report-header">
          <span class="report-name">${escapeHtml(r.name)}</span>
          <span class="report-time">${new Date(r.started_at).toLocaleString()}</span>
        </div>
        <div class="report-stats">
          <span>通过率: ${r.pass_rate?.toFixed(1)}%</span>
          <span>得分: ${r.total_score}/${r.max_score}</span>
          <span>用时: ${r.duration_ms}ms</span>
        </div>
      </div>
    `).join('');
  } catch (e) { console.error('refreshEvalReports:', e); }
}

// ---------- 初始化 ----------
async function init() {
  bindEvents();
  setupMobileKeyboard();
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

// 移动端键盘适配
function setupMobileKeyboard() {
  const isMobile = /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini/i.test(navigator.userAgent);
  if (!isMobile) return;

  const input = $('#messageInput');
  const chatContainer = $('#chatContainer');

  // 聚焦时滚动到底部，确保输入框可见
  input.addEventListener('focus', () => {
    setTimeout(() => {
      chatContainer.scrollTop = chatContainer.scrollHeight;
      input.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }, 300);
  });

  // 监听 visualViewport 变化（键盘弹出/收起）
  if (window.visualViewport) {
    let lastHeight = window.visualViewport.height;
    window.visualViewport.addEventListener('resize', () => {
      if (window.visualViewport.height < lastHeight) {
        // 键盘弹出，滚动到底部
        setTimeout(() => {
          chatContainer.scrollTop = chatContainer.scrollHeight;
        }, 100);
      }
      lastHeight = window.visualViewport.height;
    });
  }
}

document.addEventListener('DOMContentLoaded', init);

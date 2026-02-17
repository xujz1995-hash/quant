// ===== 接口请求 =====
const API = '/api/v1';

// HTML 转义函数，防止 < > & 等符号被浏览器解析
function escapeHtml(text) {
  if (!text) return text;
  const div = document.createElement('div');
  div.textContent = text;
  return div.innerHTML;
}

async function api(method, path, body) {
  const opts = {
    method,
    headers: { 'Content-Type': 'application/json' },
  };
  if (body) opts.body = JSON.stringify(body);

  const resp = await fetch(API + path, opts);
  const data = await resp.json();
  if (!resp.ok) throw new Error(data.error || `请求失败 ${resp.status}`);
  return data;
}

// ===== 健康检查 =====
async function checkHealth() {
  const dot = document.getElementById('health-dot');
  const txt = document.getElementById('health-text');
  const badge = document.getElementById('trading-mode-badge');
  try {
    const data = await api('GET', '/health');
    dot.className = 'dot dot-on';
    txt.textContent = '服务在线';
    // 更新交易模式标识
    if (data.trading) {
      const t = data.trading;
      if (t.mode === 'futures') {
        badge.textContent = `合约 ${t.leverage}x` + (t.dry_run ? ' (模拟)' : '');
        badge.className = 'mode-badge mode-futures';
      } else {
        badge.textContent = '现货' + (t.dry_run ? ' (模拟)' : '');
        badge.className = 'mode-badge mode-spot';
      }
    }
  } catch {
    dot.className = 'dot dot-off';
    txt.textContent = '服务离线';
  }
}

// ===== 提示消息 =====
function showToast(msg, type) {
  const existing = document.querySelector('.toast');
  if (existing) existing.remove();

  const el = document.createElement('div');
  el.className = type === 'success' ? 'toast toast-success' : 'toast';
  el.textContent = msg;
  document.body.appendChild(el);
  setTimeout(() => el.remove(), 4000);
}

// ===== 辅助函数 =====
function badge(text, type) {
  return `<span class="badge badge-${type}">${text}</span>`;
}

const STATUS_MAP = {
  success: '成功',
  rejected: '已拒绝',
  failed: '失败',
  running: '运行中',
};

const SIDE_MAP = {
  long: '做多',
  short: '做空',
  close: '平仓',
  none: '无方向',
};

function statusBadge(status) {
  const map = { success: 'success', rejected: 'rejected', failed: 'failed', running: 'running' };
  return badge(STATUS_MAP[status] || status, map[status] || 'running');
}

function sideBadge(side) {
  return badge(SIDE_MAP[side] || side, side);
}

const STAGE_MAP = {
  start: '启动',
  market: '行情',
  signal: '信号',
  risk: '风控',
  execution: '执行',
  '启动': '启动',
  '行情': '行情',
  '信号': '信号',
  '风控': '风控',
  '执行': '执行',
};

function formatTime(ts) {
  if (!ts) return '-';
  const d = new Date(ts);
  return d.toLocaleTimeString('zh-CN', { hour12: false });
}

// ===== 渲染执行结果 =====
function renderResult(data, container) {
  const { cycle, signal, risk, order, logs } = data;

  // 摘要信息
  const summaryEl = container.querySelector('#result-summary') || container;
  const summaryItems = [
    { label: '周期 ID', value: cycle.id.slice(0, 8) + '…' },
    { label: '交易对', value: cycle.pair },
    { label: '状态', value: statusBadge(cycle.status) },
  ];
  if (signal) {
    summaryItems.push({ label: '信号方向', value: sideBadge(signal.side) });
    summaryItems.push({ label: '置信度', value: (signal.confidence * 100).toFixed(1) + '%' });
  }
  if (risk) {
    summaryItems.push({ label: '风控结果', value: risk.approved ? badge('通过', 'success') : badge('拒绝', 'rejected') });
  }

  if (summaryEl.id === 'result-summary') {
    summaryEl.innerHTML = summaryItems.map(i =>
      `<div class="summary-item"><div class="label">${i.label}</div><div class="value">${i.value}</div></div>`
    ).join('');
  }

  // 流水线步骤
  const pipeEl = container.querySelector('#pipeline');
  if (pipeEl) {
    let steps = '';

    // 信号步骤
    if (signal) {
      steps += `<div class="pipe-step step-success">
        <div class="step-title">信号生成</div>
        <div class="step-body">
          方向: ${sideBadge(signal.side)}<br>
          置信度: ${(signal.confidence * 100).toFixed(1)}%<br>
          <small style="color:var(--text-dim)">${escapeHtml(signal.reason) || ''}</small>
        </div>
      </div>`;
    }

    // 风控步骤
    if (risk) {
      const cls = risk.approved ? 'step-success' : 'step-reject';
      steps += `<div class="pipe-step ${cls}">
        <div class="step-title">风控评估</div>
        <div class="step-body">
          ${risk.approved ? badge('通过', 'success') : badge('拒绝: ' + risk.reject_reason, 'rejected')}<br>
          最大仓位: ${risk.max_stake_usdt} USDT
        </div>
      </div>`;
    }

    // 执行步骤
    if (order) {
      const cls = order.status === 'filled' || order.status === 'submitted' || order.status === 'simulated_filled' ? 'step-success' : 'step-fail';
      const statusText = order.status === 'simulated_filled' ? '模拟成交' : order.status;
      steps += `<div class="pipe-step ${cls}">
        <div class="step-title">下单执行</div>
        <div class="step-body">
          状态: ${statusText}<br>
          金额: ${order.stake_usdt} USDT<br>
          ${order.exchange_order_id ? '订单号: ' + order.exchange_order_id : ''}
        </div>
      </div>`;
    } else if (cycle.status === 'rejected') {
      steps += `<div class="pipe-step step-reject">
        <div class="step-title">下单执行</div>
        <div class="step-body" style="color:var(--text-dim)">风控拒绝，未执行下单</div>
      </div>`;
    }

    pipeEl.innerHTML = steps;
  }

  // 执行日志
  const logsEl = container.querySelector('#logs-list');
  if (logsEl && logs && logs.length) {
    logsEl.innerHTML = logs.map(l =>
      `<div class="log-entry">
        <span class="log-time">${formatTime(l.created_at)}</span>
        <span class="log-stage">${STAGE_MAP[l.stage] || l.stage}</span>
        <span class="log-msg">${l.message}</span>
      </div>`
    ).join('');
    container.querySelector('#logs-section').hidden = false;
  }
}

// ===== 执行交易周期 =====
document.getElementById('run-form').addEventListener('submit', async (e) => {
  e.preventDefault();

  const btn = document.getElementById('run-btn');
  const btnText = btn.querySelector('.btn-text');
  const btnLoad = btn.querySelector('.btn-loading');
  btn.disabled = true;
  btnText.hidden = true;
  btnLoad.hidden = false;

  const body = {
    pair: document.getElementById('pair').value,
    snapshot: {
      last_price:   parseFloat(document.getElementById('last_price').value) || 0,
      change_24h:   parseFloat(document.getElementById('change_24h').value) || 0,
      volume_24h:   parseFloat(document.getElementById('volume_24h').value) || 0,
      funding_rate: parseFloat(document.getElementById('funding_rate').value) || 0,
    },
    portfolio: {
      daily_pnl_usdt:     parseFloat(document.getElementById('daily_pnl').value) || 0,
      open_exposure_usdt: parseFloat(document.getElementById('open_exposure').value) || 0,
    },
  };

  try {
    const data = await api('POST', '/cycles/run', body);
    const panel = document.getElementById('result-panel');
    panel.hidden = false;
    renderResult(data, panel);
    panel.scrollIntoView({ behavior: 'smooth' });
  } catch (err) {
    showToast('执行失败: ' + err.message);
  } finally {
    btn.disabled = false;
    btnText.hidden = false;
    btnLoad.hidden = true;
  }
});

// ===== 历史周期列表（分页） =====
let cyclesCurrentPage = 1;
const CYCLES_PAGE_SIZE = 15;

async function loadCycles(page) {
  if (!page || page < 1) page = 1;
  cyclesCurrentPage = page;

  const listEl = document.getElementById('cycles-list');
  const pagEl = document.getElementById('cycles-pagination');

  try {
    const data = await api('GET', `/cycles?page=${page}&page_size=${CYCLES_PAGE_SIZE}`);
    const cycles = data.cycles || [];
    const total = data.total || 0;
    const totalPages = data.total_pages || 1;

    if (cycles.length === 0) {
      listEl.innerHTML = '<p style="color:var(--text-dim)">暂无历史周期记录</p>';
      pagEl.innerHTML = '';
      return;
    }

    const STATUS_LABEL = {
      running: '运行中', success: '成功', rejected: '已拒绝', failed: '失败',
    };
    const STATUS_CLS = {
      success: 'badge-success', rejected: 'badge-rejected', failed: 'badge-failed', running: 'badge-running',
    };

    function fmtTime(ts) {
      if (!ts) return '-';
      const d = new Date(ts);
      return d.toLocaleString('zh-CN', { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' });
    }

    function truncate(str, len) {
      if (!str) return '-';
      return str.length > len ? str.slice(0, len) + '…' : str;
    }

    let html = '<div class="positions-table"><table><thead><tr>';
    html += '<th>时间</th><th>交易对</th><th>状态</th><th>信号</th><th>置信度</th><th>风控</th><th>金额(U)</th><th>成交价</th><th>模型</th><th>Token</th><th>理由</th><th>操作</th>';
    html += '</tr></thead><tbody>';

    for (const c of cycles) {
      const sLabel = STATUS_LABEL[c.status] || c.status;
      const sCls = STATUS_CLS[c.status] || 'badge-running';
      const sideText = SIDE_MAP[c.signal_side] || c.signal_side || '-';
      const sideCls = c.signal_side === 'long' ? 'badge-long' : c.signal_side === 'close' ? 'badge-short' : c.signal_side === 'short' ? 'badge-short' : 'badge-none';

      let riskText = '-';
      if (c.risk_approved === true) {
        riskText = '<span class="badge badge-success">通过</span>';
      } else if (c.risk_approved === false) {
        riskText = '<span class="badge badge-rejected">拒绝</span>';
      }

      const fPrice = c.filled_price > 0 ? (c.filled_price >= 1 ? c.filled_price.toFixed(4) : c.filled_price.toFixed(6)) : '-';
      const stake = c.stake_usdt > 0 ? c.stake_usdt.toFixed(2) : '-';
      const reason = truncate(c.signal_reason || c.error_message || c.reject_reason, 40);
      const modelDisplay = c.model_name ? truncate(c.model_name, 15) : '-';

      html += `<tr>
        <td style="white-space:nowrap">${fmtTime(c.created_at)}</td>
        <td><strong>${c.pair}</strong></td>
        <td><span class="badge ${sCls}">${sLabel}</span></td>
        <td><span class="badge ${sideCls}">${sideText}</span></td>
        <td>${c.confidence > 0 ? (c.confidence * 100).toFixed(0) + '%' : '-'}</td>
        <td>${riskText}</td>
        <td>${stake}</td>
        <td style="font-family:monospace">${fPrice}</td>
        <td style="font-family:monospace;font-size:0.75rem;color:var(--accent)" title="${c.model_name || ''}">${modelDisplay}</td>
        <td style="font-family:monospace;font-size:0.8rem;color:var(--text-dim)">${c.total_tokens > 0 ? c.total_tokens : '-'}</td>
        <td title="${(c.signal_reason || '').replace(/"/g, '&quot;')}" style="color:var(--text-dim);font-size:0.8rem;max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${reason}</td>
        <td>
          <button class="btn-view" onclick="viewCycleDetail('${c.cycle_id}')">查看</button>
          <button class="btn-delete" onclick="deleteCycle('${c.cycle_id}')" style="margin-left:4px">删除</button>
        </td>
      </tr>`;
    }
    html += '</tbody></table></div>';
    html += `<div style="color:var(--text-dim);font-size:0.8rem;margin-top:0.5rem">共 ${total} 条记录</div>`;
    listEl.innerHTML = html;

    // 分页控件
    if (totalPages <= 1) {
      pagEl.innerHTML = '';
      return;
    }

    let pagHtml = '';
    pagHtml += `<button class="page-btn" ${page <= 1 ? 'disabled' : ''} onclick="loadCycles(${page - 1})">上一页</button>`;

    // 显示页码
    const maxShow = 7;
    let startP = Math.max(1, page - Math.floor(maxShow / 2));
    let endP = Math.min(totalPages, startP + maxShow - 1);
    if (endP - startP < maxShow - 1) startP = Math.max(1, endP - maxShow + 1);

    if (startP > 1) {
      pagHtml += `<button class="page-btn" onclick="loadCycles(1)">1</button>`;
      if (startP > 2) pagHtml += '<span class="page-ellipsis">…</span>';
    }
    for (let i = startP; i <= endP; i++) {
      pagHtml += `<button class="page-btn ${i === page ? 'page-active' : ''}" onclick="loadCycles(${i})">${i}</button>`;
    }
    if (endP < totalPages) {
      if (endP < totalPages - 1) pagHtml += '<span class="page-ellipsis">…</span>';
      pagHtml += `<button class="page-btn" onclick="loadCycles(${totalPages})">${totalPages}</button>`;
    }

    pagHtml += `<button class="page-btn" ${page >= totalPages ? 'disabled' : ''} onclick="loadCycles(${page + 1})">下一页</button>`;
    pagEl.innerHTML = pagHtml;

  } catch (err) {
    listEl.innerHTML = `<p style="color:var(--red)">加载失败: ${err.message}</p>`;
    pagEl.innerHTML = '';
  }
}

document.getElementById('refresh-cycles').addEventListener('click', () => loadCycles(cyclesCurrentPage));

// ===== 周期详情弹窗 =====
function closeCycleModal() {
  document.getElementById('cycle-modal').classList.remove('modal-open');
}

// 点击遮罩关闭
document.getElementById('cycle-modal').addEventListener('click', (e) => {
  if (e.target === e.currentTarget) closeCycleModal();
});

// ESC 关闭
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') closeCycleModal();
});

async function viewCycleDetail(cycleId) {
  const modal = document.getElementById('cycle-modal');
  const body = document.getElementById('cycle-modal-body');
  modal.classList.add('modal-open');
  body.innerHTML = '<p style="color:var(--text-dim);text-align:center;padding:2rem 0">加载中...</p>';

  try {
    const data = await api('GET', '/cycles/' + encodeURIComponent(cycleId));
    const { cycle, signal, risk, position_strategy, order, logs } = data;

    const STATUS_LABEL = { running: '运行中', success: '成功', rejected: '已拒绝', failed: '失败' };
    const STATUS_CLS = { success: 'badge-success', rejected: 'badge-rejected', failed: 'badge-failed', running: 'badge-running' };

    function fmtFullTime(ts) {
      if (!ts) return '-';
      return new Date(ts).toLocaleString('zh-CN', { hour12: false });
    }

    function fmtPrice(p) {
      if (!p || p <= 0) return '-';
      if (p >= 100) return p.toFixed(2);
      if (p >= 1) return p.toFixed(4);
      return p.toFixed(6);
    }

    let html = '';

    // 周期基本信息
    html += `<div class="detail-section">
      <div class="detail-section-title">周期信息</div>
      <div class="detail-grid">
        <div class="detail-item"><span class="detail-label">周期 ID</span><span class="detail-value" style="font-family:monospace;font-size:0.8rem">${cycle.id}</span></div>
        <div class="detail-item"><span class="detail-label">交易对</span><span class="detail-value"><strong>${cycle.pair}</strong></span></div>
        <div class="detail-item"><span class="detail-label">状态</span><span class="detail-value"><span class="badge ${STATUS_CLS[cycle.status] || ''}">${STATUS_LABEL[cycle.status] || cycle.status}</span></span></div>
        <div class="detail-item"><span class="detail-label">创建时间</span><span class="detail-value">${fmtFullTime(cycle.created_at)}</span></div>
        <div class="detail-item"><span class="detail-label">更新时间</span><span class="detail-value">${fmtFullTime(cycle.updated_at)}</span></div>
        ${cycle.error_message ? `<div class="detail-item" style="grid-column:1/-1"><span class="detail-label">错误信息</span><span class="detail-value" style="color:var(--red)">${cycle.error_message}</span></div>` : ''}
      </div>
    </div>`;

    // 信号详情
    if (signal) {
      const sideText = SIDE_MAP[signal.side] || signal.side;
      const sideCls = signal.side === 'long' ? 'badge-long' : signal.side === 'close' ? 'badge-short' : signal.side === 'short' ? 'badge-short' : 'badge-none';

      html += `<div class="detail-section">
        <div class="detail-section-title">信号生成</div>
        <div class="detail-grid">
          <div class="detail-item"><span class="detail-label">方向</span><span class="detail-value"><span class="badge ${sideCls}">${sideText}</span></span></div>
          <div class="detail-item"><span class="detail-label">置信度</span><span class="detail-value">${(signal.confidence * 100).toFixed(1)}%</span></div>
          <div class="detail-item"><span class="detail-label">有效期</span><span class="detail-value">${signal.ttl_seconds}s</span></div>
          <div class="detail-item"><span class="detail-label">生成时间</span><span class="detail-value">${fmtFullTime(signal.created_at)}</span></div>
          ${signal.model_name ? `
          <div class="detail-item"><span class="detail-label">模型</span><span class="detail-value" style="font-family:monospace;color:var(--accent)">${signal.model_name}</span></div>
          ` : ''}
          ${signal.total_tokens > 0 ? `
          <div class="detail-item"><span class="detail-label">Token 消耗</span><span class="detail-value" style="font-family:monospace"><span style="color:var(--accent)">${signal.prompt_tokens}</span> + <span style="color:var(--green)">${signal.completion_tokens}</span> = <strong>${signal.total_tokens}</strong></span></div>
          ` : ''}
        </div>
        ${signal.thinking ? `<div style="margin-top:0.5rem">
          <span class="detail-label">AI 思维链（完整分析过程）</span>
          <div class="detail-reason detail-thinking">${escapeHtml(signal.thinking)}</div>
        </div>` : ''}
        <div style="margin-top:0.5rem">
          <span class="detail-label">决策摘要</span>
          <div class="detail-reason">${escapeHtml(signal.reason) || '-'}</div>
        </div>
      </div>`;
    }

    // 风控详情
    if (risk) {
      html += `<div class="detail-section">
        <div class="detail-section-title">风控评估</div>
        <div class="detail-grid">
          <div class="detail-item"><span class="detail-label">结果</span><span class="detail-value">${risk.approved ? '<span class="badge badge-success">通过</span>' : '<span class="badge badge-rejected">拒绝</span>'}</span></div>
          <div class="detail-item"><span class="detail-label">最大仓位</span><span class="detail-value">${risk.max_stake_usdt} USDT</span></div>
          <div class="detail-item"><span class="detail-label">评估时间</span><span class="detail-value">${fmtFullTime(risk.created_at)}</span></div>
          ${risk.reject_reason ? `<div class="detail-item" style="grid-column:1/-1"><span class="detail-label">拒绝原因</span><span class="detail-value" style="color:var(--red)">${risk.reject_reason}</span></div>` : ''}
        </div>
      </div>`;
    }

    // 建仓策略详情
    if (data.position_strategy) {
      const ps = data.position_strategy;
      const STRATEGY_MAP = { full: '全仓', pyramid: '金字塔', grid: '网格' };
      const strategyName = STRATEGY_MAP[ps.strategy] || ps.strategy;
      const strategyColor = ps.strategy === 'full' ? 'var(--accent)' : ps.strategy === 'pyramid' ? 'var(--green)' : 'var(--blue)';
      
      html += `<div class="detail-section">
        <div class="detail-section-title">建仓策略 📊</div>
        <div class="detail-grid">
          <div class="detail-item"><span class="detail-label">策略类型</span><span class="detail-value"><strong style="color:${strategyColor}">${strategyName}</strong></span></div>
          <div class="detail-item"><span class="detail-label">总金额</span><span class="detail-value">${ps.total_amount} USDT</span></div>
          <div class="detail-item"><span class="detail-label">分批数</span><span class="detail-value">${ps.entry_levels} 批</span></div>
          <div class="detail-item"><span class="detail-label">止盈</span><span class="detail-value" style="color:var(--green)">+${ps.take_profit_percent}%</span></div>
          <div class="detail-item"><span class="detail-label">止损</span><span class="detail-value" style="color:var(--red)">-${ps.stop_loss_percent}%</span></div>
          <div class="detail-item"><span class="detail-label">生成时间</span><span class="detail-value">${fmtFullTime(ps.created_at)}</span></div>
        </div>`;
      
      // 策略说明
      if (ps.reason) {
        html += `<div style="margin-top:0.5rem">
          <span class="detail-label">策略说明</span>
          <div class="detail-reason">${escapeHtml(ps.reason)}</div>
        </div>`;
      }
      
      // 批次列表
      if (ps.batches && ps.batches.length > 0) {
        html += `<div style="margin-top:1rem">
          <span class="detail-label">批次计划 (${ps.batches.length})</span>
          <div style="margin-top:0.5rem;border:1px solid var(--border);border-radius:8px;overflow:hidden">
            <table style="width:100%;border-collapse:collapse">
              <thead style="background:var(--bg-secondary)">
                <tr>
                  <th style="padding:0.5rem;text-align:left;font-size:0.85rem">批次</th>
                  <th style="padding:0.5rem;text-align:right;font-size:0.85rem">金额 (USDT)</th>
                  <th style="padding:0.5rem;text-align:right;font-size:0.85rem">占比</th>
                  <th style="padding:0.5rem;text-align:right;font-size:0.85rem">触发价</th>
                  <th style="padding:0.5rem;text-align:center;font-size:0.85rem">状态</th>
                  <th style="padding:0.5rem;text-align:right;font-size:0.85rem">执行价</th>
                  <th style="padding:0.5rem;text-align:right;font-size:0.85rem">数量</th>
                </tr>
              </thead>
              <tbody>`;
        
        for (const batch of ps.batches) {
          const statusMap = { pending: '待执行', executed: '已执行', cancelled: '已取消' };
          const statusColorMap = { pending: 'var(--text-dim)', executed: 'var(--green)', cancelled: 'var(--red)' };
          const statusText = statusMap[batch.status] || batch.status;
          const statusColor = statusColorMap[batch.status] || 'var(--text)';
          
          html += `<tr style="border-top:1px solid var(--border)">
            <td style="padding:0.5rem"><strong>第 ${batch.batch_no} 批</strong></td>
            <td style="padding:0.5rem;text-align:right;font-family:monospace">${batch.amount.toFixed(2)}</td>
            <td style="padding:0.5rem;text-align:right;color:var(--text-dim)">${batch.percentage.toFixed(1)}%</td>
            <td style="padding:0.5rem;text-align:right;font-family:monospace">${fmtPrice(batch.trigger_price)}</td>
            <td style="padding:0.5rem;text-align:center"><span style="color:${statusColor};font-weight:500">${statusText}</span></td>
            <td style="padding:0.5rem;text-align:right;font-family:monospace">${batch.executed_price > 0 ? fmtPrice(batch.executed_price) : '-'}</td>
            <td style="padding:0.5rem;text-align:right;font-family:monospace">${batch.executed_qty > 0 ? batch.executed_qty.toFixed(4) : '-'}</td>
          </tr>`;
        }
        
        html += `</tbody></table></div></div>`;
      }
      
      html += `</div>`;
    }

    // 订单详情
    if (order) {
      const orderStatusText = order.status === 'simulated_filled' ? '模拟成交' : order.status === 'filled' ? '已成交' : order.status === 'submitted' ? '已提交' : order.status;
      html += `<div class="detail-section">
        <div class="detail-section-title">订单执行</div>
        <div class="detail-grid">
          <div class="detail-item"><span class="detail-label">状态</span><span class="detail-value">${orderStatusText}</span></div>
          <div class="detail-item"><span class="detail-label">方向</span><span class="detail-value"><span class="badge ${order.side === 'long' ? 'badge-long' : 'badge-short'}">${SIDE_MAP[order.side] || order.side}</span></span></div>
          <div class="detail-item"><span class="detail-label">金额</span><span class="detail-value">${order.stake_usdt} USDT</span></div>
          <div class="detail-item"><span class="detail-label">成交价</span><span class="detail-value" style="font-family:monospace">${fmtPrice(order.filled_price)}</span></div>
          <div class="detail-item"><span class="detail-label">成交数量</span><span class="detail-value" style="font-family:monospace">${order.filled_qty > 0 ? order.filled_qty : '-'}</span></div>
          <div class="detail-item"><span class="detail-label">订单号</span><span class="detail-value" style="font-size:0.8rem;font-family:monospace">${order.exchange_order_id || order.client_order_id || '-'}</span></div>
          <div class="detail-item"><span class="detail-label">创建时间</span><span class="detail-value">${fmtFullTime(order.created_at)}</span></div>
        </div>
      </div>`;
    }

    // 执行日志
    if (logs && logs.length > 0) {
      const STAGE_LABEL = { start: '启动', market: '行情', signal: '信号', risk: '风控', execution: '执行', '启动':'启动', '行情':'行情', '信号':'信号', '风控':'风控', '执行':'执行' };
      html += `<div class="detail-section">
        <div class="detail-section-title">执行日志 (${logs.length})</div>
        <div class="detail-logs">`;
      for (const l of logs) {
        const logTime = fmtFullTime(l.created_at);
        const stage = STAGE_LABEL[l.stage] || l.stage;
        html += `<div class="detail-log-entry">
          <span class="detail-log-time">${logTime}</span>
          <span class="detail-log-stage">${stage}</span>
          <span class="detail-log-msg">${l.message}</span>
        </div>`;
      }
      html += '</div></div>';
    }

    body.innerHTML = html;
  } catch (err) {
    body.innerHTML = `<p style="color:var(--red);text-align:center;padding:2rem 0">加载失败: ${err.message}</p>`;
  }
}

// ===== 账户余额 =====
async function loadBalance() {
  const summaryEl = document.getElementById('balance-summary');
  const detailEl = document.getElementById('balance-detail');
  try {
    const data = await api('GET', '/balance');
    const usdtFree = data.usdt_free || 0;
    const usdtLocked = data.usdt_locked || 0;
    const usdtTotal = data.usdt_total || 0;
    const assets = data.assets || [];

    summaryEl.innerHTML = `
      <div class="holdings-stat">
        <div class="stat-label">USDT 可用</div>
        <div class="stat-value" style="color:var(--green)">${usdtFree.toFixed(4)} U</div>
      </div>
      <div class="holdings-stat">
        <div class="stat-label">USDT 冻结</div>
        <div class="stat-value">${usdtLocked.toFixed(4)} U</div>
      </div>
      <div class="holdings-stat">
        <div class="stat-label">USDT 总计</div>
        <div class="stat-value" style="font-weight:700">${usdtTotal.toFixed(4)} U</div>
      </div>
    `;

    // 其他币种资产明细
    const others = assets.filter(a => a.symbol !== 'USDT');
    if (others.length > 0) {
      let html = '<details style="margin-top:0.5rem"><summary style="cursor:pointer;color:var(--text-dim);font-size:0.85rem">其他币种资产 (' + others.length + ')</summary>';
      html += '<div class="holdings-table" style="margin-top:0.5rem"><table><thead><tr><th>币种</th><th>可用</th><th>冻结</th><th>总计</th></tr></thead><tbody>';
      for (const a of others) {
        const fmtVal = (v) => v >= 1 ? v.toFixed(4) : v >= 0.0001 ? v.toFixed(6) : v.toFixed(8);
        html += `<tr>
          <td><strong>${a.symbol}</strong></td>
          <td style="font-family:monospace">${fmtVal(a.free)}</td>
          <td style="font-family:monospace">${fmtVal(a.locked)}</td>
          <td style="font-family:monospace">${fmtVal(a.total)}</td>
        </tr>`;
      }
      html += '</tbody></table></div></details>';
      detailEl.innerHTML = html;
    } else {
      detailEl.innerHTML = '';
    }
  } catch (err) {
    summaryEl.innerHTML = `<p style="color:var(--red)">加载失败: ${err.message}</p>`;
    detailEl.innerHTML = '';
  }
}

// 同步余额
document.getElementById('sync-balance').addEventListener('click', async () => {
  const btn = document.getElementById('sync-balance');
  btn.disabled = true;
  btn.textContent = '同步中...';
  try {
    await loadBalance();
    showToast('余额已刷新', 'success');
  } finally {
    btn.disabled = false;
    btn.textContent = '从币安同步';
  }
});

document.getElementById('refresh-balance').addEventListener('click', loadBalance);

// ===== 持仓汇总 =====
async function loadHoldings() {
  const summaryEl = document.getElementById('holdings-summary');
  const listEl = document.getElementById('holdings-list');
  try {
    const data = await api('GET', '/holdings');
    const holdings = data.holdings || [];

    // 汇总指标
    const totalCost = data.total_cost || 0;
    const totalValue = data.total_value || 0;
    const totalPnL = data.total_pnl || 0;
    const pnlPct = data.pnl_percent || 0;

    const pnlClass = totalPnL > 0 ? 'positive' : totalPnL < 0 ? 'negative' : '';
    const pnlSign = totalPnL >= 0 ? '+' : '';

    summaryEl.innerHTML = `
      <div class="holdings-stat">
        <div class="stat-label">总成本</div>
        <div class="stat-value">${totalCost.toFixed(2)} U</div>
      </div>
      <div class="holdings-stat">
        <div class="stat-label">当前市值</div>
        <div class="stat-value">${totalValue.toFixed(2)} U</div>
      </div>
      <div class="holdings-stat">
        <div class="stat-label">未实现盈亏</div>
        <div class="stat-value ${pnlClass}">${pnlSign}${totalPnL.toFixed(2)} U</div>
      </div>
      <div class="holdings-stat">
        <div class="stat-label">盈亏比例</div>
        <div class="stat-value ${pnlClass}">${pnlSign}${pnlPct.toFixed(2)}%</div>
      </div>
    `;

    if (holdings.length === 0) {
      listEl.innerHTML = '<p style="color:var(--text-dim)">暂无持仓，等待首笔交易或点击"同步"从交易所获取</p>';
      return;
    }

    // 智能价格格式化
    function fmtPrice(price) {
      if (!price || price <= 0) return '-';
      if (price >= 100) return price.toFixed(2);
      if (price >= 1) return price.toFixed(4);
      if (price >= 0.01) return price.toFixed(6);
      return price.toFixed(8);
    }

    function fmtQty(qty) {
      if (!qty || qty <= 0) return '-';
      if (qty >= 10000) return qty.toFixed(1);
      if (qty >= 1) return qty.toFixed(4);
      return qty.toFixed(6);
    }

    let html = '<div class="holdings-table"><table><thead><tr>';
    html += '<th>币种</th><th>持有数量</th><th>均价</th><th>现价</th><th>成本(U)</th><th>市值(U)</th><th>盈亏(U)</th><th>盈亏%</th><th>来源</th>';
    html += '</tr></thead><tbody>';

    for (const h of holdings) {
      const pnl = h.unrealized_pnl || 0;
      const pct = h.pnl_percent || 0;
      const pnlCls = pnl > 0 ? 'pnl-positive' : pnl < 0 ? 'pnl-negative' : 'pnl-zero';
      const sign = pnl >= 0 ? '+' : '';
      const sourceText = h.source === 'exchange' ? '交易所' : '本地';

      html += `<tr>
        <td><strong>${h.symbol}</strong></td>
        <td style="font-family:monospace">${fmtQty(h.quantity)}</td>
        <td style="font-family:monospace">${fmtPrice(h.avg_price)}</td>
        <td style="font-family:monospace">${fmtPrice(h.current_price)}</td>
        <td>${h.total_cost.toFixed(2)}</td>
        <td>${(h.market_value || 0).toFixed(2)}</td>
        <td class="${pnlCls}">${sign}${pnl.toFixed(2)}</td>
        <td class="${pnlCls}">${sign}${pct.toFixed(2)}%</td>
        <td style="color:var(--text-dim);font-size:0.8rem">${sourceText}</td>
      </tr>`;
    }
    html += '</tbody></table></div>';
    listEl.innerHTML = html;
  } catch (err) {
    summaryEl.innerHTML = '';
    listEl.innerHTML = `<p style="color:var(--red)">加载失败: ${err.message}</p>`;
  }
}

// 从币安同步持仓
document.getElementById('sync-exchange').addEventListener('click', async () => {
  const btn = document.getElementById('sync-exchange');
  btn.disabled = true;
  btn.textContent = '同步中...';
  try {
    await api('POST', '/holdings/sync?source=exchange');
    showToast('已从币安同步持仓', 'success');
    await loadHoldings();
    await loadPositions();
  } catch (err) {
    showToast('同步失败: ' + err.message);
  } finally {
    btn.disabled = false;
    btn.textContent = '从币安同步';
  }
});

// 清空所有数据
document.getElementById('reset-data').addEventListener('click', async () => {
  if (!confirm('确认清空所有数据？\n\n包括：交易记录、持仓、信号、风控记录等\n此操作不可恢复！')) return;

  const btn = document.getElementById('reset-data');
  btn.disabled = true;
  btn.textContent = '清空中...';
  try {
    await api('POST', '/data/reset');
    showToast('所有数据已清空', 'success');
    await loadHoldings();
    await loadPositions();
  } catch (err) {
    showToast('清空失败: ' + err.message);
  } finally {
    btn.disabled = false;
    btn.textContent = '清空数据';
  }
});

document.getElementById('refresh-holdings').addEventListener('click', loadHoldings);

// ===== 交易记录列表 =====
async function loadPositions() {
  const container = document.getElementById('positions-list');
  try {
    const data = await api('GET', '/positions?limit=20');
    if (!data.positions || data.positions.length === 0) {
      container.innerHTML = '<p style="color:var(--text-dim)">暂无仓位记录</p>';
      return;
    }

    const ORDER_STATUS = {
      simulated_filled: '模拟成交',
      submitted: '已提交',
      filled: '已成交',
      rejected: '已拒绝',
      created: '已创建',
    };

    // 智能价格格式化：根据价格大小自动调整小数位
    function formatPrice(price) {
      if (!price || price <= 0) return '-';
      if (price >= 100) return price.toFixed(2);
      if (price >= 1) return price.toFixed(4);
      if (price >= 0.01) return price.toFixed(6);
      return price.toFixed(8);
    }

    // 数量格式化
    function formatQty(qty, price) {
      if (!qty || qty <= 0) {
        // 兜底：用金额/价格计算
        if (price > 0) return '-';
        return '-';
      }
      if (qty >= 1000) return qty.toFixed(2);
      if (qty >= 1) return qty.toFixed(4);
      return qty.toFixed(6);
    }

    let html = '<div class="positions-table"><table><thead><tr>';
    html += '<th>时间</th><th>交易对</th><th>方向</th><th>金额(USDT)</th><th>成交价</th><th>数量</th><th>置信度</th><th>状态</th><th>理由</th>';
    html += '</tr></thead><tbody>';

    for (const p of data.positions) {
      const sideClass = p.side === 'long' ? 'badge-long' : p.side === 'close' ? 'badge-short' : p.side === 'short' ? 'badge-short' : 'badge-none';
      const sideText = SIDE_MAP[p.side] || p.side;
      const statusText = ORDER_STATUS[p.status] || p.status;
      const time = formatTime(p.created_at);
      const reason = (p.signal_reason || '').length > 50 ? p.signal_reason.slice(0, 50) + '…' : (p.signal_reason || '-');

      // 从 coin pair 中提取币种名称（如 DOGE/USDT → DOGE）
      const coin = p.pair ? p.pair.split('/')[0] : '';
      const qty = p.filled_qty || 0;

      html += `<tr>
        <td>${time}</td>
        <td><strong>${p.pair}</strong></td>
        <td><span class="badge ${sideClass}">${sideText}</span></td>
        <td>${p.stake_usdt.toFixed(2)}</td>
        <td style="font-family:monospace">${formatPrice(p.filled_price)}</td>
        <td style="font-family:monospace">${qty > 0 ? formatQty(qty) + ' ' + coin : '-'}</td>
        <td>${(p.confidence * 100).toFixed(0)}%</td>
        <td>${statusText}</td>
        <td title="${(p.signal_reason || '').replace(/"/g, '&quot;')}" style="color:var(--text-dim);font-size:0.8rem;max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${reason}</td>
      </tr>`;
    }
    html += '</tbody></table></div>';
    container.innerHTML = html;
  } catch (err) {
    container.innerHTML = `<p style="color:var(--red)">加载失败: ${err.message}</p>`;
  }
}

document.getElementById('refresh-positions').addEventListener('click', loadPositions);

// 从币安同步交易记录
document.getElementById('sync-trades').addEventListener('click', async () => {
  const btn = document.getElementById('sync-trades');
  btn.disabled = true;
  btn.textContent = '同步中...';
  try {
    const data = await api('POST', '/trades/sync?pair=DOGE/USDT');
    showToast(`同步完成，新导入 ${data.imported} 笔交易`, 'success');
    await loadPositions();
    await loadHoldings();
  } catch (err) {
    showToast('同步失败: ' + err.message);
  } finally {
    btn.disabled = false;
    btn.textContent = '从币安同步';
  }
});

// ===== 删除周期 =====
async function deleteCycle(cycleId) {
  if (!confirm('确定要删除这个周期记录吗？此操作不可恢复。')) {
    return;
  }

  try {
    await api('DELETE', `/cycles/${cycleId}`);
    showToast('删除成功');
    loadCycles(cyclesCurrentPage); // 刷新列表
  } catch (err) {
    showToast('删除失败: ' + err.message);
  }
}

// ===== 初始化 =====
checkHealth();
loadBalance();
loadHoldings();
loadPositions();
loadCycles(1);
setInterval(checkHealth, 15000);
setInterval(loadBalance, 60000);   // 每分钟自动刷新余额
setInterval(loadHoldings, 60000);  // 每分钟自动刷新持仓
setInterval(() => loadCycles(cyclesCurrentPage), 60000); // 每分钟自动刷新周期列表
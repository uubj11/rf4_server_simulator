// RF4 管理后台 · 核心：DOM 构建、请求、路由、提示、弹窗、表单模型。
// 页面模块只依赖这里导出的东西，不直接碰全局状态。

// ---------- DOM ----------
export function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  if (attrs) {
    for (const [k, v] of Object.entries(attrs)) {
      if (v === undefined || v === null || v === false) continue;
      if (k === 'class') el.className = v;
      else if (k === 'text') el.textContent = v;
      else if (k === 'html') el.innerHTML = v;
      else if (k === 'style' && typeof v === 'object') Object.assign(el.style, v);
      else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2).toLowerCase(), v);
      else if (k === 'dataset') Object.assign(el.dataset, v);
      else if (v === true) el.setAttribute(k, '');
      else el.setAttribute(k, v);
    }
  }
  append(el, children);
  return el;
}
function append(el, children) {
  for (const c of children) {
    if (c === null || c === undefined || c === false) continue;
    if (Array.isArray(c)) append(el, c);
    else el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
}
export const $ = (sel, root = document) => root.querySelector(sel);
export const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];
export function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
export function svg(paths, size) {
  const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  s.setAttribute('viewBox', '0 0 24 24');
  s.setAttribute('fill', 'none');
  s.setAttribute('stroke', 'currentColor');
  s.setAttribute('stroke-width', '2');
  s.setAttribute('stroke-linecap', 'round');
  s.setAttribute('stroke-linejoin', 'round');
  if (size) { s.setAttribute('width', size); s.setAttribute('height', size); }
  s.innerHTML = paths;
  return s;
}
export const ICONS = {
  home: '<path d="M3 10.5 12 3l9 7.5"/><path d="M5 9.5V21h5v-6h4v6h5V9.5"/>',
  users: '<circle cx="9" cy="8" r="3.5"/><path d="M2.5 20c.8-3 3.4-4.5 6.5-4.5s5.7 1.5 6.5 4.5"/><circle cx="17" cy="9" r="2.5"/><path d="M17.5 15.6c2 .4 3.5 1.6 4 3.9"/>',
  mail: '<rect x="2.5" y="5" width="19" height="14" rx="2"/><path d="m3 7 9 6 9-6"/>',
  fish: '<path d="M6.5 12c1.2-3.3 4.3-5.5 7.5-5.5 3.5 0 6 2.5 7 5.5-1 3-3.5 5.5-7 5.5-3.2 0-6.3-2.2-7.5-5.5Z"/><path d="M17.5 11v.01"/><path d="M6.5 12 3 9v6l3.5-3Z"/>',
  water: '<path d="M2 15c2 0 2-1.5 4-1.5S8 15 10 15s2-1.5 4-1.5 2 1.5 4 1.5 2-1.5 4-1.5"/><path d="M2 20c2 0 2-1.5 4-1.5S8 20 10 20s2-1.5 4-1.5 2 1.5 4 1.5 2-1.5 4-1.5"/><path d="M12 3v7M9 7l3 3 3-3"/>',
  bait: '<path d="M12 3c3 4 5.5 7.4 5.5 10a5.5 5.5 0 0 1-11 0C6.5 10.4 9 7 12 3Z"/>',
  hook: '<path d="M15 3v10a6 6 0 1 1-12 0V9"/><path d="M15 3l3 3-3 3"/>',
  sliders: '<path d="M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1.5 14h5M9.5 8h5M17.5 16h5"/>',
  store: '<path d="M3 9h18l-1.5-5h-15L3 9Z"/><path d="M4 9v11h16V9"/><path d="M9 20v-6h6v6"/>',
  list: '<path d="M8 6h13M8 12h13M8 18h13M3.5 6h.01M3.5 12h.01M3.5 18h.01"/>',
  doc: '<path d="M6 3h9l4 4v14H6z"/><path d="M9 11h7M9 15h7M9 19h4"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 7.5h.01"/>',
  rules: '<path d="M9 11 11 13 15 9"/><rect x="3" y="4" width="18" height="16" rx="2"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>',
  refresh: '<path d="M21 12a9 9 0 1 1-2.6-6.4L21 8"/><path d="M21 3v5h-5"/>',
  logout: '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5"/><path d="M21 12H9"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  x: '<path d="M18 6 6 18M6 6l12 12"/>',
  check: '<path d="M20 6 9 17l-5-5"/>',
  chevron: '<path d="m9 6 6 6-6 6"/>',
  back: '<path d="m15 6-6 6 6 6"/>',
  alert: '<path d="M12 9v4M12 17h.01"/><path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z"/>',
  power: '<path d="M12 2v10"/><path d="M18.4 6.6a9 9 0 1 1-12.8 0"/>',
  trash: '<path d="M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6"/>',
  edit: '<path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 1 1 3 3L7 19l-4 1 1-4Z"/>',
  external: '<path d="M14 4h6v6M20 4l-9 9"/><path d="M18 14v6H4V6h6"/>',
  copy: '<rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h8"/>',
  logo: '<path d="M5.5 12c1.3-3.2 4.2-5.2 7.3-5.2 3.4 0 5.9 2.3 6.9 5.2-1 2.9-3.5 5.2-6.9 5.2-3.1 0-6-2-7.3-5.2Z"/><path d="M5.5 12 2.5 9.2v5.6L5.5 12Z"/><path d="M16 11.2v.01"/>',
};
export const icon = (name, size) => svg(ICONS[name] || '', size);

// ---------- 请求 ----------
// 后台可能挂在 /admin/（launcher 统一入口）或根路径（独立运行），API 前缀随之变化。
export const BASE = location.pathname.replace(/\/[^/]*$/, '').replace(/\/+$/, '').endsWith('/admin') ? '/admin' : '';
let token = '';
try { token = localStorage.getItem('rf4_admin_token') || ''; } catch (e) {}
const authListeners = [];
export function getToken() { return token; }
export function setToken(t) {
  token = t || '';
  try { t ? localStorage.setItem('rf4_admin_token', t) : localStorage.removeItem('rf4_admin_token'); } catch (e) {}
  if (!token) cleanupPage();
  authListeners.forEach((fn) => fn(!!token));
}
export function onAuth(fn) { authListeners.push(fn); }
export function webURL(p) { return BASE + p; }

export async function api(path, opt = {}) {
  const r = await fetch(BASE + path, {
    ...opt,
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token, ...(opt.headers || {}) },
  });
  const text = await r.text();
  let data = {};
  try { data = text ? JSON.parse(text) : {}; } catch (e) { data = { raw: text }; }
  if (!r.ok) {
    if (r.status === 401) setToken('');
    const err = new Error(data.error || ('请求失败（HTTP ' + r.status + '）'));
    err.status = r.status;
    err.data = data;
    throw err;
  }
  return data;
}
export const post = (path, body) => api(path, { method: 'POST', body: JSON.stringify(body ?? {}) });

// 列表接口统一兜底：后端历史版本可能把空切片编码成 null。
export async function apiList(path, opt = {}) {
  const value = await api(path, opt);
  return Array.isArray(value) ? value : [];
}

// ---------- 格式化 ----------
export const fmt = (n) => (n === null || n === undefined || n === '' ? '—' : Number(n).toLocaleString('zh-CN'));
export function timeText(ts) {
  const n = Number(ts);
  if (!n) return '—';
  return new Date(n * 1000).toLocaleString('zh-CN', { hour12: false });
}
export function relTime(ts) {
  const t = Number(ts) * 1000;
  if (!t) return '—';
  const d = (Date.now() - t) / 1000;
  if (d < 60) return '刚刚';
  if (d < 3600) return Math.floor(d / 60) + ' 分钟前';
  if (d < 86400) return Math.floor(d / 3600) + ' 小时前';
  if (d < 86400 * 7) return Math.floor(d / 86400) + ' 天前';
  return timeText(ts);
}

// ---------- 提示 ----------
export function toast(msg, type = 'ok') {
  let box = $('.toasts');
  if (!box) { box = h('div', { class: 'toasts' }); document.body.append(box); }
  const t = h('div', { class: 'toast ' + type, text: msg });
  box.append(t);
  setTimeout(() => { t.style.opacity = '0'; setTimeout(() => t.remove(), 220); }, type === 'bad' ? 5000 : 2800);
}
export function errText(e) { return (e && e.message) || String(e); }
export async function guard(fn, okMsg) {
  try { const r = await fn(); if (okMsg) toast(okMsg); return r; }
  catch (e) { toast(errText(e), 'bad'); throw e; }
}

// ---------- 弹窗 / 抽屉 ----------
export function dialog({ title, desc, body, actions = [], size = '', onClose } = {}) {
  const close = () => { overlay.remove(); document.removeEventListener('keydown', onKey); onClose && onClose(); };
  const onKey = (e) => { if (e.key === 'Escape') close(); };
  const foot = h('div', { class: 'dialog-foot' }, actions.map((a) => h('button', {
    class: 'btn ' + (a.kind || ''), disabled: a.disabled,
    onclick: async (e) => {
      if (!a.onClick) return close();
      e.currentTarget.disabled = true;
      try { const keep = await a.onClick(close); if (keep !== true) close(); }
      catch (err) { toast(errText(err), 'bad'); }
      finally { if (e.currentTarget) e.currentTarget.disabled = false; }
    },
  }, a.label)));
  const box = h('div', { class: 'dialog ' + size, role: 'dialog', 'aria-modal': 'true' },
    h('div', { class: 'dialog-head' }, h('div', { class: 'grow' }, h('h3', { text: title }), desc ? h('p', { text: desc }) : null),
      h('button', { class: 'btn ghost icon sm', 'aria-label': '关闭', onclick: close }, icon('x'))),
    body ? h('div', { class: 'dialog-body' }, body) : null,
    actions.length ? foot : null);
  const overlay = h('div', { class: 'overlay', onmousedown: (e) => { if (e.target === overlay) close(); } }, box);
  document.body.append(overlay);
  document.addEventListener('keydown', onKey);
  setTimeout(() => { const f = box.querySelector('input,select,textarea'); if (f) f.focus(); }, 30);
  return { close, box };
}
export function confirmDialog({ title, desc, confirm = '确定', danger = false }) {
  return new Promise((resolve) => {
    let done = false;
    dialog({
      title, desc,
      onClose: () => { if (!done) resolve(false); },
      actions: [
        { label: '取消', kind: 'ghost' },
        { label: confirm, kind: danger ? 'danger solid' : 'primary', onClick: () => { done = true; resolve(true); } },
      ],
    });
  });
}
export function drawer({ title, sub, body, foot, onClose, width } = {}) {
  const close = () => { overlay.remove(); document.removeEventListener('keydown', onKey); onClose && onClose(); };
  const onKey = (e) => { if (e.key === 'Escape' && !document.querySelector('.overlay:not(.drawer-overlay)')) close(); };
  const panel = h('div', { class: 'drawer', style: width ? { width } : null },
    h('div', { class: 'drawer-head' }, h('div', { class: 'grow' }, h('h3', { text: title }), sub ? h('div', { class: 'muted small', text: sub }) : null),
      h('button', { class: 'btn ghost icon sm', 'aria-label': '关闭', onclick: close }, icon('x'))),
    h('div', { class: 'drawer-body' }, body),
    foot ? h('div', { class: 'drawer-foot' }, foot) : null);
  const overlay = h('div', { class: 'overlay drawer-overlay', onmousedown: (e) => { if (e.target === overlay) close(); } }, panel);
  document.body.append(overlay);
  document.addEventListener('keydown', onKey);
  return { close, panel, body: panel.querySelector('.drawer-body') };
}

// ---------- 通用片段 ----------
export function pageHead(title, desc, actions) {
  return h('div', { class: 'page-head' },
    h('div', {}, h('h1', { text: title }), desc ? h('p', { text: desc }) : null),
    actions ? h('div', { class: 'actions' }, actions) : null);
}
export function section({ title, desc, actions, body, foot, id, cls }) {
  return h('section', { class: 'section ' + (cls || ''), id },
    title ? h('div', { class: 'section-head' }, h('div', {}, h('h2', { text: title }), desc ? h('p', { text: desc }) : null),
      actions ? h('div', { class: 'actions' }, actions) : null) : null,
    h('div', { class: 'section-body' }, body),
    foot ? h('div', { class: 'section-foot' }, foot) : null);
}
export function callout(kind, content, iconName) {
  return h('div', { class: 'callout ' + kind }, icon(iconName || (kind === 'warn' || kind === 'bad' ? 'alert' : 'info')), h('div', { class: 'grow' }, content));
}
export function badge(text, kind = '', dot = false) { return h('span', { class: 'badge ' + kind + (dot ? ' dot' : ''), text }); }
export function empty(title, desc, action) {
  return h('div', { class: 'empty' }, h('b', { text: title }), desc ? h('div', { text: desc }) : null, action || null);
}
export function loading(lines = 3) {
  return h('div', { class: 'stack', style: { padding: '8px 0' } }, Array.from({ length: lines }, (_, i) => h('div', { class: 'skeleton', style: { width: (90 - i * 15) + '%' } })));
}
export function btn(label, onClick, kind = '', extra = {}) {
  return h('button', { class: 'btn ' + kind, onclick: onClick, type: 'button', ...extra }, extra.icon ? icon(extra.icon) : null, label);
}
export function toggleEl({ checked, onChange, disabled, label, id }) {
  const input = h('input', { type: 'checkbox', id, disabled, onchange: (e) => onChange && onChange(e.target.checked) });
  input.checked = !!checked;
  return h('label', { class: 'toggle' }, input, h('span', { class: 'track' }), label ? h('span', { text: label }) : null);
}
export function segmented(options, value, onChange) {
  const box = h('div', { class: 'seg', role: 'radiogroup' });
  const render = (v) => {
    box.replaceChildren(...options.map(([val, label]) => h('button', {
      type: 'button', class: String(val) === String(v) ? 'on' : '', role: 'radio', 'aria-checked': String(String(val) === String(v)),
      onclick: () => { render(val); onChange(val); },
    }, label)));
  };
  render(value);
  return box;
}
export function tabs(items, active, onChange) {
  const box = h('div', { class: 'tabs', role: 'tablist' });
  const render = (a) => box.replaceChildren(...items.map(([key, label, count]) => h('button', {
    type: 'button', role: 'tab', class: key === a ? 'on' : '', 'aria-selected': String(key === a),
    onclick: async () => { const result = await onChange(key); if (result !== false) render(key); },
  }, label, count !== undefined && count !== null ? badge(String(count)) : null)));
  render(active);
  return box;
}
export function searchBox(placeholder, onInput, value = '') {
  const input = h('input', { class: 'input', placeholder, value, oninput: (e) => onInput(e.target.value) });
  return h('div', { class: 'search' }, icon('search'), input);
}
export function debounce(fn, ms = 250) {
  let t;
  return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); };
}

// ---------- 表单模型 ----------
// 统一处理「草稿 → 校验 → 保存」：每个字段登记原值，改动后底部出现保存条。
// spec: {key, label, help, kind:'int'|'float'|'bool'|'string'|'select'|'text', min, max, step, unit, def, options, placeholder, advanced}
export class Form {
  constructor({ onSave, onChange } = {}) {
    this.fields = new Map();
    this.onSave = onSave;
    this.onChange = onChange;
  }
  field(spec, value) {
    const f = { spec, original: normalize(spec, value), input: null, row: null, errEl: null };
    this.fields.set(spec.key, f);
    return f;
  }
  value(key) {
    const f = this.fields.get(key);
    if (!f) return undefined;
    return readInput(f);
  }
  dirtyKeys() {
    return [...this.fields.values()].filter((f) => f.input && String(readInput(f)) !== String(f.original)).map((f) => f.spec.key);
  }
  isDirty() { return this.dirtyKeys().length > 0; }
  validate(keys) {
    let ok = true;
    for (const key of keys || this.dirtyKeys()) {
      const f = this.fields.get(key);
      if (!f) continue;
      const msg = validate(f.spec, readInput(f));
      setError(f, msg);
      if (msg) ok = false;
    }
    return ok;
  }
  values(keys) {
    const out = {};
    for (const key of keys || this.dirtyKeys()) out[key] = readInput(this.fields.get(key));
    return out;
  }
  reset() {
    for (const f of this.fields.values()) { if (f.input) writeInput(f, f.original); setError(f, ''); }
    this.refresh();
  }
  commit(values) {
    for (const [k, v] of Object.entries(values || this.values())) {
      const f = this.fields.get(k);
      if (f) f.original = normalize(f.spec, v);
    }
    this.refresh();
  }
  refresh() {
    for (const f of this.fields.values()) {
      if (f.row) f.row.classList.toggle('changed', !!f.input && String(readInput(f)) !== String(f.original));
    }
    this.onChange && this.onChange(this);
  }
  // 设置行：左说明右控件
  row(spec, value, opts = {}) {
    const f = this.field(spec, value);
    const control = buildControl(this, f);
    const metaBits = [];
    if (spec.kind === 'int' || spec.kind === 'float') {
      if (spec.max !== undefined) metaBits.push('范围 ' + (spec.min ?? 0) + ' – ' + spec.max + (spec.unit ? ' ' + spec.unit : ''));
    }
    if (spec.def !== undefined && spec.def !== '') metaBits.push('默认 ' + (spec.kind === 'bool' ? (String(spec.def) === '1' || spec.def === true ? '开' : '关') : spec.def + (spec.unit && spec.kind !== 'bool' ? ' ' + spec.unit : '')));
    f.errEl = h('div', { class: 's-error', hidden: true });
    f.row = h('div', { class: 'setting' + (opts.wide ? ' wide-control' : '') },
      h('div', {},
        h('div', { class: 's-label' }, h('label', { for: f.input.id, text: spec.label })),
        spec.help ? h('div', { class: 's-help', text: spec.help }) : null,
        metaBits.length ? h('div', { class: 's-meta', text: metaBits.join(' · ') }) : null),
      h('div', { class: 's-control' }, control),
      f.errEl);
    return f.row;
  }
  // 字段：标签在上
  stacked(spec, value) {
    const f = this.field(spec, value);
    const control = buildControl(this, f);
    f.errEl = h('div', { class: 'help', style: { color: '#f08a97' }, hidden: true });
    f.row = h('div', { class: 'field' + (spec.span ? ' span-2' : '') },
      h('label', { for: f.input.id, text: spec.label }), control,
      spec.help ? h('div', { class: 'help', text: spec.help }) : null, f.errEl);
    return f.row;
  }
}
let fieldSeq = 0;
function buildControl(form, f) {
  const s = f.spec;
  const id = 'f_' + (s.id || s.key).replace(/[^\w-]/g, '_') + '_' + (++fieldSeq);
  const onInput = () => { setError(f, ''); form.refresh(); };
  if (s.kind === 'bool') {
    const input = h('input', { type: 'checkbox', id, onchange: onInput });
    input.checked = f.original === '1';
    f.input = input;
    return h('label', { class: 'toggle' }, input, h('span', { class: 'track' }), s.toggleLabel ? h('span', { class: 'small text-2', text: s.toggleLabel }) : null);
  }
  if (s.kind === 'select') {
    const input = h('select', { class: 'select', id, onchange: onInput }, (s.options || []).map(([v, l]) => h('option', { value: v, text: l })));
    input.value = f.original;
    f.input = input;
    return input;
  }
  if (s.kind === 'text') {
    const input = h('textarea', { class: 'textarea', id, rows: s.rows || 4, placeholder: s.placeholder || '', oninput: onInput });
    input.value = f.original;
    f.input = input;
    return input;
  }
  const numeric = s.kind === 'int' || s.kind === 'float';
  const input = h('input', {
    class: 'input' + (numeric ? ' num' : ''), id, type: s.secret ? 'password' : 'text', inputmode: numeric ? 'decimal' : null,
    placeholder: s.placeholder || (s.def !== undefined && s.def !== '' ? String(s.def) : ''), autocomplete: s.secret ? 'new-password' : 'off', oninput: onInput,
  });
  input.value = f.original;
  f.input = input;
  if (s.unit) return unitWrap(input, s.unit);
  return input;
}
// 单位放在输入框右侧内部，按单位文字宽度留出空间，避免和数字重叠
export function unitWrap(input, unit) {
  const wrap = h('div', { class: 'input-wrap has-unit' }, input, h('span', { class: 'unit', text: unit }));
  const em = [...unit].reduce((w, ch) => w + (/[\x00-\x7f]/.test(ch) ? 0.58 : 1), 0);
  wrap.style.setProperty('--unit-w', em.toFixed(2) + 'em');
  return wrap;
}
function normalize(spec, v) {
  if (spec.kind === 'bool') return v === true || v === 1 || v === '1' || v === 'true' ? '1' : '0';
  return v === undefined || v === null ? '' : String(v);
}
function readInput(f) {
  if (!f.input) return f.original;
  if (f.spec.kind === 'bool') return f.input.checked ? '1' : '0';
  return String(f.input.value).trim();
}
function writeInput(f, v) {
  if (f.spec.kind === 'bool') f.input.checked = v === '1';
  else f.input.value = v;
}
function validate(spec, raw) {
  if (spec.kind !== 'int' && spec.kind !== 'float') {
    if (spec.required && !raw) return '请填写' + spec.label;
    if (spec.pattern && raw && !spec.pattern.test(raw)) return spec.patternMsg || '格式不正确';
    return '';
  }
  if (raw === '') return spec.optional ? '' : '请填写数值';
  const n = Number(raw);
  if (!Number.isFinite(n)) return '请填写数字';
  if (spec.kind === 'int' && !Number.isInteger(n)) return '请填写整数';
  const lo = spec.min ?? (spec.max !== undefined ? 0 : undefined);
  if (lo !== undefined && n < lo) return '不能小于 ' + lo;
  if (spec.max !== undefined && n > spec.max) return '不能大于 ' + spec.max;
  return '';
}
function setError(f, msg) {
  if (!f || !f.input) return;
  f.input.setAttribute('aria-invalid', msg ? 'true' : 'false');
  if (f.errEl) { f.errEl.hidden = !msg; f.errEl.textContent = msg; }
}

// 底部保存条：一个页面同一时间只有一个
let savebarEl = null;
export function savebar(form, { onSave, label } = {}) {
  if (!savebarEl) { savebarEl = h('div', { class: 'savebar', role: 'status' }); document.body.append(savebarEl); }
  const count = h('span');
  const saveBtn = btn('保存修改', async () => {
    const keys = form.dirtyKeys();
    if (!form.validate(keys)) { toast('有填写不正确的项，请检查标红的输入框', 'bad'); return; }
    saveBtn.disabled = true;
    try { await onSave(form.values(keys), keys); form.commit(form.values(keys)); toast('已保存'); }
    catch (e) { toast(errText(e), 'bad'); }
    finally { saveBtn.disabled = false; }
  }, 'primary sm');
  savebarEl.replaceChildren(count, btn('放弃', () => form.reset(), 'ghost sm'), saveBtn);
  const update = () => {
    const n = form.dirtyKeys().length;
    count.replaceChildren((label || '') , h('b', { text: n }), ' 项修改尚未保存');
    savebarEl.classList.toggle('on', n > 0);
  };
  form.onChange = update;
  update();
  return update;
}
export function hideSavebar() { if (savebarEl) savebarEl.classList.remove('on'); }

// 页面生命周期：切换路由或重新渲染前释放定时器、监听器。
let pageCleanup = null;
export function setPageCleanup(fn) { pageCleanup = typeof fn === 'function' ? fn : null; }
function cleanupPage() {
  const fn = pageCleanup;
  pageCleanup = null;
  if (!fn) return;
  try { fn(); } catch (e) {}
}

// ---------- 路由 ----------
const routes = new Map();
let current = null;
let guardFn = null;
export function route(name, def) { routes.set(name, def); }
export function routeDefs() { return routes; }
export function setLeaveGuard(fn) { guardFn = fn; }
export function currentRoute() { return current; }
export function parseHash() {
  const raw = location.hash.replace(/^#\/?/, '');
  const [name, ...rest] = raw.split('/');
  return { name: name || 'dashboard', params: rest.map(decodeURIComponent) };
}
export function navigate(name, ...params) {
  const hash = '#' + [name, ...params.map(encodeURIComponent)].join('/');
  if (location.hash === hash) render(); else location.hash = hash;
}
export function replaceHash(hash) {
  history.replaceState(null, '', hash);
  lastHash = location.hash;
}
export async function render() {
  const { name, params } = parseHash();
  const def = routes.get(name) || routes.get('dashboard');
  const key = routes.has(name) ? name : 'dashboard';
  if (!$('#view')) return; // 还在登录页或框架未建好，boot 完成后会再渲染
  cleanupPage();
  hideSavebar();
  guardFn = null;
  current = key;
  document.dispatchEvent(new CustomEvent('rf4:route', { detail: { name: key } }));
  const root = $('#view');
  root.replaceChildren(h('div', { class: 'page' }, loading(4)));
  try {
    const node = await def.render(params);
    if (current !== key) return;
    root.replaceChildren(node);
    window.scrollTo(0, 0);
  } catch (e) {
    if (e && e.status === 401) return;
    root.replaceChildren(h('div', { class: 'page' }, callout('bad', ['页面加载失败：', errText(e)])));
  }
}
window.addEventListener('beforeunload', (e) => { if (guardFn && guardFn()) { e.preventDefault(); e.returnValue = ''; } });
let lastHash = location.hash;
window.addEventListener('hashchange', () => {
  if (guardFn && guardFn() && !confirm('有修改还没保存，确定离开吗？')) {
    history.replaceState(null, '', lastHash);
    return;
  }
  lastHash = location.hash;
  render();
});

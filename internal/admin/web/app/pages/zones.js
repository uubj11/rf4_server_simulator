// 鱼群区域编辑器：在纸地图上画出一块区域，规定区内出哪些鱼。
// 左边地图（矩形 / 自由涂抹 / 圆形 / 移动），右边区域列表与属性。
// 坐标换算与 docs/fish-query-coordinates.md 同源：
//   图像像素 = (世界 - 原点) × scale + offset（Z 轴从图片底部往上量，需要翻转）
//   网格 = floor(世界 / 40)；游戏里看到的坐标 = (世界 - 显示原点) / 5
import { h, api, apiList, post, toast, errText, badge, btn, empty, svg, confirmDialog, callout, setLeaveGuard, segmented, toggleEl, webURL } from '../core.js';

const CELL = 40;
const SIZE = 2048;
const COLORS = ['#37e2b0', '#42b8ff', '#ff9f6e', '#ffd166', '#b28dff', '#7bdff2', '#f78fb3', '#9ddca0'];
const NEW_COLOR = '#ffbb55';
const color = (z) => COLORS[(z.id || 0) % COLORS.length];
const tint = (hex, a) => { const n = parseInt(hex.slice(1), 16); return `rgba(${n >> 16},${(n >> 8) & 255},${n & 255},${a})`; };
const KIND_NAME = { grid_rect: '矩形', grid_cells: '自由形状', circle: '圆形', sea_zone: '海图区号' };
const SVG = {
  rect: '<rect x="4" y="5" width="16" height="14" rx="1.5"/>',
  brush: '<path d="M18.4 2.6a2 2 0 0 1 3 3L12 15l-4-4Z"/><path d="M7 14c-2 0-3.5 1.5-3.5 3.5 0 1.4-.5 2.5-1.5 3 3 .5 7-1 7-4.5"/>',
  circle: '<circle cx="12" cy="12" r="8"/><circle cx="12" cy="12" r="1" fill="currentColor"/>',
  pan: '<path d="M18 11V6a2 2 0 0 0-4 0v5M14 10V4a2 2 0 0 0-4 0v6M10 10.5V6a2 2 0 0 0-4 0v8"/><path d="M18 8a2 2 0 0 1 4 0v6a8 8 0 0 1-8 8h-2c-2.8 0-4.5-.9-6-2.3l-3.6-3.6a2 2 0 0 1 2.8-2.8L7 15"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  minus: '<path d="M5 12h14"/>',
  fit: '<path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5"/>',
};
const TOOLS = [
  ['rect', '矩形', 'R', '在地图上拖出一块矩形区域。点已有的区域可以编辑它。'],
  ['brush', '自由涂抹', 'B', '按住拖动，把格子涂进区域；按住 Alt 可以擦除。'],
  ['circle', '圆形', 'C', '从圆心往外拖出半径。适合小坑、窄口这类小地方。'],
  ['pan', '移动地图', 'H', '拖动地图。滚轮随时可以缩放。'],
];

export function zonesEditor({ area, zones, roster }) {
  const loc = area.loc;
  const kind = area.map_kind || 'none';
  const cal = area.calibration && area.calibration.paper ? area.calibration : null;
  const canDraw = kind === 'grid' && !!cal && !!area.texture;
  const fishName = new Map((roster || []).map((f) => [f.fish_key, f.name_zh || f.fish_key]));
  const S = {
    zones: zones.map((z) => ({ ...z, fish: z.fish || [] })), edit: null, dirty: false, sel: 0, hover: 0,
    tool: 'rect', erase: false, view: { tx: 0, ty: 0, k: 1 }, fitted: false, imgOk: false,
    draft: null, stroke: null, pin: null, down: null,
  };
  setLeaveGuard(() => S.dirty);

  // ---------- 坐标 ----------
  const P = cal && cal.paper;
  const toImg = (wx, wz) => [(wx - cal.origin_x) * P.scale + P.offset_x, P.height - ((wz - cal.origin_z) * P.scale + P.offset_z)];
  const toWorld = (px, pz) => [cal.origin_x + (px - P.offset_x) / P.scale, cal.origin_z + ((P.height - pz) - P.offset_z) / P.scale];
  const hud = (wx, wz) => [(wx - (cal.display_origin_x ?? cal.origin_x)) / 5, (wz - (cal.display_origin_z ?? cal.origin_z)) / 5];
  const fromHud = (hx, hz) => [hx * 5 + (cal.display_origin_x ?? cal.origin_x), hz * 5 + (cal.display_origin_z ?? cal.origin_z)];
  const clampPx = (v) => Math.min(SIZE - 0.001, Math.max(0, v));
  const gridAt = (px, pz) => { const [wx, wz] = toWorld(clampPx(px), clampPx(pz)); return [Math.floor(wx / CELL), Math.floor(wz / CELL)]; };
  const rectPx = (gx0, gz0, gx1, gz1) => {
    const [ax, az] = toImg(Math.min(gx0, gx1) * CELL, Math.min(gz0, gz1) * CELL);
    const [bx, bz] = toImg((Math.max(gx0, gx1) + 1) * CELL, (Math.max(gz0, gz1) + 1) * CELL);
    return [Math.min(ax, bx), Math.min(az, bz), Math.abs(bx - ax), Math.abs(bz - az)];
  };
  const cellsOf = (z) => {
    if (z.kind === 'grid_cells') return z.cells || [];
    if (z.kind !== 'grid_rect') return [];
    const out = [];
    for (let gx = Math.min(z.gx0, z.gx1); gx <= Math.max(z.gx0, z.gx1); gx++) for (let gz = Math.min(z.gz0, z.gz1); gz <= Math.max(z.gz0, z.gz1); gz++) out.push([gx, gz]);
    return out;
  };
  const r0 = (v) => String(Math.round(v));
  const hudRange = (gx0, gz0, gx1, gz1) => {
    const [a, b] = hud(gx0 * CELL, gz0 * CELL), [c, d] = hud((gx1 + 1) * CELL, (gz1 + 1) * CELL);
    return `${r0(a)}:${r0(b)} 到 ${r0(c)}:${r0(d)}`;
  };
  function rangeText(z) {
    if (z.kind === 'sea_zone') { const s = z.sea_zones && z.sea_zones.length ? z.sea_zones : [z.sea_zone]; return `海图 ${s.join('、')} 号区`; }
    if (z.kind === 'circle') return cal ? `圆形，半径 ${Math.round(z.radius)} 米，圆心在 ${hud(z.cx, z.cz).map(r0).join(':')}` : `圆形，半径 ${Math.round(z.radius)} 米`;
    const cells = cellsOf(z);
    if (!cells.length) return '还没有画范围';
    let x0 = 1e9, z0 = 1e9, x1 = -1e9, z1 = -1e9;
    cells.forEach(([x, zz]) => { x0 = Math.min(x0, x); z0 = Math.min(z0, zz); x1 = Math.max(x1, x); z1 = Math.max(z1, zz); });
    const size = z.kind === 'grid_cells' ? `${cells.length} 个格子` : `${(x1 - x0 + 1) * CELL}×${(z1 - z0 + 1) * CELL} 米`;
    return cal ? `${size}，游戏坐标 ${hudRange(x0, z0, x1, z1)}` : `${size}，网格 ${x0}:${z0} 到 ${x1}:${z1}`;
  }
  function fishSummary(z) {
    const on = (z.fish || []).filter((f) => f.enabled);
    if (!on.length) return z.mode === 'exclusive' ? '没有指定鱼种：区内钓不到鱼' : '没有指定鱼种';
    return on.slice(0, 6).map((f) => fishName.get(f.fish_key) || f.fish_key).join('、') + (on.length > 6 ? ` 等 ${on.length} 种` : '');
  }

  // ---------- 地图 DOM ----------
  const canvas = () => h('canvas', { width: SIZE, height: SIZE });
  const img = h('img', { alt: '', draggable: 'false' });
  const cvGrid = canvas(), cvZones = canvas(), cvTop = canvas();
  const inner = h('div', { class: 'zmap-inner' }, img, cvGrid, cvZones, cvTop);
  const guide = h('div', { class: 'zmap-guide' }, h('b', { text: '在地图上拖出一块矩形，就能创建第一个鱼群区域' }), h('span', { text: '也可以用左边的「自由涂抹」或「圆形」工具。' }));
  const stage = h('div', { class: 'zmap-stage' }, inner, guide);
  const readout = h('span', { class: 'grow' });
  const overlapNote = h('span', { style: { color: '#f3a0a0' } });
  const toolButtons = {};
  const toolbar = h('div', { class: 'zmap-tools' },
    TOOLS.map(([t, label, key, help]) => (toolButtons[t] = h('button', { type: 'button', title: `${label}（${key}）\n${help}`, 'aria-label': label, onclick: () => setTool(t) }, svg(SVG[t], 18)))),
    h('hr'),
    h('button', { type: 'button', title: '放大', 'aria-label': '放大', onclick: () => zoomBy(1.35) }, svg(SVG.plus, 18)),
    h('button', { type: 'button', title: '缩小', 'aria-label': '缩小', onclick: () => zoomBy(1 / 1.35) }, svg(SVG.minus, 18)),
    h('button', { type: 'button', title: '看全图', 'aria-label': '看全图', onclick: fit }, svg(SVG.fit, 18)));
  const pinInput = h('input', { class: 'input', style: { width: '120px', height: '28px', fontSize: '12px' }, placeholder: '如 81:61', onchange: (e) => setPin(e.target.value) });
  const status = h('div', { class: 'zmap-status' },
    readout, overlapNote,
    h('label', { class: 'row', style: { gap: '6px' }, title: '填入游戏里看到的坐标，地图上会标出位置，用来核对区域画得准不准' }, '定位', pinInput));
  const mapBox = h('div', { class: 'zmap-canvas' }, stage, toolbar, status);

  // ---------- 视图 ----------
  // 舞台不一定是正方形（高度受窗口限制）：地图比舞台小的方向居中，比舞台大的方向限制拖出边界
  function applyView() {
    const v = S.view, w = stage.clientWidth || 1, hh = stage.clientHeight || 1;
    v.k = Math.min(12, Math.max(Math.min(w, hh) / SIZE, v.k));
    const sw = SIZE * v.k;
    v.tx = sw <= w ? (w - sw) / 2 : Math.min(0, Math.max(w - sw, v.tx));
    v.ty = sw <= hh ? (hh - sw) / 2 : Math.min(0, Math.max(hh - sw, v.ty));
    inner.style.transform = `translate(${v.tx}px,${v.ty}px) scale(${v.k})`;
  }
  function fit() { S.view = { tx: 0, ty: 0, k: 0 }; applyView(); }
  function zoomAt(sx, sy, f) {
    const v = S.view, px = (sx - v.tx) / v.k, py = (sy - v.ty) / v.k;
    v.k *= f; applyView(); v.tx = sx - px * v.k; v.ty = sy - py * v.k; applyView();
  }
  function zoomBy(f) { zoomAt(stage.clientWidth / 2, stage.clientHeight / 2, f); }
  function centerOn(px, py) { const v = S.view; v.tx = stage.clientWidth / 2 - px * v.k; v.ty = stage.clientHeight / 2 - py * v.k; applyView(); }
  const stagePos = (e) => { const r = stage.getBoundingClientRect(), v = S.view; return [(e.clientX - r.left - v.tx) / v.k, (e.clientY - r.top - v.ty) / v.k]; };
  new ResizeObserver(() => { if (!stage.clientWidth) return; if (!S.fitted) { S.fitted = true; fit(); } else applyView(); }).observe(stage);

  // ---------- 绘制 ----------
  const ctx = (cv) => { const g = cv.getContext('2d'); g.clearRect(0, 0, SIZE, SIZE); return g; };
  function label(g, text, x, y, fg) {
    g.font = 'bold 40px "Segoe UI","Microsoft YaHei",sans-serif';
    g.fillStyle = 'rgba(6,17,29,.8)'; g.fillRect(x, y, g.measureText(text).width + 22, 54);
    g.fillStyle = fg; g.fillText(text, x + 11, y + 40);
  }
  function drawCircle(g, cx, cz, rM, col, fillA, lw, dash) {
    const [px, pz] = toImg(cx, cz), r = rM * P.scale;
    g.fillStyle = tint(col, fillA); g.beginPath(); g.arc(px, pz, r, 0, Math.PI * 2); g.fill();
    g.setLineDash(dash); g.strokeStyle = col; g.lineWidth = lw; g.beginPath(); g.arc(px, pz, r, 0, Math.PI * 2); g.stroke(); g.setLineDash([]);
    g.fillStyle = col; g.beginPath(); g.arc(px, pz, 4, 0, Math.PI * 2); g.fill();
  }
  function drawGrid() {
    const g = ctx(cvGrid);
    if (!S.imgOk) return;
    const [ax, az] = toWorld(0, 0), [bx, bz] = toWorld(SIZE, SIZE);
    const gx0 = Math.floor(Math.min(ax, bx) / CELL), gx1 = Math.ceil(Math.max(ax, bx) / CELL);
    const gz0 = Math.floor(Math.min(az, bz) / CELL), gz1 = Math.ceil(Math.max(az, bz) / CELL);
    g.font = 'bold 34px Consolas,monospace';
    for (let gx = gx0; gx <= gx1; gx++) {
      const x = toImg(gx * CELL, 0)[0], major = gx % 5 === 0;
      g.strokeStyle = major ? 'rgba(66,184,255,.40)' : 'rgba(66,184,255,.13)'; g.lineWidth = major ? 2 : 1;
      g.beginPath(); g.moveTo(x, 0); g.lineTo(x, SIZE); g.stroke();
      if (major) { g.fillStyle = 'rgba(170,215,245,.9)'; g.fillText(r0(hud(gx * CELL, 0)[0]), Math.min(x + 6, SIZE - 90), 38); }
    }
    for (let gz = gz0; gz <= gz1; gz++) {
      const y = toImg(0, gz * CELL)[1], major = gz % 5 === 0;
      g.strokeStyle = major ? 'rgba(66,184,255,.40)' : 'rgba(66,184,255,.13)'; g.lineWidth = major ? 2 : 1;
      g.beginPath(); g.moveTo(0, y); g.lineTo(SIZE, y); g.stroke();
      if (major) { g.fillStyle = 'rgba(170,215,245,.9)'; g.fillText(r0(hud(0, gz * CELL)[1]), 8, Math.min(y - 6, SIZE - 8)); }
    }
  }
  function drawZones() {
    if (!canDraw) return;
    const g = ctx(cvZones);
    if (!S.imgOk) return;
    const list = S.zones.map((z) => (S.edit && S.edit.id === z.id ? S.edit : z));
    if (S.edit && !S.edit.id) list.push(S.edit);
    const cover = new Map();
    for (const z of list) {
      const editing = S.edit === z, isNew = !z.id;
      const sel = editing && !isNew, hov = z.id && z.id === S.hover;
      const dim = !z.enabled && !editing;
      const col = isNew ? NEW_COLOR : dim ? '#8294aa' : color(z);
      const fillA = dim ? 0.08 : (sel || hov || isNew) ? 0.32 : 0.18;
      const lw = sel || isNew ? 4 : hov ? 3 : 2;
      const dash = dim || isNew ? [12, 6] : [];
      const text = z.name || (isNew ? '新区域' : '#' + z.id);
      const fg = isNew ? '#ffe3b0' : dim ? '#c3ccd6' : '#eafffa';
      if (z.kind === 'circle') {
        drawCircle(g, z.cx, z.cz, z.radius || 2, col, fillA, lw, dash);
        const [px, pz] = toImg(z.cx, z.cz); label(g, text, px + 6, pz - 62, fg);
        continue;
      }
      const cells = cellsOf(z);
      if (!cells.length) continue;
      let x0 = 1e9, y0 = 1e9, x1 = -1e9, y1 = -1e9;
      for (const [gx, gz] of cells) {
        const [x, y, w, hh] = rectPx(gx, gz, gx, gz);
        g.fillStyle = tint(col, fillA); g.fillRect(x, y, w, hh);
        x0 = Math.min(x0, x); y0 = Math.min(y0, y); x1 = Math.max(x1, x + w); y1 = Math.max(y1, y + hh);
        if (z.enabled || editing) { const k = gx + ',' + gz; cover.set(k, (cover.get(k) || 0) + 1); }
      }
      // 外轮廓：只描和区外相邻的边，形状看得清楚
      const inside = new Set(cells.map(([a, b]) => a + ',' + b));
      g.setLineDash(dash); g.strokeStyle = col; g.lineWidth = lw; g.beginPath();
      for (const [gx, gz] of cells) {
        const [x, y, w, hh] = rectPx(gx, gz, gx, gz);
        if (!inside.has(gx + ',' + (gz + 1))) { g.moveTo(x, y); g.lineTo(x + w, y); }
        if (!inside.has(gx + ',' + (gz - 1))) { g.moveTo(x, y + hh); g.lineTo(x + w, y + hh); }
        if (!inside.has((gx - 1) + ',' + gz)) { g.moveTo(x, y); g.lineTo(x, y + hh); }
        if (!inside.has((gx + 1) + ',' + gz)) { g.moveTo(x + w, y); g.lineTo(x + w, y + hh); }
      }
      g.stroke(); g.setLineDash([]);
      label(g, text, x0 + 4, y0 + 4, fg);
    }
    let overlaps = 0;
    g.lineWidth = 2; g.strokeStyle = 'rgba(255,120,120,.8)';
    for (const [k, n] of cover) {
      if (n < 2) continue;
      overlaps++;
      const [gx, gz] = k.split(',').map(Number), [x, y, w, hh] = rectPx(gx, gz, gx, gz);
      g.beginPath(); g.moveTo(x, y + hh); g.lineTo(x + w, y); g.stroke();
    }
    overlapNote.textContent = overlaps ? `${overlaps} 个格子被多个区域覆盖（红色斜线），会同时生效` : '';
    guide.hidden = !!(S.zones.length || S.edit);
  }
  function drawTop() {
    if (!canDraw) return;
    const g = ctx(cvTop);
    if (!S.imgOk) return;
    const d = S.draft;
    if (d) {
      const [x, y, w, hh] = rectPx(d.gx0, d.gz0, d.gx1, d.gz1);
      g.fillStyle = tint(NEW_COLOR, 0.22); g.fillRect(x, y, w, hh);
      g.setLineDash([12, 6]); g.strokeStyle = NEW_COLOR; g.lineWidth = 3; g.strokeRect(x, y, w, hh); g.setLineDash([]);
    }
    if (S.stroke && S.stroke.circle) drawCircle(g, S.stroke.cx, S.stroke.cz, Math.max(S.stroke.r, 0.5), NEW_COLOR, 0.22, 3, [12, 6]);
    if (S.pin) {
      const [px, py] = toImg(S.pin[0], S.pin[1]);
      g.strokeStyle = '#ff4d6d'; g.lineWidth = 5; g.beginPath();
      g.moveTo(px - 28, py); g.lineTo(px + 28, py); g.moveTo(px, py - 28); g.lineTo(px, py + 28); g.stroke();
      g.beginPath(); g.arc(px, py, 10, 0, Math.PI * 2); g.stroke();
    }
  }
  const drawAll = () => { drawGrid(); drawZones(); drawTop(); };

  function setPin(v) {
    v = (v || '').trim();
    if (!v) { S.pin = null; drawTop(); return; }
    const m = v.match(/(-?[\d.]+)\s*[:：,/ ]\s*(-?[\d.]+)/);
    if (!m) { toast('请按游戏里的格式填写，例如 81:61', 'bad'); return; }
    S.pin = fromHud(parseFloat(m[1]), parseFloat(m[2]));
    drawTop();
    const [px, py] = toImg(S.pin[0], S.pin[1]);
    centerOn(px, py);
  }

  // ---------- 命中与交互 ----------
  function hitTest(pos) {
    const [wx, wz] = toWorld(pos[0], pos[1]), gx = Math.floor(wx / CELL), gz = Math.floor(wz / CELL);
    for (let i = S.zones.length - 1; i >= 0; i--) {
      const z = S.zones[i];
      if (z.kind === 'circle') { if ((wx - z.cx) ** 2 + (wz - z.cz) ** 2 <= z.radius ** 2) return z; }
      else if (cellsOf(z).some(([a, b]) => a === gx && b === gz)) return z;
    }
    return null;
  }
  function paintAt(pos, erase) {
    const z = S.edit;
    if (!z || z.kind !== 'grid_cells') return;
    const [gx, gz] = gridAt(pos[0], pos[1]);
    z.cells = z.cells || [];
    const i = z.cells.findIndex(([a, b]) => a === gx && b === gz);
    if (erase && i >= 0) z.cells.splice(i, 1);
    else if (!erase && i < 0) z.cells.push([gx, gz]);
    else return;
    S.dirty = true;
    drawZones();
    if (refs.cellCount) refs.cellCount.textContent = z.cells.length + ' 个格子';
  }
  function hoverText(pos) {
    const [gx, gz] = gridAt(pos[0], pos[1]);
    const [hx, hz] = hud(gx * CELL, gz * CELL);
    return `游戏坐标 ${r0(hx)}~${r0(hx + 8)} : ${r0(hz)}~${r0(hz + 8)}（一格 40 米）`;
  }
  function toolHint() { return (TOOLS.find(([t]) => t === S.tool) || [])[3] || ''; }

  if (canDraw) {
    stage.addEventListener('wheel', (e) => { e.preventDefault(); const r = stage.getBoundingClientRect(); zoomAt(e.clientX - r.left, e.clientY - r.top, Math.pow(1.0015, -e.deltaY)); }, { passive: false });
    stage.addEventListener('pointerdown', (e) => {
      if (!S.imgOk) return;
      e.preventDefault();
      try { stage.setPointerCapture(e.pointerId); } catch (err) { /* 指针已失效时不影响后续绘制 */ }
      const pos = stagePos(e);
      if (S.tool === 'pan' || e.button === 1) { S.stroke = { pan: true, sx: e.clientX, sy: e.clientY, tx: S.view.tx, ty: S.view.ty }; return; }
      if (S.tool === 'brush') { S.stroke = { paint: true, erase: e.altKey || S.erase }; paintAt(pos, S.stroke.erase); return; }
      const hit = hitTest(pos);
      if (hit) { select(hit.id); return; }
      if (S.tool === 'circle') { const [wx, wz] = toWorld(pos[0], pos[1]); S.stroke = { circle: true, cx: wx, cz: wz, r: 0 }; return; }
      const [gx, gz] = gridAt(pos[0], pos[1]);
      S.down = pos; S.draft = { ax: gx, az: gz, gx0: gx, gz0: gz, gx1: gx, gz1: gz };
      drawTop();
    });
    stage.addEventListener('pointermove', (e) => {
      if (!S.imgOk) return;
      const pos = stagePos(e);
      const st = S.stroke;
      if (st && st.pan) { S.view.tx = st.tx + (e.clientX - st.sx); S.view.ty = st.ty + (e.clientY - st.sy); applyView(); return; }
      if (st && st.paint) { paintAt(pos, st.erase); readout.textContent = `涂抹中：${(S.edit.cells || []).length} 个格子${st.erase ? '（擦除）' : ''}`; return; }
      if (st && st.circle) {
        const [wx, wz] = toWorld(pos[0], pos[1]); st.r = Math.hypot(wx - st.cx, wz - st.cz); drawTop();
        readout.textContent = `半径 ${Math.round(st.r)} 米，圆心在 ${hud(st.cx, st.cz).map(r0).join(':')} · 松开完成`;
        return;
      }
      if (S.draft) {
        const [gx, gz] = gridAt(pos[0], pos[1]), d = S.draft;
        d.gx0 = Math.min(d.ax, gx); d.gx1 = Math.max(d.ax, gx); d.gz0 = Math.min(d.az, gz); d.gz1 = Math.max(d.az, gz);
        drawTop();
        readout.textContent = `${(d.gx1 - d.gx0 + 1) * CELL}×${(d.gz1 - d.gz0 + 1) * CELL} 米，游戏坐标 ${hudRange(d.gx0, d.gz0, d.gx1, d.gz1)} · 松开完成`;
        return;
      }
      const hit = S.tool === 'rect' || S.tool === 'circle' ? hitTest(pos) : null;
      const id = hit ? hit.id : 0;
      if (id !== S.hover) { S.hover = id; drawZones(); }
      stage.style.cursor = S.tool === 'pan' ? 'grab' : S.tool === 'brush' ? 'cell' : hit ? 'pointer' : 'crosshair';
      readout.textContent = hit ? `「${hit.name || '#' + hit.id}」· 点击编辑` : hoverText(pos);
    });
    const finish = (e) => {
      const st = S.stroke; S.stroke = null;
      readout.textContent = toolHint();
      if (st) {
        if (st.circle) { drawTop(); if (st.r >= 2) startNew({ kind: 'circle', cx: st.cx, cz: st.cz, radius: Math.round(st.r) }); }
        return;
      }
      if (!S.draft) return;
      const d = S.draft; S.draft = null; drawTop();
      const pos = e ? stagePos(e) : S.down;
      if (S.down && Math.hypot(pos[0] - S.down[0], pos[1] - S.down[1]) < 6) { if (S.edit && !S.dirty) closeEdit(); return; }
      startNew({ kind: 'grid_rect', gx0: d.gx0, gz0: d.gz0, gx1: d.gx1, gz1: d.gz1 });
    };
    stage.addEventListener('pointerup', finish);
    stage.addEventListener('pointercancel', () => { S.stroke = null; S.draft = null; drawTop(); });
    stage.addEventListener('pointerleave', () => { if (!S.stroke && !S.draft) readout.textContent = toolHint(); });
    const onKey = (e) => {
      if (!stage.isConnected) { document.removeEventListener('keydown', onKey); return; }
      if (document.querySelector('.overlay')) return;
      const tag = (document.activeElement && document.activeElement.tagName) || '';
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || e.ctrlKey || e.metaKey) return;
      const k = e.key.toLowerCase();
      const t = { r: 'rect', v: 'rect', b: 'brush', c: 'circle', h: 'pan' }[k];
      if (t) { setTool(t); e.preventDefault(); }
      else if (e.key === 'Escape' && S.edit && !S.dirty) closeEdit();
    };
    document.addEventListener('keydown', onKey);
    img.onload = () => { S.imgOk = true; drawAll(); readout.textContent = toolHint(); };
    img.onerror = () => { S.imgOk = false; mapBox.replaceChildren(callout('warn', '读不到这张地图的图片。地图图片从游戏目录加载，请确认启动器里设置的游戏路径正确。')); };
    img.src = webURL('/' + area.texture);
  }

  function setTool(t) {
    if (t === 'brush' && !(S.edit && S.edit.kind === 'grid_cells')) {
      startNew({ kind: 'grid_cells', cells: [] }, false).then((ok) => { if (ok) applyTool('brush'); });
      return;
    }
    applyTool(t);
  }
  function applyTool(t) {
    S.tool = t;
    for (const [k, b] of Object.entries(toolButtons)) b.classList.toggle('on', k === t);
    stage.style.cursor = t === 'pan' ? 'grab' : t === 'brush' ? 'cell' : 'crosshair';
    readout.textContent = toolHint();
  }

  // ---------- 状态切换 ----------
  async function canLeaveEdit() {
    if (!S.dirty) return true;
    const ok = await confirmDialog({ title: '这个区域还没保存', desc: '继续操作会放弃刚才的修改。', confirm: '放弃修改' });
    if (ok) S.dirty = false;
    return ok;
  }
  async function startNew(fields, dirty = true) {
    if (!(await canLeaveEdit())) return false;
    S.edit = { id: 0, loc, name: '', kind: 'grid_rect', sea_zone: 1, gx0: 0, gz0: 0, gx1: 0, gz1: 0, mode: 'exclusive', boost: 3, keep_others: 0.35, enabled: true, fish: [], ...fields };
    S.sel = 0; S.dirty = dirty;
    renderSide(); drawZones();
    setTimeout(() => refs.name && refs.name.focus(), 30);
    return true;
  }
  async function select(id) {
    if (S.edit && S.edit.id === id) return;
    if (!(await canLeaveEdit())) return;
    const z = S.zones.find((x) => x.id === id);
    if (!z) return;
    S.edit = JSON.parse(JSON.stringify(z)); S.sel = id; S.dirty = false;
    if (S.tool === 'brush' && S.edit.kind !== 'grid_cells') applyTool('rect');
    renderSide(); drawZones();
  }
  function closeEdit() {
    S.edit = null; S.sel = 0; S.dirty = false;
    if (S.tool === 'brush') applyTool('rect');
    renderSide(); drawZones();
  }
  async function reload() {
    const d = await api('/api/fish/zones');
    S.zones = (d.zones || []).filter((z) => z.loc === loc).map((z) => ({ ...z, fish: z.fish || [] }));
  }

  // ---------- 右侧面板 ----------
  const side = h('div', { class: 'card zmap-side' });
  const refs = {};
  function renderSide() { side.replaceChildren(S.edit ? editPanel() : listPanel()); }

  function listPanel() {
    const zs = S.zones;
    const cards = zs.map((z) => {
      const sw = h('span', { class: 'zswatch', style: { background: z.enabled ? color(z) : '#8294aa' } });
      const tg = h('span', { onclick: (e) => e.stopPropagation() }, toggleEl({ checked: z.enabled, onChange: (on) => toggleEnabled(z, on) }));
      return h('div', {
        class: 'zcard' + (z.enabled ? '' : ' off'), tabindex: 0,
        onclick: () => select(z.id), onkeydown: (e) => { if (e.key === 'Enter') select(z.id); },
        onmouseenter: () => { S.hover = z.id; drawZones(); }, onmouseleave: () => { S.hover = 0; drawZones(); },
      },
      h('div', { class: 'row', style: { flexWrap: 'nowrap' } }, sw, h('b', { class: 'grow', style: { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }, text: z.name || '#' + z.id }),
        z.mode === 'exclusive' ? badge('独占') : badge('增益 ×' + z.boost, 'accent'), tg),
      h('div', { class: 'sub', text: rangeText(z) }),
      h('div', { class: 'sub', style: { color: 'var(--text-2)' }, text: fishSummary(z) }));
    });
    const stats = zs.length ? `独占 ${zs.filter((z) => z.mode === 'exclusive').length} · 增益 ${zs.filter((z) => z.mode === 'boost').length}` + (zs.some((z) => !z.enabled) ? ` · 停用 ${zs.filter((z) => !z.enabled).length}` : '') : '';
    return h('div', { class: 'zside-body' },
      h('div', { class: 'row', style: { marginBottom: '4px' } }, h('b', { class: 'grow', text: `区域（${zs.length}）` }),
        kind !== 'sea' ? btn(canDraw ? '手动输入' : '新建区域', () => startNew({ kind: 'grid_rect' }), canDraw ? 'ghost sm' : 'primary sm', { icon: canDraw ? null : 'plus', title: '按网格数字新建矩形区域' }) : null),
      stats ? h('div', { class: 'muted small', style: { marginBottom: '10px' }, text: stats }) : null,
      zs.length ? h('div', { class: 'stack', style: { gap: '8px' } }, cards)
        : empty('还没有鱼群区域', kind === 'sea' ? '挪威海不划分区域，按全图鱼种出鱼。' : canDraw ? '在左边地图上画一块区域，规定那里出哪些鱼。' : '这张地图没有可用的纸地图，可以按网格数字新建。'),
      h('p', { class: 'muted small', style: { marginTop: '14px' }, text: '独占：区内只钓得到名单里的鱼。增益：名单里的鱼在区内更常见。区域保存后约 30 秒生效，不受其他玩法开关影响。' }));
  }

  function editPanel() {
    const z = S.edit, isNew = !z.id;
    const touch = () => { S.dirty = true; drawZones(); };
    refs.name = h('input', { class: 'input', value: z.name || '', placeholder: '例如 湾口鲤鱼区', oninput: (e) => { z.name = e.target.value; touch(); } });

    // 范围
    let range;
    const num = (val, onInput, extra = {}) => h('input', { class: 'input num', value: val, oninput: (e) => onInput(e.target.value), ...extra });
    if (z.kind === 'grid_cells') {
      refs.cellCount = h('span', { class: 'muted small', text: (z.cells || []).length + ' 个格子' });
      range = h('div', { class: 'stack', style: { gap: '8px' } },
        h('div', { class: 'row' }, segmented([['paint', '涂'], ['erase', '擦']], S.erase ? 'erase' : 'paint', (v) => { S.erase = v === 'erase'; }),
          btn('清空', () => { z.cells = []; refs.cellCount.textContent = '0 个格子'; touch(); }, 'ghost sm'), h('span', { class: 'spacer' }), refs.cellCount),
        h('div', { class: 'muted small', text: S.tool === 'brush' ? '在地图上按住拖动来涂格子，按住 Alt 临时擦除。' : '选左边的「自由涂抹」工具（B）继续涂。' }));
    } else if (z.kind === 'circle') {
      range = h('div', { class: 'form-grid' },
        h('div', { class: 'field' }, h('label', { text: '半径（米）' }), num(Math.round(z.radius) || 10, (v) => { z.radius = Math.min(500, Math.max(2, Math.round(Number(v) || 2))); touch(); })),
        h('div', { class: 'field' }, h('span', { class: 'label', text: '圆心' }), h('div', { class: 'muted small', style: { paddingTop: '9px' }, text: cal ? '游戏坐标 ' + hud(z.cx, z.cz).map(r0).join(':') : '—' })));
    } else if (z.kind === 'sea_zone') {
      range = h('div', { class: 'muted small', text: rangeText(z) });
    } else {
      const coords = h('div', { class: 'form-grid', style: { gridTemplateColumns: 'repeat(4,minmax(0,1fr))' } },
        [['gx0', '起点 X'], ['gz0', '起点 Z'], ['gx1', '终点 X'], ['gz1', '终点 Z']].map(([k, l]) => h('div', { class: 'field' }, h('label', { text: l }),
          num(z[k], (v) => { z[k] = Math.round(Number(v) || 0); rangeLine.textContent = rangeText(z); touch(); }))));
      const rangeLine = h('div', { class: 'muted small', text: rangeText(z) });
      range = h('div', { class: 'stack', style: { gap: '8px' } }, rangeLine,
        canDraw ? h('details', { class: 'advanced', style: { marginTop: 0 } }, h('summary', {}, '按网格数字调整'), h('div', { style: { paddingTop: '10px' } }, coords, h('div', { class: 'help small muted', style: { marginTop: '6px' }, text: '网格数字，一格 40 米。' })))
          : h('div', {}, coords, h('div', { class: 'muted small', style: { marginTop: '6px' }, text: '网格数字，一格 40 米。' })));
    }

    // 出鱼方式
    const modeHelp = h('div', { class: 'muted small' });
    const modeParams = h('div');
    const drawMode = () => {
      modeHelp.textContent = z.mode === 'exclusive' ? '玩家在区内钓鱼时，只会钓到下面名单里的鱼。名单为空表示区内钓不到鱼。' : '名单里的鱼在区内更常见，其他鱼照样会出现，只是少一些。';
      modeParams.replaceChildren(z.mode === 'boost' ? h('div', { class: 'form-grid', style: { marginTop: '10px' } },
        h('div', { class: 'field' }, h('label', { text: '名单里的鱼多几倍' }), num(z.boost, (v) => { z.boost = v.trim() === '' ? 3 : Number(v); touch(); }), h('div', { class: 'help', text: '默认 3' })),
        h('div', { class: 'field' }, h('label', { text: '其他鱼保留多少' }), num(z.keep_others, (v) => { z.keep_others = v.trim() === '' ? 0.35 : Number(v); touch(); }), h('div', { class: 'help', text: '0–1，默认 0.35' }))) : '');
    };
    drawMode();

    // 鱼种名单
    const chosen = h('div');
    const drawChosen = () => {
      chosen.replaceChildren(z.fish.length ? h('div', { class: 'card' }, h('div', { class: 'list' }, z.fish.map((f, i) => {
        const on = h('input', { type: 'checkbox', 'aria-label': '启用', onchange: (e) => { f.enabled = e.target.checked; touch(); } });
        on.checked = !!f.enabled;
        return h('div', { class: 'list-item', style: { padding: '7px 10px', gap: '8px' } },
          h('label', { class: 'toggle' }, on, h('span', { class: 'track' })),
          h('div', { class: 'grow', style: { minWidth: 0 } }, h('div', { class: 'title', style: { fontSize: '13px' }, text: fishName.get(f.fish_key) || f.fish_key }),
            fishName.has(f.fish_key) ? null : h('div', { class: 'sub', text: '不在这张图的鱼种里' })),
          h('input', { class: 'input num', style: { width: '64px', height: '28px' }, title: '权重：在名单内的相对多少', value: f.weight, oninput: (e) => { f.weight = Number(e.target.value) || 1; touch(); } }),
          h('button', { type: 'button', class: 'btn ghost icon sm', 'aria-label': '移除', onclick: () => { z.fish.splice(i, 1); touch(); drawChosen(); drawResults(); } }, svg('<path d="M18 6 6 18M6 6l12 12"/>', 14)));
      }))) : h('div', { class: 'muted small', style: { padding: '4px 0 8px' }, text: z.mode === 'exclusive' ? '还没有加鱼：保存后这个区域里钓不到鱼。' : '还没有加鱼。' }));
    };
    let q = '', allPool = false, seq = 0;
    const results = h('div', { class: 'list' });
    async function drawResults() {
      const my = ++seq;
      let items;
      if (allPool) {
        if (!q.trim()) { results.replaceChildren(h('div', { class: 'muted small', style: { padding: '6px 0' }, text: '输入鱼名，搜索全部鱼种。' })); return; }
        const list = await apiList('/api/fish/pool?limit=30&q=' + encodeURIComponent(q.trim())).catch(() => []);
        if (my !== seq) return;
        items = list.map((x) => ({ key: x.fish_key, name: x.name_zh || x.fish_key, w: 1 }));
      } else {
        const lq = q.trim().toLowerCase();
        items = (roster || []).filter((f) => !lq || `${f.fish_key} ${f.name_zh}`.toLowerCase().includes(lq)).map((f) => ({ key: f.fish_key, name: f.name_zh || f.fish_key, w: Number(f.base_weight) || 1 }));
      }
      items = items.filter((x) => !z.fish.some((f) => f.fish_key === x.key));
      results.replaceChildren(...(items.length ? items.slice(0, 40).map((x) => h('div', {
        class: 'list-item clickable', style: { padding: '6px 10px' },
        onclick: () => { fishName.set(x.key, x.name); z.fish.push({ fish_key: x.key, weight: x.w, enabled: true }); touch(); drawChosen(); drawResults(); },
      }, h('span', { class: 'grow small', text: x.name }), h('span', { class: 'muted small', text: '添加' }))) : [h('div', { class: 'muted small', style: { padding: '6px 0' }, text: allPool ? '没有找到' : '这张图的鱼都已经加进来了' })]));
    }
    drawChosen(); drawResults();
    const picker = h('div', { class: 'stack', style: { gap: '8px' } },
      h('input', { class: 'input', placeholder: '筛选鱼名', oninput: (e) => { q = e.target.value; drawResults(); } }),
      h('div', { class: 'card', style: { maxHeight: '200px', overflow: 'auto' } }, results),
      toggleEl({ checked: false, label: '也搜索其他水域的鱼', onChange: (v) => { allPool = v; drawResults(); } }));

    const save = btn('保存', saveZone, 'primary');
    const sec = (title, ...body) => h('div', { class: 'zsec' }, h('div', { class: 'zsec-title', text: title }), ...body);
    return h('div', { class: 'zside-edit' },
      h('div', { class: 'zside-head' },
        h('button', { type: 'button', class: 'btn ghost icon sm', 'aria-label': '返回列表', title: '返回列表', onclick: async () => { if (await canLeaveEdit()) closeEdit(); } }, svg('<path d="m15 6-6 6 6 6"/>', 16)),
        h('b', { class: 'grow', text: isNew ? '新区域' : '编辑区域' }),
        badge(KIND_NAME[z.kind] || z.kind),
        isNew ? null : h('span', { onclick: (e) => e.stopPropagation() }, toggleEl({ checked: z.enabled, label: z.enabled ? '启用' : '停用', onChange: (v) => { z.enabled = v; touch(); } }))),
      h('div', { class: 'zside-body' },
        sec('名称', refs.name),
        sec('范围', range),
        sec('出鱼方式', segmented([['exclusive', '独占'], ['boost', '增益']], z.mode, (v) => { z.mode = v; touch(); drawMode(); drawChosen(); }), modeHelp, modeParams),
        sec('区内的鱼', chosen, picker)),
      h('div', { class: 'zside-foot' },
        isNew ? null : btn('删除', () => removeZone(z), 'danger sm', { icon: 'trash' }),
        h('span', { class: 'spacer' }),
        btn('取消', async () => { if (await canLeaveEdit()) closeEdit(); }, 'ghost'),
        save));
  }

  async function saveZone() {
    const z = S.edit;
    if (!z) return;
    const body = { ...z, name: (z.name || '').trim(), fish: z.fish.filter((f) => f.fish_key) };
    if (body.kind === 'grid_cells') {
      const seen = new Set();
      body.cells = (body.cells || []).filter(([a, b]) => { const k = a + ',' + b; if (seen.has(k)) return false; seen.add(k); return true; });
      if (!body.cells.length) { toast('还没有涂任何格子。选左边的「自由涂抹」工具，在地图上按住拖动。', 'bad'); return; }
    } else if (body.kind === 'circle') {
      body.radius = Math.min(500, Math.max(2, Math.round(body.radius || 2)));
    } else if (body.kind === 'grid_rect') {
      [body.gx0, body.gx1] = [Math.min(body.gx0, body.gx1), Math.max(body.gx0, body.gx1)];
      [body.gz0, body.gz1] = [Math.min(body.gz0, body.gz1), Math.max(body.gz0, body.gz1)];
    }
    if (body.mode === 'exclusive' && !body.fish.some((f) => f.enabled)) {
      const ok = await confirmDialog({ title: '名单是空的', desc: '独占区的名单为空，保存后这块区域里钓不到任何鱼。确定吗？', confirm: '仍然保存' });
      if (!ok) return;
    }
    try {
      const res = await post('/api/fish/zones', body);
      toast('已保存，约 30 秒内生效');
      S.dirty = false;
      await reload();
      const saved = S.zones.find((x) => x.id === res.id);
      S.edit = saved ? JSON.parse(JSON.stringify(saved)) : null;
      S.sel = saved ? saved.id : 0;
      renderSide(); drawZones();
    } catch (e) { toast(errText(e), 'bad'); }
  }
  async function removeZone(z) {
    const ok = await confirmDialog({ title: `删除「${z.name || '#' + z.id}」？`, desc: '这块区域恢复按地图的鱼种出鱼。不能撤销。', confirm: '删除', danger: true });
    if (!ok) return;
    try {
      await post('/api/fish/zones/delete', { id: z.id });
      toast('已删除，约 30 秒内生效');
      S.dirty = false; S.edit = null; S.sel = 0;
      await reload();
      renderSide(); drawZones();
    } catch (e) { toast(errText(e), 'bad'); }
  }
  async function toggleEnabled(z, on) {
    try {
      await post('/api/fish/zones', { ...z, fish: z.fish || [], enabled: on });
      toast(on ? '已启用，约 30 秒内生效' : '已停用，约 30 秒内生效');
      await reload();
      renderSide(); drawZones();
    } catch (e) { toast(errText(e), 'bad'); renderSide(); }
  }

  renderSide();
  applyTool('rect');
  let mapArea;
  if (canDraw) mapArea = mapBox;
  else if (kind === 'sea') mapArea = h('div', {}, callout('info', '挪威海不划分鱼群区域，按整张图的鱼种出鱼。'));
  else mapArea = h('div', {}, callout('info', '这张地图没有可用的纸地图，无法在图上画。可以在右边按网格数字新建矩形区域。'));
  return h('div', { class: 'zmap' + (canDraw ? '' : ' no-map') }, mapArea, side);
}

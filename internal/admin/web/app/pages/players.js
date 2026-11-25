// 玩家：左边找人，右边看和改这个人的一切。
import {
  h, api, apiList, post, icon, toast, errText, pageHead, section, callout, badge, empty, btn, loading, Form, savebar, setLeaveGuard,
  tabs, searchBox, debounce, confirmDialog, dialog, fmt, timeText, relTime, navigate, segmented, hideSavebar, setPageCleanup, replaceHash,
} from '../core.js';
import { itemIcon } from '../widgets.js';

const TABS = [['profile', '概况'], ['items', '背包'], ['keepnet', '护鱼网'], ['records', '图鉴补录'], ['skills', '技能'], ['mail', '邮箱']];
const GRADE = ['未钓到', '钓到过', '达标', '星级', '蓝冠'];
const cents = (v) => (Number(v || 0) / 100).toFixed(2).replace(/\.00$/, '');
let QUERY = '';

export default async function players(params) {
  const [uidParam, tabParam] = params;
  const listBox = h('div', { class: 'list' }, loading(4));
  const detail = h('div', {});
  const root = h('div', { class: 'page' },
    pageHead('玩家', '查看和修改角色的等级、货币、背包、护鱼网、图鉴和技能。', btn('刷新', () => loadList(), 'sm', { icon: 'refresh' })),
    h('div', { class: 'master-detail' },
      h('div', { class: 'card master' },
        h('div', { style: { padding: '12px' } }, searchBox('搜索昵称或 UID', debounce((v) => { QUERY = v; loadList(); }, 250), QUERY)),
        listBox),
      detail));
  let list = [];
  async function loadList() {
    try {
      list = await apiList('/api/players?q=' + encodeURIComponent(QUERY));
      listBox.replaceChildren(...(list.length ? list.map((p) => h('div', {
        class: 'list-item clickable' + (String(p.uid) === String(uidParam) ? ' on' : ''),
        onclick: () => navigate('players', p.uid),
      }, h('div', { class: 'avatar', text: (p.name || '?').slice(0, 1) }),
      h('div', { class: 'grow' }, h('div', { class: 'title', text: p.name }), h('div', { class: 'sub', text: `UID ${p.uid} · ${p.level} 级` })),
      p.banned ? badge('已封禁', 'bad') : p.online ? badge('在线', 'ok', true) : null)) : [empty(QUERY ? '没有找到' : '还没有玩家', QUERY ? '换个关键词试试。' : '玩家第一次登录游戏后会出现在这里。')]));
      return list;
    } catch (e) { listBox.replaceChildren(empty('读取失败', errText(e))); return []; }
  }
  const loaded = await loadList();
  const timer = setInterval(() => { if (!document.hidden) loadList(); }, 8000);
  setPageCleanup(() => clearInterval(timer));
  if (!uidParam) {
    if (loaded.length) { setTimeout(() => navigate('players', loaded[0].uid), 0); }
    detail.replaceChildren(h('div', { class: 'card' }, empty('选择一个玩家', '在左边的列表里点一个玩家。')));
    return root;
  }
  detail.replaceChildren(h('div', { class: 'card card-pad' }, loading(5)));
  try {
    const p = await api('/api/players/detail?uid=' + encodeURIComponent(uidParam));
    detail.replaceChildren(playerDetail(p, TABS.some(([k]) => k === tabParam) ? tabParam : 'profile'));
  } catch (e) { detail.replaceChildren(h('div', { class: 'card' }, empty('读取玩家失败', errText(e)))); }
  return root;
}

function playerDetail(p, tab) {
  const reload = () => navigate('players', p.uid, current);
  let current = tab;
  let tabDirty = () => false;
  const pane = h('div');
  const show = async (t) => {
    if (t === current && pane.childNodes.length) return false;
    if (tabDirty()) {
      const ok = await confirmDialog({ title: '有修改还没保存', desc: '切换标签会放弃这些修改。', confirm: '放弃并切换' });
      if (!ok) return false;
    }
    current = t;
    tabDirty = () => false;
    replaceHash('#players/' + p.uid + '/' + t);
    hideSavebar(); setLeaveGuard(null);
    const builders = { profile, items, keepnet, records, skills, mail };
    pane.replaceChildren(builders[t](p, reload, (fn) => { tabDirty = fn; }));
  };
  const head = h('div', { class: 'row', style: { alignItems: 'flex-start', marginBottom: '6px' } },
    h('div', { class: 'avatar', style: { width: '44px', height: '44px', fontSize: '18px' }, text: (p.name || '?').slice(0, 1) }),
    h('div', { class: 'grow' },
      h('div', { class: 'row' }, h('h2', { style: { fontSize: '19px' }, text: p.name }), p.banned ? badge('已封禁', 'bad') : null),
      h('div', { class: 'muted small', text: `UID ${p.uid} · ${p.level} 级 · 经验 ${fmt(p.xp)}` })),
    h('div', { class: 'row' },
      btn('发邮件', () => navigate('mail', p.uid), 'sm', { icon: 'mail' }),
      btn('踢下线', async () => {
        if (!(await confirmDialog({ title: `把「${p.name}」踢下线？`, desc: '玩家会被断开连接，可以马上重新登录。', confirm: '踢下线' }))) return;
        try { await post('/api/players/kick', { uid: p.uid }); toast('已踢下线'); } catch (e) { toast(errText(e), 'bad'); }
      }, 'sm'),
      btn(p.banned ? '解除封禁' : '封禁', async () => {
        const ban = !p.banned;
        if (!(await confirmDialog({ title: ban ? `封禁「${p.name}」？` : `解除对「${p.name}」的封禁？`, desc: ban ? '封禁后这个玩家无法登录游戏。' : '解除后玩家可以正常登录。', confirm: ban ? '封禁' : '解除封禁', danger: ban }))) return;
        try { await post('/api/players/ban', { uid: p.uid, banned: ban }); toast(ban ? '已封禁' : '已解除封禁'); reload(); } catch (e) { toast(errText(e), 'bad'); }
      }, p.banned ? 'sm' : 'danger sm')));
  const counts = { items: p.item_count, keepnet: p.keepnet_count, mail: p.mail_count };
  const card = h('div', { class: 'card card-pad' }, head,
    tabs(TABS.map(([k, l]) => [k, l, counts[k]]), tab, show), pane);
  show(tab);
  return card;
}

// ---------- 概况 ----------
function profile(p, reload, setDirty) {
  const form = new Form();
  savebar(form, {
    onSave: async (v) => {
      const body = { uid: p.uid, name: form.value('name'), level: +form.value('level'), xp: +form.value('xp'),
        silver: Math.round(+form.value('silver') * 100), gold: Math.round(+form.value('gold') * 100), bank: Math.round(+form.value('bank') * 100) };
      const m = +memberField.input.value;
      const exp = m === 1 && expInput.value ? Math.floor(new Date(expInput.value).getTime() / 1000) : 0;
      if ('membership' in v || 'membership_exp' in v) {
        if (m === 1 && (!exp || exp <= Date.now() / 1000)) throw new Error('限时会员需要选一个未来的到期时间');
        body.membership = m; body.membership_exp = exp;
      }
      await post('/api/players/update', body);
      toast('已保存。玩家在线时会立即看到变化。');
      setTimeout(reload, 400);
    },
  });
  setLeaveGuard(() => form.isDirty());
  setDirty(() => form.isDirty());
  // 会员：分段选择 + 到期时间
  const memberField = form.field({ key: 'membership', label: '会员', kind: 'string' }, String(p.membership || 0));
  memberField.input = h('input', { type: 'hidden', value: memberField.original });
  const toLocal = (ts) => { if (!ts) return ''; const d = new Date(ts * 1000); d.setMinutes(d.getMinutes() - d.getTimezoneOffset()); return d.toISOString().slice(0, 16); };
  const expField = form.field({ key: 'membership_exp', label: '到期', kind: 'string' }, toLocal(p.membership_exp));
  const expInput = h('input', { type: 'datetime-local', class: 'input', value: expField.original, style: { width: '240px' }, oninput: () => form.refresh() });
  expField.input = expInput;
  const expWrap = h('div', { class: 'row', hidden: String(p.membership) !== '1' }, h('span', { class: 'muted small', text: '到期时间' }), expInput);
  const seg = segmented([['0', '普通'], ['1', '限时会员'], ['2', '永久会员']], String(p.membership || 0), (v) => { memberField.input.value = v; expWrap.hidden = v !== '1'; form.refresh(); });
  const hh = p.happy_hour || {};
  const hhText = hh.enabled === false ? '欢乐时光已在「玩法规则」里关闭。' : !hh.qualified ? '开通会员后可以享受欢乐时光加成。'
    : hh.unlimited ? '永久会员，欢乐时光一直有效。' : `欢乐时光有效，剩余约 ${Math.ceil((hh.remaining_seconds || 0) / 60)} 分钟。`;
  return h('div', { class: 'stack', style: { paddingTop: '4px' } },
    h('div', {},
      h('h3', { class: 'small', style: { color: 'var(--muted)', margin: '4px 0 4px' }, text: '角色' }),
      form.row({ key: 'name', label: '昵称', kind: 'string', required: true }, p.name),
      form.row({ key: 'level', label: '等级', kind: 'int', min: 1, max: 58, unit: '级' }, p.level),
      form.row({ key: 'xp', label: '累计经验', kind: 'int', min: 0, help: '等级和经验要对得上，否则游戏里经验条会显示异常。' }, p.xp)),
    h('div', {},
      h('h3', { class: 'small', style: { color: 'var(--muted)', margin: '8px 0 4px' }, text: '货币' }),
      form.row({ key: 'silver', label: '银币', kind: 'float', min: 0, help: '按游戏里显示的数字填写，可以有两位小数。' }, cents(p.silver)),
      form.row({ key: 'gold', label: '金币', kind: 'float', min: 0 }, cents(p.gold)),
      form.row({ key: 'bank', label: '银行存款', kind: 'float', min: 0 }, cents(p.bank))),
    h('div', {},
      h('h3', { class: 'small', style: { color: 'var(--muted)', margin: '8px 0 10px' }, text: '会员' }),
      h('div', { class: 'stack' }, h('div', { class: 'row' }, seg, expWrap), h('div', { class: 'muted small', text: hhText }))));
}

// ---------- 背包 ----------
function items(p, reload) {
  let q = '';
  const body = h('tbody');
  const draw = () => {
    const rows = (p.items || []).filter((x) => !q || `${x.item_id} ${x.name} ${x.guid}`.toLowerCase().includes(q));
    body.replaceChildren(...(rows.length ? rows.map((x) => h('tr', {},
      h('td', {}, h('div', { class: 'row', style: { flexWrap: 'nowrap' } }, itemIcon(x.item_id), h('div', {}, h('b', { text: x.name || x.item_id }), h('span', { class: 'sub mono', text: x.item_id })))),
      h('td', { text: x.container === 'equip' ? '装备中' : x.container === 'bag' ? '背包' : (x.container || '') }),
      h('td', { class: 'num', text: fmt(x.qty) }),
      h('td', { class: 'num', text: x.durability === undefined || x.durability === null ? '—' : Number(x.durability) <= 1 ? (Number(x.durability) * 100).toFixed(1) + '%' : String(x.durability) }),
      h('td', { class: 'num' }, btn('删除', async () => {
        if (!(await confirmDialog({ title: `删除「${x.name || x.item_id}」？`, desc: '从这个玩家的背包或装备里删除，不能恢复。', confirm: '删除', danger: true }))) return;
        try { await post('/api/players/items/delete', { uid: p.uid, guids: [x.guid] }); toast('已删除，约 2 秒后生效'); setTimeout(reload, 2300); } catch (e) { toast(errText(e), 'bad'); }
      }, 'ghost sm')))) : [h('tr', {}, h('td', { colspan: 5 }, empty(q ? '没有匹配的物品' : '背包是空的')))]));
  };
  draw();
  return h('div', { class: 'stack' },
    searchBox('筛选物品名称或 ID', (v) => { q = v.toLowerCase(); draw(); }),
    h('div', { class: 'table-wrap', style: { maxHeight: '62vh' } }, h('table', { class: 'table' },
      h('thead', {}, h('tr', {}, h('th', { text: '物品' }), h('th', { text: '位置' }), h('th', { class: 'num', text: '数量' }), h('th', { class: 'num', text: '耐久' }), h('th', {}))), body)));
}

// ---------- 护鱼网 ----------
function keepnet(p, reload) {
  const rows = p.keepnet || [];
  return h('div', { class: 'stack' },
    h('div', { class: 'row' }, h('span', { class: 'grow muted small', text: `共 ${rows.length} 条鱼。删除后统计和鱼市会同步更新。` }), btn('放入一条鱼', () => addFish(p, reload), 'primary sm', { icon: 'plus' })),
    rows.length ? h('div', { class: 'table-wrap', style: { maxHeight: '62vh' } }, h('table', { class: 'table' },
      h('thead', {}, h('tr', {}, ['鱼', '重量', '档位', '地点', '饵', '时间', ''].map((t, i) => h('th', { class: i === 1 ? 'num' : '', text: t })))),
      h('tbody', {}, rows.map((x) => h('tr', {},
        h('td', {}, h('b', { text: x.name_zh || x.species }), h('span', { class: 'sub mono', text: x.species })),
        h('td', { class: 'num', text: fmt(x.weight_g) + ' g' }),
        h('td', {}, badge(GRADE[x.cat] || ('档位 ' + x.cat), x.cat >= 4 ? 'accent' : x.cat >= 3 ? 'warn' : '')),
        h('td', { text: x.keep_level || x.keep_loc || '—' }),
        h('td', { class: 'mono small', text: x.keep_bait || '—' }),
        h('td', { class: 'small muted', text: timeText(x.ts) }),
        h('td', {}, btn('删除', async () => {
          if (!(await confirmDialog({ title: '从护鱼网删除这条鱼？', desc: '不能恢复。', confirm: '删除', danger: true }))) return;
          try { await post('/api/players/keepnet/delete', { uid: p.uid, guids: [x.echo_guid] }); toast('已删除，约 2 秒后生效'); setTimeout(reload, 2300); } catch (e) { toast(errText(e), 'bad'); }
        }, 'ghost sm'))))))) : h('div', { class: 'card' }, empty('护鱼网是空的')));
}
function addFish(p, reload) {
  const form = new Form();
  const results = h('div', { class: 'card', style: { maxHeight: '200px', overflow: 'auto' } });
  const species = form.stacked({ key: 'species', label: '鱼种', kind: 'string', required: true, placeholder: '搜索鱼名，如 拟鲤', span: true }, '');
  const speciesInput = form.fields.get('species').input;
  const hint = h('div', { class: 'help' });
  speciesInput.addEventListener('input', debounce(async () => {
    const q = speciesInput.value.trim();
    if (!q) { results.replaceChildren(); return; }
    const list = await apiList('/api/fish/pool?limit=30&q=' + encodeURIComponent(q)).catch(() => []);
    results.replaceChildren(list.length ? h('div', { class: 'list' }, list.map((f) => h('div', { class: 'list-item clickable', onclick: () => { speciesInput.value = f.fish_key; hint.textContent = `${f.name_zh}，常见 ${fmt(f.min_g)}–${fmt(f.max_g)} g`; results.replaceChildren(); } },
      h('div', { class: 'grow' }, h('div', { class: 'title', text: f.name_zh }), h('div', { class: 'sub mono', text: f.fish_key })), h('span', { class: 'muted small', text: `${fmt(f.min_g)}–${fmt(f.max_g)} g` })))) : empty('没有找到', '换个鱼名或 ID 试试。'));
  }, 250));
  dialog({
    title: '放入护鱼网', size: 'lg',
    body: h('div', { class: 'stack' }, species, results, hint, h('div', { class: 'form-grid' },
      form.stacked({ key: 'weight_g', label: '重量（克）', kind: 'int', min: 1 }, '500'),
      form.stacked({ key: 'cat', label: '档位', kind: 'select', options: [['1', '普通'], ['2', '达标'], ['3', '奖杯'], ['4', '蓝冠']] }, '1'),
      form.stacked({ key: 'keep_level', label: '地点（可选）', kind: 'string', placeholder: '例如 level_001_lake' }, ''),
      form.stacked({ key: 'keep_bait', label: '用的饵（可选）', kind: 'string', placeholder: '例如 worm_01' }, ''))),
    actions: [{ label: '取消', kind: 'ghost' }, {
      label: '放入', kind: 'primary', onClick: async () => {
        if (!form.validate(['species', 'weight_g'])) return true;
        await post('/api/players/keepnet/add', { uid: p.uid, species: form.value('species'), weight_g: +form.value('weight_g'), cat: +form.value('cat'), keep_level: form.value('keep_level'), keep_loc: '', keep_spot: 0, keep_bait: form.value('keep_bait') });
        toast('已放入，约 2 秒后生效'); setTimeout(reload, 2300);
      },
    }],
  });
}

// ---------- 图鉴补录 ----------
function records(p, reload, setDirty) {
  const box = h('div', {}, loading(5));
  let data = null, level = '', q = '';
  const changes = new Map();
  setLeaveGuard(() => changes.size > 0);
  setDirty(() => changes.size > 0);
  const saveBtn = btn('保存补录', async () => {
    saveBtn.disabled = true;
    try {
      const r = await post('/api/players/records', { uid: p.uid, level, changes: [...changes.values()] });
      toast('已保存，并自动备份了存档（' + r.backup + '）');
      changes.clear(); await load(level);
    } catch (e) { toast(errText(e), 'bad'); saveBtn.disabled = false; }
  }, 'primary', { disabled: true });
  const sync = () => { saveBtn.disabled = !changes.size; saveBtn.textContent = changes.size ? `保存补录（${changes.size}）` : '保存补录'; };
  async function load(lv) {
    box.replaceChildren(loading(5));
    try { data = await api('/api/players/records?uid=' + p.uid + '&level=' + encodeURIComponent(lv || '')); level = data.level; draw(); }
    catch (e) { box.replaceChildren(empty('读取失败', errText(e))); }
  }
  function draw() {
    const mapSel = h('select', { class: 'select', style: { width: '220px' }, onchange: async (e) => {
      if (changes.size && !(await confirmDialog({ title: '这张地图的修改还没保存', desc: '切换地图会放弃这些修改。', confirm: '放弃并切换' }))) { e.target.value = level; return; }
      changes.clear(); sync(); load(e.target.value);
    } }, data.maps.map((m) => h('option', { value: m.level, text: m.name, selected: m.level === data.level })));
    const tbody = h('tbody');
    const rows = () => tbody.replaceChildren(...data.fish.filter((f) => !q || `${f.name} ${f.fish_key}`.toLowerCase().includes(q)).map((f) => {
      const c = changes.get(f.fish_key) || { fish_key: f.fish_key, grade: f.restored_grade || 0, weight_g: f.restored_weight_g || 0 };
      const sel = h('select', { class: 'select', style: { height: '30px' } }, [0, 1, 2, 3, 4].map((g) => h('option', { value: g, text: g === 0 ? '不补录' : GRADE[g], selected: +c.grade === g })));
      const w = h('input', { class: 'input num', style: { height: '30px', width: '130px' }, placeholder: '自动', value: c.weight_g || '' });
      const edit = () => { changes.set(f.fish_key, { fish_key: f.fish_key, grade: +sel.value, weight_g: +w.value || 0 }); sync(); };
      sel.addEventListener('change', () => { w.value = ''; edit(); });
      w.addEventListener('input', edit);
      return h('tr', {}, h('td', {}, h('b', { text: f.name }), h('span', { class: 'sub mono', text: f.fish_key })),
        h('td', {}, badge(GRADE[f.grade] || '未钓到', f.grade >= 4 ? 'accent' : f.grade >= 3 ? 'warn' : f.grade > 0 ? 'ok' : '')),
        h('td', { class: 'num', text: f.weight_g ? fmt(f.weight_g) + ' g' : '—' }), h('td', {}, sel), h('td', {}, w));
    }));
    rows();
    box.replaceChildren(h('div', { class: 'stack' },
      callout('info', '用于补齐图鉴。补录算一次钓获，但不给经验、货币，也不放进护鱼网。真实钓到的记录不会被覆盖。保存前会自动备份存档。建议在玩家下线时补录。'),
      h('div', { class: 'row' }, mapSel, h('div', { class: 'grow' }, searchBox('筛选鱼名', (v) => { q = v.toLowerCase(); rows(); })), saveBtn),
      h('div', { class: 'table-wrap', style: { maxHeight: '58vh' } }, h('table', { class: 'table' },
        h('thead', {}, h('tr', {}, h('th', { text: '鱼' }), h('th', { text: '现在' }), h('th', { class: 'num', text: '最重' }), h('th', { text: '补录为' }), h('th', { text: '补录重量（克）' }))), tbody))));
  }
  load('');
  return box;
}

// ---------- 技能 ----------
function skills(p, reload, setDirty) {
  const list = p.skills || [];
  const avail = Number(p.available_points ?? 0);
  const unsaved = new Set();
  setLeaveGuard(() => unsaved.size > 0);
  setDirty(() => unsaved.size > 0);
  const points = h('input', { class: 'input num', style: { width: '100px' }, value: avail, oninput: () => { unsaved.add('points'); } });
  return h('div', { class: 'stack' },
    h('div', { class: 'card', style: { padding: '14px 16px', background: 'var(--surface-2)' } }, h('div', { class: 'row' },
      h('div', { class: 'grow' }, h('b', { text: '未分配的技能点' }), h('div', { class: 'muted small', text: `等级累计获得 ${p.skill_total ?? 0} 点，已经用掉 ${p.skill_spent ?? 0} 点。` })),
      points, btn('保存', async () => {
        try { await post('/api/players/skills/points', { uid: p.uid, points: Math.max(0, Math.floor(+points.value || 0)) }); unsaved.delete('points'); toast('已保存'); } catch (e) { toast(errText(e), 'bad'); }
      }, 'primary sm'))),
    h('p', { class: 'muted small', text: '下面是每种钓法的熟练度（0–100%）。' }),
    h('div', { class: 'grid-2' }, list.map((s) => {
      const v = Number(s.points) || 0;
      const out = h('output', { text: v.toFixed(1) + '%' });
      const range = h('input', { type: 'range', min: 0, max: 100, step: 0.1, value: v, oninput: (e) => { out.textContent = Number(e.target.value).toFixed(1) + '%'; save.hidden = false; unsaved.add(s.skill_id); } });
      const save = btn('保存', async () => {
        try { await post('/api/players/skills/update', { uid: p.uid, skill_id: s.skill_id, points: +range.value }); unsaved.delete(s.skill_id); toast(`已保存「${s.name}」`); save.hidden = true; } catch (e) { toast(errText(e), 'bad'); }
      }, 'primary sm', { hidden: true });
      return h('div', { class: 'card', style: { padding: '12px 14px', background: 'var(--surface-2)' } },
        h('div', { class: 'row', style: { justifyContent: 'space-between' } }, h('b', { text: s.name }), save),
        h('div', { class: 'range-row', style: { marginTop: '6px' } }, range, out));
    })),
    h('div', {}, btn('全部清零', async () => {
      if (!(await confirmDialog({ title: '把所有熟练度清零？', desc: '所有钓法的熟练度都会变成 0%，不能恢复。', confirm: '清零', danger: true }))) return;
      try { for (const s of list) await post('/api/players/skills/update', { uid: p.uid, skill_id: s.skill_id, points: 0 }); unsaved.clear(); toast('已清零'); reload(); } catch (e) { toast(errText(e), 'bad'); }
    }, 'danger sm')));
}

// ---------- 邮箱 ----------
function mail(p, reload) {
  const rows = p.mails || [];
  return h('div', { class: 'stack' },
    h('div', { class: 'row' }, h('span', { class: 'grow muted small', text: '玩家自己删不掉领不出附件的坏邮件，可以在这里删掉后重新发。' }), btn('给他发邮件', () => navigate('mail', p.uid), 'primary sm', { icon: 'mail' })),
    rows.length ? h('div', { class: 'table-wrap', style: { maxHeight: '62vh' } }, h('table', { class: 'table' },
      h('thead', {}, h('tr', {}, h('th', { text: '内容' }), h('th', { text: '类型' }), h('th', { text: '状态' }), h('th', { text: '时间' }), h('th', {}))),
      h('tbody', {}, rows.map((m) => h('tr', {},
        h('td', { style: { whiteSpace: 'normal', maxWidth: '420px' }, text: m.text || '（只有附件）' }),
        h('td', { text: m.kind ? '带附件' : '文字' }),
        h('td', {}, m.read ? badge('已读') : badge('未读', 'accent')),
        h('td', { class: 'small muted', title: timeText(m.created_ts), text: relTime(m.created_ts) }),
        h('td', {}, btn('删除', async () => {
          if (!(await confirmDialog({ title: '删除这封邮件？', desc: '附件会一起丢弃，不能恢复。', confirm: '删除', danger: true }))) return;
          try { await post('/api/players/mail/delete', { uid: p.uid, ids: [m.id] }); toast('已删除'); setTimeout(reload, 300); } catch (e) { toast(errText(e), 'bad'); }
        }, 'ghost sm'))))))) : h('div', { class: 'card' }, empty('邮箱是空的')));
}

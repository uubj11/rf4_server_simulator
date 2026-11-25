// 水域与鱼种：每个水域出哪些鱼、各占多少、咬钩快慢。
import { zonesEditor } from './zones.js';
import {
  h, api, apiList, post, toast, errText, pageHead, section, callout, badge, btn, empty, loading, searchBox, debounce, dialog,
  confirmDialog, navigate, Form, savebar, setLeaveGuard, render, tabs, webURL, fmt,
} from '../core.js';

const ORDER = ['克马羚诺也湖', '埃尔克湖', '惟有诺克河', '旧奥斯特罗格湖', '白河', '廓里湖', '梅德韦杰湖', '沃尔霍夫河', '北顿涅茨河', '苏拉河', '拉多加湖', '琥珀湖', '拉多加湖群岛', '阿赫图巴河', '铜湖', '下通古斯卡河', '亚马河', '挪威海'];

export function fishIcon(key, w = 64) {
  const img = h('img', { src: webURL('/icons/fish/' + encodeURIComponent(String(key || '').toLowerCase().replace(/\./g, '_')) + '.png'), alt: '', loading: 'lazy', style: { width: w + 'px', height: (w / 2) + 'px', objectFit: 'contain', flex: 'none' } });
  img.onerror = () => { img.style.display = 'none'; };
  return img;
}

export default async function waters(params) {
  const [areas, zonesData] = await Promise.all([api('/api/fish/areas'), api('/api/fish/zones').catch(() => ({ zones: [] }))]);
  const list = (Array.isArray(areas) ? areas : []).slice().sort((a, b) => {
    const ia = ORDER.indexOf(a.loc), ib = ORDER.indexOf(b.loc);
    return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib);
  });
  const zones = zonesData.zones || [];
  const loc = params[0] && list.some((a) => a.loc === params[0]) ? params[0] : (list[0] && list[0].loc);
  const tab = params[1] === 'zones' ? 'zones' : 'fish';
  const side = h('div', { class: 'card master' }, h('div', { class: 'list' }, list.map((a) => {
    const n = zones.filter((z) => z.loc === a.loc).length;
    return h('div', { class: 'list-item clickable' + (a.loc === loc ? ' on' : ''), onclick: () => navigate('waters', a.loc, tab) },
      h('div', { class: 'grow' }, h('div', { class: 'title', text: a.loc }), h('div', { class: 'sub', text: `${a.fish_count} 种鱼` + (n ? ` · ${n} 个鱼群区域` : '') })),
      a.map_kind === 'sea' ? badge('海图') : null);
  })));
  const main = h('div', {}, h('div', { class: 'card card-pad' }, loading(6)));
  const root = h('div', { class: 'page' },
    pageHead('水域与鱼种', '每个水域出哪些鱼、各占多少，以及咬钩的快慢。'),
    h('div', { class: 'master-detail', style: { gridTemplateColumns: '240px minmax(0,1fr)' } }, side, main));
  if (!loc) { main.replaceChildren(h('div', { class: 'card' }, empty('没有水域数据'))); return root; }
  const tabBar = tabs([['fish', '鱼种'], ['zones', '鱼群区域', zones.filter((z) => z.loc === loc).length]], tab, (t) => navigate('waters', loc, t));
  if (tab === 'zones') {
    // 画地图需要空间：收起左侧水域列表，改用标题栏的下拉选择
    const cfg = await api('/api/fish/config?loc=' + encodeURIComponent(loc)).catch(() => ({ fish: [] }));
    const area = list.find((a) => a.loc === loc);
    const picker = h('select', { class: 'select', style: { width: '220px' }, 'aria-label': '选择水域', onchange: (e) => navigate('waters', e.target.value, 'zones') },
      list.map((a) => h('option', { value: a.loc, text: a.loc + (a.map_kind === 'sea' ? '（海图）' : ''), selected: a.loc === loc })));
    return h('div', { class: 'page' },
      pageHead('水域与鱼种', '在地图上画出一块区域，规定那里出哪些鱼。', picker),
      h('div', { class: 'card card-pad' }, tabBar,
        zonesEditor({ area, zones: zones.filter((z) => z.loc === loc), roster: cfg.fish || [] })));
  }
  try {
    const [cfg, prefs] = await Promise.all([
      api('/api/fish/config?loc=' + encodeURIComponent(loc)),
      api('/api/bait/prefs?limit=2000&loc=' + encodeURIComponent(loc)).catch(() => ({ rows: [] })),
    ]);
    const baitByKey = Object.fromEntries((prefs.rows || []).map((r) => [r.fish_key, r]));
    main.replaceChildren(h('div', { class: 'card card-pad' }, h('h2', { style: { fontSize: '17px', marginBottom: '10px' }, text: loc }), tabBar, fishView(cfg, baitByKey)));
  } catch (e) { main.replaceChildren(h('div', { class: 'card' }, empty('读取失败', errText(e)))); }
  return root;
}

// ---------- 鱼种名册 ----------
// 爱吃的饵：静态规则来自鱼饵偏好；轮换只认名册行里后端给的 bait_groups / bait_rotate_sec
function baitCell(row, pref) {
  const allow = (pref && pref.allow) || [];
  const groups = (row.bait_groups || []).filter((g) => g && g.length);
  const rotating = groups.length > 1 && Number(row.bait_rotate_sec) > 0;
  return h('td', { style: { whiteSpace: 'normal', maxWidth: '320px' } },
    allow.length ? h('div', { class: 'small text-2', text: allow.slice(0, 5).join('、') + (allow.length > 5 ? ` 等 ${allow.length} 种` : '') }) : h('span', { class: 'muted small', text: '没有设置' }),
    rotating ? h('div', { style: { marginTop: '4px' } }, badge(`轮换 ${groups.length} 组 · 每 ${Math.round(row.bait_rotate_sec / 60)} 分钟${row.bait_rotate_mode === 'strict' ? ' · 严格' : ''}`, 'accent')) : null);
}

function fishView(cfg, baitByKey = {}) {
  const fish = cfg.fish.map((f) => ({ ...f }));
  const s = cfg.setting || {};
  const form = new Form();
  let dirtyRows = false;
  const isDirty = () => form.isDirty() || dirtyRows;
  const bar = savebar(form, {
    onSave: async () => {
      const setting = { ...s,
        bite_probability: +form.value('bite_probability'), bite_delay_fixed: +form.value('bite_delay_fixed'), bite_delay_scale: +form.value('bite_delay_scale'),
        bite_delay_max: +form.value('bite_delay_max'), bite_delay_lure_max: +form.value('bite_delay_lure_max') };
      await post('/api/fish/config', { area: cfg.area, setting, fish });
      dirtyRows = false;
      toast('已保存，立即生效');
      setTimeout(render, 200);
    },
  });
  // 鱼种表的改动不经过 Form 字段，手动把它算进「未保存」
  const origDirty = form.dirtyKeys.bind(form);
  form.dirtyKeys = () => { const k = origDirty(); return dirtyRows && !k.length ? ['_fish'] : k; };
  const origValues = form.values.bind(form);
  form.values = (keys) => origValues((keys || []).filter((k) => k !== '_fish'));
  const origReset = form.reset.bind(form);
  form.reset = () => { dirtyRows = false; origReset(); render(); };
  setLeaveGuard(isDirty);
  const touch = () => { dirtyRows = true; form.refresh(); };

  let q = '';
  const tbody = h('tbody');
  const total = () => fish.filter((f) => f.enabled && Number(f.weight) > 0).reduce((n, f) => n + Number(f.weight), 0) || 1;
  const draw = () => {
    const sum = total();
    const rows = fish.filter((f) => !q || `${f.fish_key} ${f.name_zh}`.toLowerCase().includes(q));
    tbody.replaceChildren(...(rows.length ? rows.map((f) => {
      const on = h('input', { type: 'checkbox', 'aria-label': '启用 ' + f.name_zh, onchange: (e) => { f.enabled = e.target.checked; touch(); draw(); } });
      on.checked = !!f.enabled;
      const w = h('input', { class: 'input num', style: { width: '90px', height: '30px' }, value: f.weight, onchange: (e) => { const n = Number(e.target.value); f.weight = e.target.value.trim() !== '' && Number.isFinite(n) ? n : f.base_weight; f.has_override = true; touch(); draw(); } });
      const pct = f.enabled && Number(f.weight) > 0 ? (Number(f.weight) / sum * 100) : 0;
      return h('tr', { style: { opacity: f.enabled ? 1 : 0.5 } },
        h('td', {}, h('label', { class: 'toggle' }, on, h('span', { class: 'track' }))),
        h('td', {}, h('div', { class: 'row', style: { flexWrap: 'nowrap' } }, fishIcon(f.fish_key), h('div', {}, h('b', { text: f.name_zh }), h('span', { class: 'sub mono', text: f.fish_key })))),
        baitCell(f, baitByKey[f.fish_key]),
        h('td', {}, w, h('span', { class: 'sub', text: '默认 ' + f.base_weight })),
        h('td', { style: { width: '220px' } }, h('div', { class: 'row', style: { flexWrap: 'nowrap' } },
          h('div', { style: { flex: 1, height: '6px', borderRadius: '3px', background: 'var(--surface-4)', overflow: 'hidden' } }, h('div', { style: { width: Math.min(100, pct * 3) + '%', height: '100%', background: 'var(--accent)' } })),
          h('span', { class: 'small', style: { width: '52px', textAlign: 'right' }, text: pct ? pct.toFixed(1) + '%' : '—' }))),
        h('td', {}, f.has_override ? badge('改过', 'warn') : h('span', { class: 'muted small', text: '默认' })));
    }) : [h('tr', {}, h('td', { colspan: 6 }, empty('没有匹配的鱼')))]));
  };
  draw();

  const params = h('div', {},
    form.row({ key: 'bite_probability', label: '每次抛竿有鱼的概率', help: '1 表示每竿必有鱼咬。', kind: 'float', min: 0, max: 1, def: '1' }, s.bite_probability),
    form.row({ key: 'bite_delay_scale', label: '咬钩速度', help: '0.5 = 快一倍，2 = 慢一倍。', kind: 'float', min: 0, unit: '倍', def: '1' }, s.bite_delay_scale),
    form.row({ key: 'bite_delay_max', label: '最长等待', help: '普通钓法等这么久一定有鱼咬。', kind: 'float', min: 1, unit: '秒', def: '20' }, s.bite_delay_max),
    form.row({ key: 'bite_delay_lure_max', label: '路亚最长等待', kind: 'float', min: 0.1, unit: '秒', def: '6' }, s.bite_delay_lure_max),
    form.row({ key: 'bite_delay_fixed', label: '固定咬钩时间', help: '0 表示随机；填了数字后每次都在这个时间咬钩。', kind: 'float', min: 0, unit: '秒', def: '0' }, s.bite_delay_fixed));

  return h('div', { class: 'stack' },
    h('details', { class: 'advanced', style: { borderTop: 0, marginTop: '-6px' } }, h('summary', {}, '这个水域的咬钩快慢 ', h('small', { text: `（现在：${Number(s.bite_delay_fixed) > 0 ? '固定 ' + s.bite_delay_fixed + ' 秒' : '最长 ' + s.bite_delay_max + ' 秒'}）` })), params),
    h('div', { class: 'row' },
      h('div', { class: 'grow', style: { maxWidth: '300px' } }, searchBox('筛选鱼名', (v) => { q = v.toLowerCase(); draw(); })),
      btn(q ? '启用筛选到的' : '全部启用', () => { fish.forEach((f) => { if (!q || `${f.fish_key} ${f.name_zh}`.toLowerCase().includes(q)) f.enabled = true; }); touch(); draw(); }, 'ghost sm'),
      btn(q ? '关闭筛选到的' : '全部关闭', () => { fish.forEach((f) => { if (!q || `${f.fish_key} ${f.name_zh}`.toLowerCase().includes(q)) f.enabled = false; }); touch(); draw(); }, 'ghost sm'),
      h('span', { class: 'spacer' }),
      btn('添加鱼种', () => addFish(fish, () => { touch(); draw(); }), 'sm', { icon: 'plus' }),
      btn('恢复默认', async () => {
        if (!(await confirmDialog({ title: `把「${cfg.area}」恢复默认？`, desc: '清除这个水域所有的修改，回到游戏自带的鱼种和比例。', confirm: '恢复默认', danger: true }))) return;
        try { await post('/api/fish/config/reset', { area: cfg.area }); toast('已恢复默认'); dirtyRows = false; render(); } catch (e) { toast(errText(e), 'bad'); }
      }, 'ghost sm')),
    h('p', { class: 'muted small', text: '「权重」决定这种鱼出现的多少：权重越大越常见，右边的百分比是换算后的占比。关闭或权重填 0 表示这里不出这种鱼。' }),
    h('div', { class: 'table-wrap', style: { maxHeight: 'calc(100vh - 360px)' } }, h('table', { class: 'table' },
      h('thead', {}, h('tr', {}, h('th', { text: '出现' }), h('th', { text: '鱼' }), h('th', { text: '爱吃的饵' }), h('th', { text: '权重' }), h('th', { text: '占比' }), h('th', {}))), tbody)));
}

function addFish(fish, done) {
  const results = h('div', { class: 'card', style: { maxHeight: '400px', overflow: 'auto' } }, empty('输入鱼名开始搜索'));
  const search = debounce(async (q) => {
    const list = await apiList('/api/fish/pool?limit=80&q=' + encodeURIComponent(q)).catch(() => []);
    results.replaceChildren(list.length ? h('div', { class: 'list' }, list.map((x) => {
      const has = fish.some((f) => f.fish_key === x.fish_key);
      return h('div', { class: 'list-item' + (has ? '' : ' clickable'), style: { opacity: has ? 0.5 : 1 }, onclick: has ? null : () => {
        fish.push({ fish_key: x.fish_key, name_zh: x.name_zh, base_weight: 0, weight: 1, enabled: true, has_override: true });
        close(); done(); toast(`已加入「${x.name_zh}」，记得保存`);
      } }, fishIcon(x.fish_key), h('div', { class: 'grow' }, h('div', { class: 'title', text: x.name_zh }), h('div', { class: 'sub mono', text: x.fish_key })),
      has ? badge('已在名单里') : h('span', { class: 'muted small', text: `${fmt(x.min_g)}–${fmt(x.max_g)} g` }));
    })) : empty('没有找到'));
  }, 250);
  const { close } = dialog({ title: '添加鱼种', size: 'lg', body: h('div', { class: 'stack' }, searchBox('输入鱼名，如 狗鱼', search), results) });
}

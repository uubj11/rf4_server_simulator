// 商店：直接在表格里改价格和库存，底部统一保存。
import { h, api, apiList, post, toast, errText, pageHead, badge, btn, empty, loading, Form, savebar, setLeaveGuard, searchBox, debounce, confirmDialog, fmt, navigate, render, toggleEl } from '../core.js';
import { itemIcon } from '../widgets.js';

const money = (c) => (Number(c || 0) / 100).toFixed(2).replace(/\.00$/, '');
let Q = '';
let ONLY_CHANGED = false;

// 商店 ID 前缀 → 店铺类型。认不出的品牌店保留原 ID。
const STORE_KIND = [
  [/^fishing_?store/, '渔具店'], [/^food_?store/, '食品店'], [/^tool_store/, '工具店'], [/^workshop/, '工坊'], [/^boatstation/, '船坞'],
  [/^atm/, '取款机'], [/^admin/, '管理处'], [/^monkeycola/, '饮料售货机'], [/^ny_?ba+za+r/i, '新年集市'], [/^cafestore/, '咖啡馆'],
  [/^bakery/, '面包店'], [/^craft_baits/, '饵料制作'], [/^craft_lures/, '拟饵制作'], [/^farmstore/, '农场商店'], [/^fishstore/, '鱼店'],
  [/^housestore/, '房屋商店'], [/^milkstore/, '乳品店'], [/^sawmill/, '锯木厂'], [/^mill/, '磨坊'], [/^paseka/, '养蜂场'],
  [/^rentbike/, '租车'], [/^tours/, '旅行社'], [/^arbuz/, '西瓜摊'],
];
const storeKind = (id) => (STORE_KIND.find(([re]) => re.test(id)) || [null, ''])[1];
let MAP = '';

export default async function shop(params) {
  const [stores, areas] = await Promise.all([apiList('/api/stores'), api('/api/fish/areas').catch(() => [])]);
  const mapName = Object.fromEntries((Array.isArray(areas) ? areas : areas.areas || []).map((a) => [a.level, a.loc]));
  const storeId = params[0] && stores.some((s) => s.store_id === params[0]) ? params[0] : (stores[0] && stores[0].store_id);
  const cur = stores.find((s) => s.store_id === storeId);
  if (cur) MAP = cur.map_key || '';
  const maps = [...new Set(stores.map((s) => s.map_key || ''))].sort((a, b) => a.localeCompare(b));
  const mapLabel = (k) => mapName[k] || (k ? k.replace(/^level_\d+_/, '') : '其他');
  const listBox = h('div', { class: 'list' });
  const drawList = () => listBox.replaceChildren(...stores.filter((s) => (s.map_key || '') === MAP).map((s) => {
    const kind = storeKind(s.store_id);
    return h('div', { class: 'list-item clickable' + (s.store_id === storeId ? ' on' : ''), onclick: () => navigate('shop', s.store_id) },
      h('div', { class: 'grow' }, h('div', { class: 'title', text: kind || s.store_id }), h('div', { class: 'sub', text: (kind ? s.store_id + ' · ' : '') + `${s.item_count} 件商品` })),
      s.override_count ? badge(`改过 ${s.override_count}`, 'warn') : null);
  }));
  const mapSel = h('select', { class: 'select', onchange: (e) => { MAP = e.target.value; drawList(); } },
    maps.map((k) => h('option', { value: k, text: `${mapLabel(k)}（${stores.filter((s) => (s.map_key || '') === k).length}）`, selected: k === MAP })));
  drawList();
  const side = h('div', { class: 'card master' }, h('div', { style: { padding: '12px' } }, h('div', { class: 'field' }, h('label', { text: '地图' }), mapSel)), listBox);
  const main = h('div', {}, loading(6));
  const root = h('div', { class: 'page' },
    pageHead(cur ? `商店 · ${mapLabel(cur.map_key)} · ${storeKind(cur.store_id) || cur.store_id}` : '商店', '修改商品价格和库存，保存后立即生效。库存卖完会自动补到 1000。'),
    h('div', { class: 'master-detail', style: { gridTemplateColumns: '240px minmax(0,1fr)' } }, side, main));
  if (!storeId) { main.replaceChildren(h('div', { class: 'card' }, empty('没有商店数据'))); return root; }

  const form = new Form();
  let items = [];
  savebar(form, {
    label: '',
    onSave: async (values) => {
      const touched = new Set(Object.keys(values).map((k) => k.split('|')[0]));
      for (const id of touched) {
        await post('/api/stores/items/update', {
          store_id: storeId, item_id: id,
          silver: Math.round(+form.value(id + '|silver') * 100), gold: Math.round(+form.value(id + '|gold') * 100), stock: Math.floor(+form.value(id + '|stock')),
        });
      }
      toast(`已保存 ${touched.size} 件商品`);
      setTimeout(render, 200);
    },
  });
  setLeaveGuard(() => form.isDirty());

  const tbody = h('tbody');
  const count = h('span', { class: 'muted small' });
  const cell = (x, field, value, def) => {
    const key = x.item_id + '|' + field;
    let f = form.fields.get(key);
    if (!f) f = form.field({ key, label: field, kind: field === 'stock' ? 'int' : 'float', min: 0 }, value);
    const input = h('input', { class: 'input num', style: { width: '110px' }, value: f.input ? f.input.value : f.original, placeholder: def, oninput: () => form.refresh() });
    f.input = input;
    f.row = input;
    return h('td', {}, input, h('span', { class: 'sub', text: '默认 ' + def }));
  };
  const draw = () => {
    const rows = items.filter((x) => !ONLY_CHANGED || x.has_override);
    count.textContent = `显示 ${rows.length} / ${items.length} 件`;
    tbody.replaceChildren(...(rows.length ? rows.map((x) => h('tr', {},
      h('td', {}, h('div', { class: 'row', style: { flexWrap: 'nowrap' } }, itemIcon(x.item_id), h('div', {}, h('b', { text: x.name || x.item_id }), h('span', { class: 'sub mono', text: x.item_id })))),
      cell(x, 'silver', money(x.silver), money(x.base_silver)),
      cell(x, 'gold', money(x.gold), money(x.base_gold)),
      cell(x, 'stock', String(x.stock), fmt(x.base_stock)),
      h('td', {}, x.has_override ? h('div', { class: 'row', style: { flexWrap: 'nowrap' } }, badge('已修改', 'warn'), btn('恢复默认', async () => {
        if (!(await confirmDialog({ title: `把「${x.name || x.item_id}」恢复默认？`, desc: '价格和库存回到游戏自带的数值。', confirm: '恢复' }))) return;
        try { await post('/api/stores/items/reset', { store_id: storeId, item_id: x.item_id }); toast('已恢复默认'); load(); } catch (e) { toast(errText(e), 'bad'); }
      }, 'ghost sm')) : null))) : [h('tr', {}, h('td', { colspan: 5 }, empty(Q || ONLY_CHANGED ? '没有符合条件的商品' : '这个商店没有商品')))]));
  };
  async function load() {
    try {
      items = await apiList('/api/stores/items?limit=1000&store_id=' + encodeURIComponent(storeId) + '&q=' + encodeURIComponent(Q));
      draw();
    } catch (e) { tbody.replaceChildren(h('tr', {}, h('td', { colspan: 5 }, empty('读取失败', errText(e))))); }
  }
  main.replaceChildren(h('div', { class: 'card card-pad' },
    h('div', { class: 'row', style: { marginBottom: '14px' } },
      h('div', { class: 'grow', style: { maxWidth: '380px' } }, searchBox('搜索商品名称', debounce((v) => {
        if (form.isDirty()) { toast('先保存或放弃当前修改，再搜索', 'bad'); return; }
        Q = v; load();
      }, 300), Q)),
      toggleEl({ checked: ONLY_CHANGED, label: '只看改过的', onChange: (v) => { ONLY_CHANGED = v; draw(); } }),
      h('span', { class: 'spacer' }), count),
    h('div', { class: 'table-wrap', style: { maxHeight: 'calc(100vh - 260px)' } }, h('table', { class: 'table' },
      h('thead', {}, h('tr', {}, h('th', { text: '商品' }), h('th', { text: '银币价' }), h('th', { text: '金币价' }), h('th', { text: '库存' }), h('th', {}))), tbody))));
  await load();
  return root;
}

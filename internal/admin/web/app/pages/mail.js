// 发邮件：选人 → 写内容 → 加附件 → 发送。右边实时预览玩家看到的样子。
import { h, api, apiList, post, toast, errText, pageHead, section, btn, empty, debounce, searchBox, icon, badge, setLeaveGuard, confirmDialog } from '../core.js';
import { itemIcon, pickItem } from '../widgets.js';

const TEMPLATES = [
  ['补偿', '服务器补偿', '因为最近服务器的问题给你带来了不便，特此补偿，请查收附件。'],
  ['活动奖励', '活动奖励', '感谢参与本次活动！奖励已随邮件附上。'],
  ['欢迎', '欢迎来钓鱼', '欢迎来到我们的服务器！祝你钓到大鱼。'],
];

export default async function mail(params) {
  let to = null;
  const attach = [];
  if (params[0]) {
    try { const p = await api('/api/players/detail?uid=' + encodeURIComponent(params[0])); to = { uid: p.uid, name: p.name, level: p.level }; } catch (e) {}
  }
  const text = h('textarea', { class: 'textarea', rows: 9, placeholder: '写给玩家的话。可以只发附件、不写内容。', oninput: () => preview() });
  setLeaveGuard(() => !!(text.value.trim() || attach.length));

  // 收件人
  const toBox = h('div');
  const drawTo = () => {
    if (to) {
      toBox.replaceChildren(h('div', { class: 'row card', style: { padding: '10px 12px', background: 'var(--surface-2)' } },
        h('div', { class: 'avatar', text: (to.name || '?').slice(0, 1) }),
        h('div', { class: 'grow' }, h('b', { text: to.name }), h('div', { class: 'muted small', text: `UID ${to.uid}` + (to.level ? ` · ${to.level} 级` : '') })),
        btn('换一个', () => { to = null; drawTo(); preview(); }, 'ghost sm')));
      return;
    }
    const results = h('div', { class: 'card', hidden: true, style: { marginTop: '6px', maxHeight: '240px', overflow: 'auto' } });
    const search = searchBox('输入玩家昵称或 UID', debounce(async (q) => {
      if (!q.trim()) { results.hidden = true; return; }
      const list = await apiList('/api/players?q=' + encodeURIComponent(q.trim())).catch(() => []);
      results.hidden = false;
      results.replaceChildren(list.length ? h('div', { class: 'list' }, list.slice(0, 20).map((p) => h('div', {
        class: 'list-item clickable', onclick: () => { to = { uid: p.uid, name: p.name, level: p.level }; drawTo(); preview(); },
      }, h('div', { class: 'avatar', text: (p.name || '?').slice(0, 1) }), h('div', { class: 'grow' }, h('div', { class: 'title', text: p.name }), h('div', { class: 'sub', text: `UID ${p.uid} · ${p.level} 级` })), p.online ? badge('在线', 'ok', true) : null))) : empty('没有找到'));
    }, 250));
    toBox.replaceChildren(search, results);
  };
  drawTo();

  // 附件
  const attachBox = h('div');
  const drawAttach = () => {
    attachBox.replaceChildren(attach.length ? h('div', { class: 'card' }, h('div', { class: 'list' }, attach.map((a, i) => h('div', { class: 'list-item' },
      itemIcon(a.item_id), h('div', { class: 'grow' }, h('div', { class: 'title', text: a.name }), h('div', { class: 'sub mono', text: a.item_id })),
      h('label', { class: 'row small muted' }, '数量', h('input', { class: 'input num', style: { width: '80px', height: '30px' }, value: a.qty, oninput: (e) => { a.qty = Math.max(1, Math.floor(+e.target.value || 1)); preview(); } })),
      btn('', () => { attach.splice(i, 1); drawAttach(); preview(); }, 'ghost icon sm', { icon: 'x', 'aria-label': '移除' }))))) : h('div', { class: 'muted small', text: '没有附件。' }));
  };
  drawAttach();

  // 预览
  const pv = h('div');
  function preview() {
    const lines = text.value.trim();
    pv.replaceChildren(h('div', { class: 'card', style: { padding: '18px', background: 'var(--surface-2)' } },
      h('div', { class: 'muted small', text: '收件人：' + (to ? `${to.name}（UID ${to.uid}）` : '还没选') }),
      h('div', { style: { margin: '12px 0', whiteSpace: 'pre-wrap', minHeight: '60px', color: lines ? 'var(--text)' : 'var(--faint)' }, text: lines || '（没有正文）' }),
      attach.length ? h('div', { class: 'row', style: { gap: '10px', borderTop: '1px solid var(--line)', paddingTop: '12px' } }, attach.map((a) => h('div', { class: 'row', style: { gap: '6px' } }, itemIcon(a.item_id, 28), h('span', { class: 'small', text: `${a.name} ×${a.qty}` })))) : null));
  }
  preview();

  const send = btn('发送', async () => {
    if (!to) { toast('请先选择收件人', 'bad'); return; }
    if (!text.value.trim() && !attach.length) { toast('正文和附件不能都为空', 'bad'); return; }
    if (!(await confirmDialog({ title: `发给「${to.name}」？`, desc: attach.length ? `附带 ${attach.length} 种物品。发出后不能撤回。` : '发出后不能撤回。', confirm: '发送' }))) return;
    send.disabled = true;
    try {
      await post('/api/mail/send', { uid: to.uid, text: text.value.trim(), items: attach.map((a) => ({ item_id: a.item_id, qty: a.qty })) });
      toast('已发送，约 2 秒内送达');
      text.value = ''; attach.length = 0; drawAttach(); preview();
    } catch (e) { toast('发送失败：' + errText(e), 'bad'); }
    finally { send.disabled = false; }
  }, 'primary lg', { icon: 'mail' });

  return h('div', { class: 'page' },
    pageHead('发邮件', '给一个玩家发消息，可以附带物品。玩家在游戏的邮箱里领取。'),
    h('div', { class: 'grid-2', style: { gridTemplateColumns: 'minmax(0,1.3fr) minmax(0,1fr)', alignItems: 'start' } },
      h('div', {},
        section({ title: '发给谁', body: toBox }),
        section({
          title: '写点什么',
          actions: h('div', { class: 'row' }, h('span', { class: 'muted small', text: '模板' }), TEMPLATES.map(([label, title, body]) => btn(label, () => { text.value = title + '\n\n' + body; preview(); }, 'ghost sm'))),
          body: text,
        }),
        section({ title: '附件', desc: '物品会和邮件一起送达，玩家领取后进背包。', actions: btn('添加物品', () => pickItem({ title: '添加附件', onPick: (x) => { if (!attach.some((a) => a.item_id === x.item_id)) attach.push({ ...x, qty: 1 }); drawAttach(); preview(); } }), 'sm', { icon: 'plus' }), body: attachBox })),
      h('div', { style: { position: 'sticky', top: '20px' } },
        section({ title: '预览', desc: '玩家收到的内容。', body: pv }),
        h('div', { class: 'row', style: { justifyContent: 'flex-end' } }, send))));
}

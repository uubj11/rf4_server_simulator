import { h, api, apiList, fmt, relTime, timeText, icon, navigate, section, badge, empty, pageHead, btn, render, setPageCleanup } from '../core.js';
import { auditLabel } from '../labels.js';
import { updateBanner } from '../main.js';

const SHORTCUTS = [
  ['players', 'users', '查找玩家', '改等级、货币、背包或技能'],
  ['mail', 'mail', '发邮件', '给玩家发消息或物品'],
  ['rules', 'rules', '调整玩法规则', '选择真实或随机的钓鱼体验'],
  ['waters', 'water', '设置水域鱼种', '每个水域出哪些鱼'],
  ['shop', 'store', '调整商店', '改商品价格和库存'],
];
const RULES = [
  ['fish_bait_rules_enabled', '鱼会挑饵'], ['fish_distribution_enabled', '鱼群分布'], ['fish_weather_enabled', '天气与水温'],
  ['fish_ecology_enabled', '鱼种习性'], ['bite_gate_enabled', '饵要对口'], ['happy_hour_enabled', '欢乐时光'],
];

export default async function dashboard() {
  const [d, audits, tuns, mode] = await Promise.all([
    api('/api/dashboard'), apiList('/api/audit?limit=8').catch(() => []), api('/api/tunables').catch(() => ({})),
    api('/api/fish/bite_mode').catch(() => ({})),
  ]);
  updateBanner(d.update);
  const timer = setInterval(() => { if (!document.hidden) render(); }, 8000);
  setPageCleanup(() => clearInterval(timer));
  const stats = [
    ['当前在线', d.sessions, '正在游戏的连接'],
    ['角色', d.players, '已创建的角色'],
    ['物品', d.items, '所有背包和装备里的物品'],
    ['后台修改', d.audits, '累计记录的操作'],
  ];
  const modeName = { realistic: '真实鱼汛', legacy: '全随机（旧版）', mixed: '自定义' }[mode.mode] || '未知';
  const ruleOn = (k) => (tuns[k] === undefined ? k === 'happy_hour_enabled' : tuns[k] === '1');

  return h('div', { class: 'page' },
    pageHead('总览', '服务器现在的情况，以及常用功能的入口。', btn('刷新', () => render(), 'sm', { icon: 'refresh' })),
    h('div', { class: 'grid-4', style: { marginBottom: '16px' } },
      stats.map(([k, v, s]) => h('div', { class: 'card stat' }, h('div', { class: 'k', text: k }), h('div', { class: 'v', text: fmt(v) }), h('div', { class: 's', text: s })))),
    h('div', { class: 'grid-2', style: { gridTemplateColumns: 'minmax(0,1.4fr) minmax(0,1fr)', alignItems: 'start' } },
      section({
        title: '常用功能',
        body: h('div', { class: 'grid-2', style: { gap: '10px' } }, SHORTCUTS.map(([r, ic, t, d2]) => h('button', {
          class: 'card', type: 'button', onclick: () => navigate(r),
          style: { display: 'flex', gap: '12px', alignItems: 'center', padding: '14px', textAlign: 'left', background: 'var(--surface-2)', cursor: 'pointer' },
        }, h('span', { style: { width: '34px', height: '34px', borderRadius: '8px', display: 'grid', placeItems: 'center', background: 'var(--accent-soft)', color: 'var(--accent)', flex: 'none' } }, icon(ic, 17)),
        h('span', {}, h('b', { style: { display: 'block', fontWeight: 600 }, text: t }), h('span', { class: 'muted small', text: d2 }))))),
      }),
      section({
        title: '当前玩法',
        actions: btn('调整', () => navigate('rules'), 'sm'),
        body: h('div', {},
          h('div', { class: 'row', style: { marginBottom: '10px' } }, h('span', { class: 'muted small', text: '玩法风格' }), badge(modeName, 'accent')),
          h('div', { class: 'list' }, RULES.map(([k, n]) => h('div', { class: 'row', style: { justifyContent: 'space-between', padding: '7px 0', borderTop: '1px solid var(--line)' } },
            h('span', { text: n }), ruleOn(k) ? badge('开启', 'ok', true) : badge('关闭', '', true))))),
      })),
    section({
      title: '最近的修改',
      actions: btn('全部记录', () => navigate('audit'), 'sm'),
      body: audits.length ? h('div', { class: 'list' }, audits.map((e) => {
        const ts = e.TS ?? e.ts, target = (e.Target ?? e.target) || '';
        return h('div', { class: 'row', style: { padding: '9px 0', borderTop: '1px solid var(--line)', flexWrap: 'nowrap' } },
          h('span', { class: 'muted small nowrap', style: { width: '90px' }, title: timeText(ts), text: relTime(ts) }),
          h('b', { style: { fontWeight: 600 }, text: auditLabel(e.Action ?? e.action) }),
          h('span', { class: 'muted small grow', style: { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }, text: target }),
          h('span', { class: 'muted small', text: e.Admin ?? e.admin ?? '' }));
      })) : empty('还没有修改记录', '在后台做的每一次修改都会记在这里。'),
    }));
}

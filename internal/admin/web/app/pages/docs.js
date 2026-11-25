import { h, api, pageHead, empty, btn, errText } from '../core.js';
import { markdownHTML, ABOUT_REPO } from '../markdown.js';

function docPage(title, desc, path, emptyText, actions) {
  return async () => {
    let body;
    try {
      const d = await api(path);
      body = d.content ? h('div', { class: 'md', html: markdownHTML(d.content) }) : empty(emptyText);
    } catch (e) {
      body = empty('读取失败', errText(e) + '。如果是自己从源码运行，没有发布包里的文件是正常的。');
    }
    return h('div', { class: 'page narrow' }, pageHead(title, desc, actions), h('div', { class: 'card', style: { padding: '28px 32px' } }, body));
  };
}

export const changelog = docPage('更新日志', '这个版本新增和修复了什么。', '/api/release-notes', '没有更新日志');
export const about = docPage('关于', '免费开源的 RF4 本地服务器。以下是项目说明。', '/api/about', '没有项目说明',
  h('a', { class: 'btn', href: ABOUT_REPO, target: '_blank', rel: 'noreferrer', text: '在 GitHub 上查看' }));

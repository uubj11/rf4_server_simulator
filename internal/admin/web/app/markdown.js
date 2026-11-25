// 轻量 Markdown 渲染（README / 发布说明用）。所有文本先转义，只生成固定标签。
import { esc } from './core.js';
export const ABOUT_REPO='https://github.com/OpenFisherCommunity/rf4_server_simulator';
export const ABOUT_BLOB=ABOUT_REPO+'/blob/main/';
function readmeInline(text){
  const stash=[];
  const keep=html=>{stash.push(html);return '\u0000'+(stash.length-1)+'\u0000'};
  let s=String(text??'');
  s=s.replace(/`([^`]+)`/g,(_,code)=>keep('<code>'+esc(code)+'</code>'));
  s=s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g,(_,label,href)=>{
    let u=href;
    if(!/^(?:https?:\/\/|mailto:|#|\/)/i.test(u)){
      if(!/^(?:\.{0,2}\/)/.test(u))u='./'+u;
      u=ABOUT_BLOB+u.replace(/^\.\//,'');
    }
    return keep('<a href="'+esc(u)+'" target="_blank" rel="noreferrer">'+esc(label)+'</a>');
  });
  s=esc(s).replace(/\*\*([^*]+)\*\*/g,'<strong>$1</strong>');
  return s.replace(/\u0000(\d+)\u0000/g,(_,i)=>stash[Number(i)]);
}
function readmeCells(line){
  return line.trim().replace(/^\|/,'').replace(/\|$/,'').split('|').map(x=>x.trim());
}
export function markdownHTML(src){
  const lines=String(src??'').replace(/\r\n?/g,'\n').split('\n');
  const out=[];let i=0;
  const isBlockStart=line=>/^(#{1,6})\s+/.test(line)||/^`{3}/.test(line)||/^\s*>/.test(line)||/^\s*[-*+]\s+/.test(line)||/^\s*\d+\.\s+/.test(line)||/^\s*(---|\*\*\*|___)\s*$/.test(line)||/^\s*\|.*\|\s*$/.test(line);
  while(i<lines.length){
    const line=lines[i];
    if(!line.trim()){i++;continue}
    if(/^`{3}/.test(line)){
      const lang=line.slice(3).trim(),code=[];i++;
      while(i<lines.length&&!/^`{3}/.test(lines[i])){code.push(lines[i]);i++}
      if(i<lines.length)i++;
      out.push('<pre><code'+(lang?' class="language-'+esc(lang)+'"':'')+'>'+esc(code.join('\n'))+'</code></pre>');continue;
    }
    const h=line.match(/^(#{1,6})\s+(.*)$/);
    if(h){const n=h[1].length;out.push('<h'+n+'>'+readmeInline(h[2])+'</h'+n+'>');i++;continue}
    if(/^\s*(---|\*\*\*|___)\s*$/.test(line)){out.push('<hr>');i++;continue}
    if(/^\s*\|.*\|\s*$/.test(line)&&i+1<lines.length&&/^\s*\|?\s*:?-{3,}/.test(lines[i+1])){
      const head=readmeCells(line);i+=2;const rows=[];
      while(i<lines.length&&/^\s*\|.*\|\s*$/.test(lines[i])){rows.push(readmeCells(lines[i]));i++}
      out.push('<table><thead><tr>'+head.map(x=>'<th>'+readmeInline(x)+'</th>').join('')+'</tr></thead><tbody>'+rows.map(r=>'<tr>'+head.map((_,j)=>'<td>'+readmeInline(r[j]||'')+'</td>').join('')+'</tr>').join('')+'</tbody></table>');continue;
    }
    if(/^\s*>/.test(line)){
      const quote=[];while(i<lines.length&&/^\s*>/.test(lines[i])){quote.push(lines[i].replace(/^\s*>\s?/,''));i++}
      out.push('<blockquote>'+quote.map(readmeInline).join('<br>')+'</blockquote>');continue;
    }
    const ul=/^\s*[-*+]\s+(.*)$/,ol=/^\s*\d+\.\s+(.*)$/;
    if(ul.test(line)||ol.test(line)){
      const ordered=ol.test(line),items=[];
      while(i<lines.length){const m=lines[i].match(ordered?ol:ul);if(!m)break;items.push('<li>'+readmeInline(m[1])+'</li>');i++}
      out.push('<'+(ordered?'ol':'ul')+'>'+items.join('')+'</'+(ordered?'ol':'ul')+'>');continue;
    }
    const para=[line.trim()];i++;
    while(i<lines.length&&lines[i].trim()&&!isBlockStart(lines[i])){para.push(lines[i].trim());i++}
    out.push('<p>'+readmeInline(para.join(' '))+'</p>');
  }
  return out.join('');
}

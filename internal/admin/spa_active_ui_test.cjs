// Active SPA smoke test: serve ES modules from disk and inject nullable list responses.
const {test}=require('node:test');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {chromium}=require('playwright');
const webRoot=path.join(__dirname,'web');

test('active admin SPA tolerates empty lists and shows four QQ groups',async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.RF4_TEST_BROWSER});
 try{
  const context=await browser.newContext({viewport:{width:1280,height:900}});
  await context.addInitScript(()=>localStorage.setItem('rf4_admin_token','fixture'));
  const page=await context.newPage(),errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',route=>{
   const url=new URL(route.request().url());
   if(url.pathname==='/')return route.fulfill({contentType:'text/html; charset=utf-8',body:fs.readFileSync(path.join(webRoot,'index.html'),'utf8')});
   if(url.pathname.startsWith('/app/')){
    const file=path.join(webRoot,url.pathname.replace(/^\//,''));
    if(fs.existsSync(file))return route.fulfill({contentType:file.endsWith('.css')?'text/css; charset=utf-8':'text/javascript; charset=utf-8',body:fs.readFileSync(file)});
    return route.fulfill({status:404,body:''});
   }
   if(url.pathname==='/api/me')return route.fulfill({contentType:'application/json',body:JSON.stringify({username:'admin'})});
   if(url.pathname==='/api/dashboard')return route.fulfill({contentType:'application/json',body:JSON.stringify({sessions:0,players:0,items:0,audits:0})});
   if(url.pathname==='/api/tunables')return route.fulfill({contentType:'application/json',body:'{}'});
   if(url.pathname==='/api/fish/bite_mode')return route.fulfill({contentType:'application/json',body:'{}'});
   if(url.pathname==='/api/audit'||url.pathname==='/api/players')return route.fulfill({contentType:'application/json',body:'null'});
   return route.fulfill({contentType:'application/json',body:'{}'});
  });
  await page.goto('http://127.0.0.1:19214/#players');
  await page.getByRole('heading',{name:'玩家',exact:true}).waitFor();
  await page.getByText('还没有玩家').waitFor();
  const notice=await page.locator('.notice-bar').innerText();
  for(const group of ['379233540','225460820','1128152596','1134665746'])assert.ok(notice.includes(group),group);
  await page.getByLabel('主导航').getByRole('button',{name:'总览',exact:true}).click();
  await page.getByRole('heading',{name:'总览',exact:true}).waitFor();
  await page.getByText('还没有修改记录').waitFor();
  assert.equal(await page.getByText('页面加载失败').count(),0);
  assert.deepEqual(errors,[]);
  await context.close();
 }finally{await browser.close()}
});


test('active skill gain UI switches mode through one endpoint',async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.RF4_TEST_BROWSER});
 try{
  const context=await browser.newContext({viewport:{width:1280,height:900}});
  await context.addInitScript(()=>localStorage.setItem('rf4_admin_token','fixture'));
  const page=await context.newPage(),errors=[];let mode='native';
  page.on('pageerror',e=>errors.push(e.message));
  const pctKeys=['skill_gain_pct_1','skill_gain_pct_2','skill_gain_pct_3','skill_gain_pct_11','skill_gain_pct_4','skill_gain_pct_6','skill_gain_pct_5','skill_gain_pct_8'];
  await page.route('**/*',async route=>{
   const url=new URL(route.request().url());
   if(url.pathname==='/')return route.fulfill({contentType:'text/html; charset=utf-8',body:fs.readFileSync(path.join(webRoot,'index.html'),'utf8')});
   if(url.pathname.startsWith('/app/')){
    const file=path.join(webRoot,url.pathname.replace(/^\//,''));
    if(fs.existsSync(file))return route.fulfill({contentType:file.endsWith('.css')?'text/css; charset=utf-8':'text/javascript; charset=utf-8',body:fs.readFileSync(file)});
    return route.fulfill({status:404,body:''});
   }
   if(url.pathname==='/api/me')return route.fulfill({contentType:'application/json',body:JSON.stringify({username:'admin'})});
   if(url.pathname==='/api/tunables'){const data={skill_gain_mode:mode};for(const key of pctKeys)data[key]='0';return route.fulfill({contentType:'application/json',body:JSON.stringify(data)})}
   if(url.pathname==='/api/skill_gain_mode'&&route.request().method()==='POST'){mode=route.request().postDataJSON().mode;return route.fulfill({contentType:'application/json',body:JSON.stringify({ok:true,mode})})}
   return route.fulfill({contentType:'application/json',body:'{}'});
  });
  await page.goto('http://127.0.0.1:19214/#tuning/reward');
  await page.getByRole('heading',{name:'数值调整',exact:true}).waitFor();
  await page.getByText('技能升级概率',{exact:true}).waitFor();
  for(const label of ['原版概率','百分百','自定义'])assert.equal(await page.getByRole('radio',{name:label,exact:true}).count(),1);
  await page.getByRole('radio',{name:'自定义',exact:true}).click();
  await page.getByText('浮子钓鱼',{exact:true}).waitFor();
  assert.equal(await page.locator('input[id^="f_skill_gain_pct_"]').count(),8);
  assert.deepEqual(errors,[]);
  await context.close();
 }finally{await browser.close()}
});

// 饵效控速参数显隐与保存链路：开关关闭时数值调整页不渲染 pace_* 行（原位提示），
// 开启后出现且保存走 POST /api/tunables。
const {test}=require('node:test');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {chromium}=require('playwright');
const webRoot=path.join(__dirname,'web');

function mockRoute(page,data,posts){
 page.route('**/*',async route=>{
  const url=new URL(route.request().url());
  if(url.pathname==='/')return route.fulfill({contentType:'text/html; charset=utf-8',body:fs.readFileSync(path.join(webRoot,'index.html'),'utf8')});
  if(url.pathname.startsWith('/app/')){
   const file=path.join(webRoot,url.pathname.replace(/^\//,''));
   if(fs.existsSync(file))return route.fulfill({contentType:file.endsWith('.css')?'text/css; charset=utf-8':'text/javascript; charset=utf-8',body:fs.readFileSync(file)});
   return route.fulfill({status:404,body:''});
  }
  if(url.pathname==='/api/me')return route.fulfill({contentType:'application/json',body:JSON.stringify({username:'admin'})});
  if(url.pathname==='/api/tunables'&&route.request().method()==='POST'){posts.push(route.request().postDataJSON());return route.fulfill({contentType:'application/json',body:'{}'})}
  if(url.pathname==='/api/tunables')return route.fulfill({contentType:'application/json',body:JSON.stringify(data)});
  return route.fulfill({contentType:'application/json',body:'{}'});
 });
}

test('tuning hides pace rows while bite pace disabled',async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.RF4_TEST_BROWSER});
 try{
  const context=await browser.newContext({viewport:{width:1280,height:900}});
  await context.addInitScript(()=>localStorage.setItem('rf4_admin_token','fixture'));
  const page=await context.newPage(),errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  mockRoute(page,{bite_pace_enabled:'0'},[]);
  await page.goto('http://127.0.0.1:19216/#tuning/bite');
  await page.getByRole('heading',{name:'数值调整',exact:true}).waitFor();
  await page.getByText('每次抛竿有鱼的概率',{exact:true}).waitFor();
  assert.equal(await page.locator('#f_pace_good_max').count(),0,'开关关闭时不应渲染 pace 参数行');
  assert.equal(await page.locator('#f_pace_good_min').count(),0);
  await page.getByText('开启后这里才会出现好饵').waitFor();
  assert.equal(await page.getByText('3 项，一般不用改').count(),1,'进阶折叠区应只剩 3 项');
  assert.deepEqual(errors,[]);
  await context.close();
 }finally{await browser.close()}
});

test('tuning shows pace rows when enabled and saves through tunables API',async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.RF4_TEST_BROWSER});
 try{
  const context=await browser.newContext({viewport:{width:1280,height:900}});
  await context.addInitScript(()=>localStorage.setItem('rf4_admin_token','fixture'));
  const page=await context.newPage(),errors=[],posts=[];
  page.on('pageerror',e=>errors.push(e.message));
  mockRoute(page,{bite_pace_enabled:'1'},posts);
  await page.goto('http://127.0.0.1:19216/#tuning/bite');
  await page.getByRole('heading',{name:'数值调整',exact:true}).waitFor();
  await page.getByText('好饵等待下限（饵效控速）',{exact:true}).waitFor();
  assert.equal(await page.locator('#f_pace_good_max').count(),1);
  assert.equal(await page.locator('#f_pace_lure_max').count(),1);
  assert.equal(await page.getByText('开启后这里才会出现好饵').count(),0,'开启后不应显示提示');
  await page.locator('#f_pace_good_max').fill('45');
  await page.getByRole('button',{name:'保存修改',exact:true}).click();
  await page.getByText('已保存',{exact:true}).waitFor({timeout:5000});
  const hit=posts.find(p=>p&&p.key==='pace_good_max');
  assert.ok(hit,'应 POST pace_good_max');
  assert.equal(String(hit.value),'45');
  assert.deepEqual(errors,[]);
  await context.close();
 }finally{await browser.close()}
});

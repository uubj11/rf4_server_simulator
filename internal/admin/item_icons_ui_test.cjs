// Legacy fallback fixture; active SPA smoke lives in spa_active_ui_test.cjs.
const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require('playwright');

const PNG=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=','base64');

test('item and fish rows load encoded icons and hide missing images',async()=>{
 const html=fs.readFileSync(path.join(__dirname,'web/legacy.html'));
 const iconRequests=[];
 const server=http.createServer((req,res)=>{
  const url=new URL(req.url,'http://fixture'),key=url.pathname.replace(/^\/admin/,'');
  const json=(value,status=200)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(value));};
  if(key==='/api/me')return json({username:'admin'});
  if(key==='/api/players')return json([{uid:1000001,name:'Tester',level:5,online:false,banned:false}]);
  if(key==='/api/players/detail')return json({
   uid:1000001,name:'Tester',level:5,xp:10,silver:20,gold:30,bank:40,membership:0,membership_exp:0,
   item_count:1,keepnet_count:0,mail_count:0,banned:false,happy_hour:null,
   items:[{item_id:'Bait A',name:'Bait A',category:27,container:'backpack',qty:2,durability:1,guid:'0123456789abcdef'}],
   keepnet:[],mails:[],skills:[],skill_total:0,skill_spent:0,skill_bonus:0,available_points:0
  });
  if(key==='/api/tunables')return json({});
  if(key==='/api/fish/bite_mode')return json({mode:'legacy'});
  if(key==='/api/fish/zones')return json({zones:[]});
  if(key==='/api/fish/areas')return json([{loc:'Test Lake',fish_count:2,level:'level_test',map_kind:'none',texture:'',calibration:null}]);
  if(key==='/api/bait/prefs')return json({rows:[]});
  if(key==='/api/fish/config')return json({
   area:'Test Lake',
   setting:{bite_probability:1,bite_delay_fixed:0,bite_delay_scale:1,bite_delay_max:20,bite_delay_lure_max:6},
   fish:[
    {fish_key:'Test.Fish',name_zh:'Test Fish',base_weight:1,weight:1,enabled:true},
    {fish_key:'Missing.Fish',name_zh:'Missing Fish',base_weight:1,weight:1,enabled:true}
   ]
  });
  if(key.startsWith('/icons/')){
   iconRequests.push(key);
   if(key==='/icons/items/bait%20a.webp'||key==='/icons/fish/test_fish.png'){
    res.writeHead(200,{'Content-Type':'image/png'});return res.end(PNG);
   }
   res.writeHead(404,{'Content-Type':'application/json'});return res.end('{"error":"missing"}');
  }
  if(key.startsWith('/api/'))return json({});
  res.writeHead(200,{'Content-Type':'text/html; charset=utf-8'});res.end(html);
 });
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 let browser;
 try{
  browser=await chromium.launch({headless:true,executablePath:process.env.RF4_TEST_BROWSER||path.join(process.env['ProgramFiles(x86)'],'Microsoft/Edge/Application/msedge.exe')});
  const context=await browser.newContext({viewport:{width:1280,height:900}});
  await context.addInitScript(()=>{localStorage.setItem('rf4_local_admin_token','fixture');localStorage.setItem('rf4_admin_token','fixture');});
  const page=await context.newPage(),errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  const base=`http://127.0.0.1:${server.address().port}`;

  await page.goto(base+'/admin/#players',{waitUntil:'networkidle'});
  await page.locator('.player-row').first().click();
  await page.locator('button:has-text("背包 / 装备")').click();
  const item=page.locator('.item-icon');
  await item.waitFor();
  assert.equal(await item.getAttribute('src'),'icons/items/bait%20a.webp');
  await page.waitForFunction(()=>{const i=document.querySelector('.item-icon');return i&&i.complete&&i.naturalWidth>0});

  await page.goto(base+'/admin/#fish',{waitUntil:'networkidle'});
  const fish=page.locator('.fish-icon').first();
  await fish.waitFor();
  assert.equal(await fish.getAttribute('src'),'icons/fish/test_fish.png');
  await page.waitForFunction(()=>{const i=document.querySelector('.fish-icon');return i&&i.complete&&i.naturalWidth>0});
  const missing=page.locator('.fish-icon[src="icons/fish/missing_fish.png"]');
  await page.waitForFunction(()=>{const i=document.querySelector('.fish-icon[src="icons/fish/missing_fish.png"]');return i&&i.complete&&i.style.display==='none'});
  assert.equal(await missing.evaluate(el=>el.style.display),'none');

  assert.ok(iconRequests.includes('/icons/items/bait%20a.webp'),JSON.stringify(iconRequests));
  assert.ok(iconRequests.includes('/icons/fish/test_fish.png'),JSON.stringify(iconRequests));
  assert.deepEqual(errors,[]);
  await context.close();
 }finally{
  if(browser)await browser.close();
  await new Promise(r=>server.close(r));
 }
});

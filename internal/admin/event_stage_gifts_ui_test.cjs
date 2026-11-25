// Legacy fallback fixture; active SPA smoke lives in spa_active_ui_test.cjs.
const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path');
const {chromium}=require('playwright');

const html=fs.readFileSync(path.join(__dirname,'web/legacy.html'),'utf8');

test('event stage gifts picker follows selected stage and saves structured JSON',async()=>{
  const browser=await chromium.launch({headless:true,executablePath:process.env.RF4_TEST_BROWSER||path.join(process.env['ProgramFiles(x86)'],'Microsoft/Edge/Application/msedge.exe')});
  try{
    for(const width of [1280,360]){
      const values={event_newyear_enabled:'1',event_anniversary_enabled:'0',event_branches:'13,14',event_stage_gifts:'{"13":{"1":[{"item":"fcb_coffee_can2","qty":1},{"item":"ab_tea_can2","qty":2}]}}'};
      const writes=[];
      const context=await browser.newContext({viewport:{width,height:900},deviceScaleFactor:1.5});
      await context.addInitScript(()=>localStorage.setItem('rf4_admin_token','fixture'));
      const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
      const saveRewards=async()=>{
        await page.evaluate(()=>{const save=saveEventStageGifts;window.saveEventStageGifts=()=>window.eventGiftTestSave=save()});
        await page.getByRole('button',{name:'保存全部奖励'}).click();
        await page.evaluate(()=>window.eventGiftTestSave);
      };
      await page.route('**/*',route=>{
        const url=new URL(route.request().url());
        if(url.pathname==='/admin/')return route.fulfill({contentType:'text/html; charset=utf-8',body:html});
        if(/^\/admin\/event-reward-presets\/(?:anniversary-202[456]|newyear-202[56])\.json$/.test(url.pathname))return route.fulfill({contentType:'application/json',body:fs.readFileSync(path.join(__dirname,'web/event-reward-presets',path.basename(url.pathname)),'utf8')});
        if(url.pathname==='/admin/api/me')return route.fulfill({contentType:'application/json',body:JSON.stringify({username:'admin'})});
        if(url.pathname==='/admin/api/tunables'){
          if(route.request().method()==='POST'){
            const value=JSON.parse(route.request().postData());writes.push(value);values[value.key]=value.value;
            if(value.value==='1'&&['event_newyear_enabled','event_anniversary_enabled'].includes(value.key))values[value.key==='event_newyear_enabled'?'event_anniversary_enabled':'event_newyear_enabled']='0';
            return route.fulfill({contentType:'application/json',body:JSON.stringify({ok:true})});
          }
          return route.fulfill({contentType:'application/json',body:JSON.stringify(values)});
        }
        if(url.pathname==='/admin/api/catalog/items'){
          const q=(url.searchParams.get('q')||'').toLowerCase();
          const items=[{item_id:'fcb_coffee_can2',name:'咖啡',kind:'food',group:'食物',category:27},{item_id:'ab_tea_can2',name:'早餐茶',kind:'food',group:'食物',category:27},{item_id:'fcb_cocoa_can2',name:'可可',kind:'food',group:'食物',category:27},{item_id:'rod_blank_test',name:'竿坯组件',kind:'rod_blank',group:'组件',category:36}].filter(x=>!q||x.item_id.includes(q)||x.name.includes(q));
          const complete=url.searchParams.get('complete_only')==='1';
          const group=url.searchParams.get('group');
          const visible=(complete?items.filter(x=>x.kind!=='rod_blank'):items).filter(x=>!group||group==='food');
          return route.fulfill({contentType:'application/json',body:JSON.stringify(visible)});
        }
        if(url.pathname==='/admin/api/catalog/groups'){
          return route.fulfill({contentType:'application/json',body:JSON.stringify([{key:'food',name:'食物与饮料',count:3}])});
        }
        if(url.pathname.startsWith('/admin/api/'))return route.fulfill({contentType:'application/json',body:JSON.stringify([])});
        return route.fulfill({status:404,body:'not found'});
      });
      await page.goto(`http://127.0.0.1:19214/admin/#params`,{waitUntil:'networkidle'});
      await page.locator('button[data-page="event"]').click();
      await page.locator('#eventStageGiftPlans').waitFor();
      assert.deepEqual(await page.locator('#eventGiftBranch option').evaluateAll(xs=>xs.map(x=>x.value)),['13','14']);
      assert.equal(await page.locator('#eventGiftStage option').count(),8);
      await page.locator('#eventGiftBranch').selectOption('14');
      assert.equal(await page.locator('#eventGiftStage option').count(),11);
      await page.locator('#eventGiftStage').selectOption('10');
      await page.locator('#eventGiftBranch').selectOption('13');
      assert.equal(await page.locator('#eventGiftStage option').count(),8);
      assert.equal(await page.locator('#eventGiftStage').inputValue(),'7');
      await page.locator('#eventGiftStage').selectOption('1');
      assert.equal(await page.locator('.event-gift-item').count(),2);
      await page.waitForFunction(()=>document.querySelector('.event-gift-item .picked')?.innerText.includes('咖啡'));
      assert.match(await page.locator('.event-gift-item .picked').first().innerText(),/咖啡/);
      assert.equal(await page.locator('input[aria-label="物品 ID"]').count(),0);
      await page.locator('#eventGiftStage').selectOption('2');
      assert.equal(await page.locator('.event-gift-item').count(),0);
      assert.match(await page.locator('#eventStageGiftPlans').innerText(),/暂无奖励/);
      await page.locator('#eventGiftStage').selectOption('1');
      assert.equal(await page.locator('.event-gift-item').count(),2);
      await page.locator('.event-gift-item input[aria-label="物品数量"]').nth(1).fill('3');
      await page.getByRole('button',{name:'＋ 添加奖励物品'}).click();
      await page.locator('#eventGiftResults .result').first().waitFor();
      assert.ok(await page.locator('#eventGiftGroups button[data-group]').count()>0);
      assert.match(await page.locator('#eventGiftGroups').innerText(),/食物与饮料/);
      assert.doesNotMatch(await page.locator('#eventGiftResults').innerText(),/竿坯组件/);
      await page.locator('#eventGiftResults button').filter({hasText:'可可'}).click();
      await saveRewards();
      await page.waitForFunction(()=>document.querySelector('.toast')?.textContent.includes('请重启游戏客户端'));
      await page.waitForFunction(()=>document.querySelector('.event-gift-item .picked')?.innerText.includes('咖啡'));
      assert.match(await page.locator('.event-gift-item .picked').first().innerText(),/咖啡/);
      const saved=writes.at(-1);
      assert.equal(saved.key,'event_stage_gifts');
      assert.deepEqual(JSON.parse(saved.value),{13:{1:[{item:'fcb_coffee_can2',qty:1},{item:'ab_tea_can2',qty:3},{item:'fcb_cocoa_can2',qty:1}]}});
      // 回归：保存当前阶段 2 后，页面仍应停留在阶段 2，不能回到默认阶段 1。
      await page.locator('#eventGiftStage').selectOption('2');
      await page.getByRole('button',{name:'＋ 添加奖励物品'}).click();
      await page.locator('#eventGiftResults .result').first().waitFor();
      await page.locator('#eventGiftResults button').filter({hasText:'咖啡'}).click();
      await saveRewards();
      await page.waitForFunction(()=>document.querySelector('.toast')?.textContent.includes('请重启游戏客户端'));
      assert.equal(await page.locator('#eventGiftStage').inputValue(),'2');
      const savedStage2=writes.at(-1);
      assert.deepEqual(JSON.parse(savedStage2.value),{13:{1:[{item:'fcb_coffee_can2',qty:1},{item:'ab_tea_can2',qty:3},{item:'fcb_cocoa_can2',qty:1}],2:[{item:'fcb_coffee_can2',qty:1}]}});
      const metrics=await page.locator('.event-gift-card').evaluate(card=>({viewport:innerWidth,scroll:document.documentElement.scrollWidth,controls:[...card.querySelectorAll('input,select,button')].map(e=>{const r=e.getBoundingClientRect();return{x:r.x,right:r.right,height:r.height};})}));
      assert.ok(metrics.scroll<=metrics.viewport+2,JSON.stringify(metrics));
      for(const control of metrics.controls)assert.ok(control.x>=-1&&control.right<=metrics.viewport+2&&control.height>=15,JSON.stringify({width,control,metrics}));
      const savedRewards=values.event_stage_gifts;
      const assertDisabled=async()=>assert.ok(await page.locator('.event-gift-editor').evaluate(el=>[...el.querySelectorAll('button,input,select')].every(x=>x.matches(':disabled'))));
      await page.locator('#tun_event_newyear_enabled').click();
      await page.waitForFunction(()=>document.querySelector('.event-gift-editor')?.disabled);
      await assertDisabled();
      assert.match(await page.locator('#eventGiftStatus').innerText(),/尚未开启限时活动/);
      const writesBefore=writes.length;
      await page.evaluate(async()=>{addEventStageGiftItem();removeEventStageGiftItem(0);clearEventStageGiftsDraft();openEventStageGiftPicker(0);await saveEventStageGifts()});
      assert.equal(writes.length,writesBefore);
      assert.equal(values.event_stage_gifts,savedRewards);
      await page.locator('#tun_event_anniversary_enabled').click();
      await page.waitForFunction(()=>document.querySelector('.event-gift-editor')?.disabled===false);
      assert.equal(await page.locator('#eventGiftStage').inputValue(),'2');
      assert.equal(await page.locator('.event-gift-item').count(),1);
      await page.locator('#tun_event_newyear_enabled').click();
      await page.waitForFunction(()=>document.querySelector('#tun_event_anniversary_enabled')?.textContent.includes('：关'));
      assert.equal(await page.locator('.event-gift-editor').isDisabled(),false);
      const saveBranches=async value=>{
        await page.locator('#tun_event_branches').fill(value);
        const saved=page.waitForResponse(r=>r.url().endsWith('/api/tunables')&&r.request().method()==='POST');
        await page.locator('#tun_event_branches').locator('..').getByRole('button',{name:'保存',exact:true}).click();
        await saved;
        const expected=value?[...new Set(value.split(','))].sort((a,b)=>Number(a)-Number(b)):[''];
        await page.waitForFunction(expected=>JSON.stringify([...document.querySelectorAll('#eventGiftBranch option')].map(x=>x.value))===JSON.stringify(expected),expected);
      };
      await saveBranches('14');
      assert.deepEqual(await page.locator('#eventGiftBranch option').evaluateAll(xs=>xs.map(x=>x.value)),['14']);
      assert.equal(await page.locator('#eventGiftBranch').inputValue(),'14');
      assert.equal(await page.locator('.event-gift-item').count(),0);
      await page.getByRole('button',{name:'清空可选分支奖励'}).click();
      await saveRewards();
      await page.waitForLoadState('networkidle');
      assert.deepEqual(JSON.parse(values.event_stage_gifts),JSON.parse(savedRewards));
      await saveBranches('');await assertDisabled();
      assert.match(await page.locator('#eventGiftStatus').innerText(),/尚未配置活动分支/);
      assert.equal(await page.locator('#eventGiftBranch').inputValue(),'');
      await saveBranches('14,13,13');
      assert.deepEqual(await page.locator('#eventGiftBranch option').evaluateAll(xs=>xs.map(x=>x.value)),['13','14']);
      await page.locator('#eventGiftBranch').selectOption('13');
      await page.locator('#eventGiftStage').selectOption('2');
      await page.waitForFunction(()=>document.querySelector('.event-gift-item .picked')?.innerText.includes('咖啡'));
      assert.equal(await page.locator('.event-gift-item').count(),1);
      await page.reload({waitUntil:'networkidle'});await page.locator('button[data-page="event"]').click();
      assert.equal(await page.locator('#eventGiftBranch').inputValue(),'13');
      assert.equal(await page.locator('#eventGiftStage').inputValue(),'2');
      assert.deepEqual(errors,[]);
      await page.locator('#eventGiftStage').selectOption('0');
      await page.getByRole('button',{name:'＋ 添加奖励物品'}).click();
      await page.locator('#eventGiftResults .result').first().waitFor();
      await page.locator('#eventGiftResults button').filter({hasText:'咖啡'}).click();
      await saveRewards();
      assert.equal(await page.locator('#eventGiftStage').inputValue(),'0');
      assert.deepEqual(JSON.parse(values.event_stage_gifts)['13']['0'],[{item:'fcb_coffee_can2',qty:1}]);
      await page.reload({waitUntil:'networkidle'});await page.locator('button[data-page="event"]').click();
      assert.equal(await page.locator('#eventGiftStage').inputValue(),'0');
      assert.equal(await page.locator('.event-gift-item').count(),1);
      // Presets only fill missing stages of enabled branches and remain a draft until saved.
      await saveBranches('13,19');
      await page.locator('#eventGiftBranch').selectOption('19');
      await page.locator('#eventGiftStage').selectOption('1');
      await page.getByRole('button',{name:'＋ 添加奖励物品'}).click();
      await page.locator('#eventGiftResults .result').first().waitFor();
      await page.locator('#eventGiftResults button').filter({hasText:'咖啡'}).click();
      await page.locator('.event-gift-item input[aria-label="物品数量"]').fill('7');
      const beforePreset=values.event_stage_gifts, beforePresetWrites=writes.length;
      await page.evaluate(()=>applyEventRewardPreset());
      assert.equal(values.event_stage_gifts,beforePreset);
      assert.equal(writes.length,beforePresetWrites);
      assert.match(await page.locator('#eventGiftPresetStatus').innerText(),/未保存/);
      let draft=JSON.parse(await page.locator('#eventStageGiftJson').textContent());
      assert.deepEqual(draft['13'],JSON.parse(beforePreset)['13']);
      assert.deepEqual(draft['19']['1'],[{item:'fcb_coffee_can2',qty:7}]);
      assert.equal(Object.keys(draft['19']).length,8);
      assert.equal(draft['20'],undefined);
      await page.evaluate(()=>applyEventRewardPreset());
      assert.deepEqual(JSON.parse(await page.locator('#eventStageGiftJson').textContent()),draft);
      await saveRewards();
      assert.deepEqual(JSON.parse(values.event_stage_gifts),draft);
      // Enabling the second branch later fills it without replacing the first branch.
      await saveBranches('13,19,20');
      await page.getByRole('button',{name:'填入所选预设'}).click();
      await page.waitForFunction(()=>JSON.parse(document.querySelector('#eventStageGiftJson').textContent)['20']);
      draft=JSON.parse(await page.locator('#eventStageGiftJson').textContent());
      assert.equal(draft['20']['1'][0].qty,50);
      assert.equal(draft['20']['2'][0].qty,50);
      assert.deepEqual(draft['19']['1'],[{item:'fcb_coffee_can2',qty:7}]);
      await saveRewards();
      await page.reload({waitUntil:'networkidle'});await page.locator('button[data-page="event"]').click();
      assert.deepEqual(JSON.parse(await page.locator('#eventStageGiftJson').textContent()),draft);
      // Historical presets stay separate, and all only fills enabled branches.
      await saveBranches('11,12,15,16,19,20');
      await page.locator('#eventGiftPreset').selectOption('anniversary-2025');
      await page.evaluate(()=>applyEventRewardPreset());
      let history=JSON.parse(await page.locator('#eventStageGiftJson').textContent());
      assert.equal(history['11'],undefined);
      assert.equal(history['15']['7'][0].item,'spinreel_pelengas_2025');
      assert.equal(history['16']['0'][0].item,'spin_5150_S8');
      const writesBeforeAll=writes.length;
      await page.locator('#eventGiftPreset').selectOption('all');
      await page.evaluate(()=>applyEventRewardPreset());
      history=JSON.parse(await page.locator('#eventStageGiftJson').textContent());
      assert.equal(Object.keys(history['11']).length,8);
      assert.equal(history['12']['0'][0].item,'spin_6020');
      assert.deepEqual(history['19'],draft['19']);
      assert.equal(writes.length,writesBeforeAll);
      assert.equal(await page.locator('#eventGiftPreset option[value="newyear-2024"]').count(),0);
      await saveRewards();
      await saveBranches('9,10,13');
      await page.locator('#eventGiftPreset').selectOption('all');
      await page.evaluate(()=>applyEventRewardPreset());
      const allDraft=JSON.parse(await page.locator('#eventStageGiftJson').textContent());
      assert.equal(allDraft['9'],undefined);
      assert.equal(allDraft['10'],undefined);
      assert.equal(Object.keys(allDraft['13']).length,8);
      await page.locator('#eventGiftBranch').selectOption('9');
      assert.ok(await page.getByRole('button',{name:'＋ 添加奖励物品'}).isEnabled());
      // New Year ordinary/extra branches have seven/ten stages plus completion.
      await saveBranches('13,14,17');
      const writesBeforeNewYear=writes.length;
      await page.locator('#eventGiftPreset').selectOption('newyear-2025');
      assert.ok(await page.getByRole('button',{name:'填入所选预设'}).isEnabled());
      await page.evaluate(()=>applyEventRewardPreset());
      let ny=JSON.parse(await page.locator('#eventStageGiftJson').textContent());
      assert.deepEqual(ny['13']['1'],history['13']['1']);
      assert.equal(Object.keys(ny['13']).length,8);
      assert.equal(Object.keys(ny['14']).length,11);
      assert.deepEqual(ny['14']['5'],[{item:'big_coffee_can25',qty:50}]);
      assert.equal(ny['17'],undefined);
      await page.locator('#eventGiftPreset').selectOption('all');
      await page.evaluate(()=>applyEventRewardPreset());
      ny=JSON.parse(await page.locator('#eventStageGiftJson').textContent());
      assert.equal(Object.keys(ny['17']).length,8);
      assert.equal(ny['18'],undefined);
      assert.deepEqual(ny['19'],history['19']);
      assert.equal(writes.length,writesBeforeNewYear);
      await saveRewards();
      await saveBranches('13,14,17,18');
      await page.locator('#eventGiftPreset').selectOption('newyear-2026');
      await page.evaluate(()=>applyEventRewardPreset());
      ny=JSON.parse(await page.locator('#eventStageGiftJson').textContent());
      assert.equal(Object.keys(ny['18']).length,11);
      assert.deepEqual(ny['18']['0'],[{item:'cast_10148_CF77XH_NY26',qty:1},{item:'bcr_6049',qty:1}]);
      await saveRewards();
      await page.reload({waitUntil:'networkidle'});await page.locator('button[data-page="event"]').click();
      assert.deepEqual(JSON.parse(await page.locator('#eventStageGiftJson').textContent()),ny);
      assert.deepEqual(errors,[]);
      await context.close();
    }
  }finally{await browser.close()}
});

import assert from 'node:assert/strict';
import {join} from 'node:path';
const artifacts=process.env.PARENT_TEST_ARTIFACTS;
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const origin=process.env.PARENT_TEST_ORIGIN;
const browser=await chromium.launch({channel:'chrome',headless:true});
try {
  const page=await browser.newPage({viewport:{width:1440,height:1000}});
  const errors=[];page.on('pageerror',error=>errors.push(error.message));
  await page.goto(origin+'/');
  await page.getByRole('heading',{name:'Welcome back.'}).waitFor();
  assert.equal((await page.request.get(origin+'/media/index.m3u8')).status(),401);
  await page.screenshot({path:join(artifacts,'parent-portal-login.png'),fullPage:true});
  await page.getByLabel('Email address').fill('test.parent@example.test');
  assert.equal(await page.locator('input[type=password]').count(),0);
  await page.getByRole('button',{name:'Continue'}).click();
  await page.getByRole('heading',{name:'A little verification.'}).waitFor();
  await page.locator('#otp-destination').filter({hasText:process.env.PARENT_TEST_OTP_TO}).waitFor();
  await page.screenshot({path:join(artifacts,'parent-portal-otp.png'),fullPage:true});
  const mail=await (await page.request.get(origin+'/test-mailbox')).json();
  assert.equal(mail.to,process.env.PARENT_TEST_OTP_TO);
  assert(!mail.body.includes('Password:'));
  const code=mail.body.match(/^Verification code: (\d{6})$/m)[1];
  assert.equal((await page.request.get(origin+'/media/index.m3u8')).status(),401);
  await page.getByLabel('Six-digit verification code').fill(code==='000000'?'111111':'000000');
  await page.getByRole('button',{name:'Open my classroom'}).click();
  await page.getByRole('alert').filter({hasText:'Code is incorrect'}).waitFor();
  await page.context().grantPermissions(['clipboard-read','clipboard-write']);
  for(const text of ['', '1234567', 'not a code']){
    await page.evaluate(value=>navigator.clipboard.writeText(value),text);
    await page.getByRole('button',{name:'Paste code',exact:true}).click();
  await page.locator('#paste-code:not(:disabled)').waitFor();
    await page.getByRole('alert').filter({hasText:'Copy the six-digit code'}).waitFor();
  }
  await page.evaluate(()=>navigator.clipboard.writeText('00 12-34'));
  await page.getByRole('button',{name:'Paste code',exact:true}).click();
  await page.locator('#paste-code:not(:disabled)').waitFor();
  assert.equal(await page.locator('#otp').inputValue(),'001234');
  await page.evaluate(()=>{window.originalReadText=navigator.clipboard.readText;Object.defineProperty(navigator.clipboard,'readText',{configurable:true,value:()=>Promise.reject(new Error('denied'))})});
  await page.getByRole('button',{name:'Paste code',exact:true}).click();
  await page.locator('#paste-code:not(:disabled)').waitFor();
  await page.getByRole('alert').filter({hasText:'Paste the code into the field'}).waitFor();
  await page.evaluate(()=>Object.defineProperty(navigator.clipboard,'readText',{configurable:true,value:window.originalReadText}));
  await page.locator('#otp').evaluate((input,value)=>{
    const clipboardData=new DataTransfer();clipboardData.setData('text',value.split('').join(' '));
    input.dispatchEvent(new ClipboardEvent('paste',{clipboardData,bubbles:true,cancelable:true}));
  },code);
  assert.equal(await page.locator('#otp').inputValue(),code);
  await page.locator('#otp').fill('');
  await page.evaluate(value=>navigator.clipboard.writeText(value),code);
  await page.getByRole('button',{name:'Paste code',exact:true}).click();
  await page.locator('#paste-code:not(:disabled)').waitFor();
  assert.equal(await page.locator('#otp').inputValue(),code);
  assert.equal((await page.request.get(origin+'/media/index.m3u8')).status(),401);
  await page.setViewportSize({width:390,height:844});
  assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
  await page.setViewportSize({width:1440,height:1000});
  await page.getByRole('button',{name:'Open my classroom'}).click();
  await page.getByRole('heading',{name:'Test classroom · Bluebird'}).waitFor();
  let playing=false;
  for(let attempt=0;attempt<80;attempt++){
    playing=await page.locator('video').evaluate(v=>v.videoWidth===640&&v.currentTime>2);
    if(playing)break;
    await new Promise(resolve=>setTimeout(resolve,250));
  }
  assert(playing,'The synthetic classroom video must play');
  assert.equal(await page.locator('video').evaluate(v=>v.controls),false);
  assert.equal(await page.locator('#toggle-play').count(),0);
  await page.getByRole('button',{name:'Fullscreen',exact:true}).click();
  await page.waitForFunction(()=>document.fullscreenElement?.id==='viewer');
  await page.getByRole('button',{name:'Exit fullscreen',exact:true}).click();
  await page.waitForFunction(()=>!document.fullscreenElement);

  assert.equal((await page.request.get(origin+'/media/camera-b/index.m3u8')).status(),404);
  await page.screenshot({path:join(artifacts,'parent-portal-classroom.png'),fullPage:true});
  await page.setViewportSize({width:390,height:844});
  assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
  await page.screenshot({path:join(artifacts,'parent-portal-mobile.png'),fullPage:true});
  await page.getByRole('button',{name:'Sign out'}).click();
  await page.getByRole('heading',{name:'Welcome back.'}).waitFor();
  assert.equal((await page.request.get(origin+'/media/index.m3u8')).status(),401);
  await page.reload();
  await page.getByRole('heading',{name:'Welcome back.'}).waitFor();
  assert.deepEqual(errors,[]);
  console.log('PASS: subpath portal, email-only sign-in, correct OTP destination, wrong-code handling, clipboard paste and permission fallback, gated camera, synthetic HLS playback, mobile layout and logout.');
}finally{await browser.close()}

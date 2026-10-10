'use strict';
const $=id=>document.getElementById(id);
let player=null,expiryTimer=null,codeTimer=null;
async function api(path,body){
  const response=await fetch('.'+path,{method:body===undefined?'GET':'POST',credentials:'same-origin',headers:body===undefined?{}:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});
  const data=await response.json();
  if(!response.ok){const error=new Error(data.error||'Something went wrong. Please try again.');error.status=response.status;throw error}
  return data;
}
function stopVideo(){if(player){player.destroy();player=null}const video=$('video');video.pause();video.removeAttribute('src');video.load();clearInterval(expiryTimer)}
function signIn(message=''){
  $('viewer').classList.remove('expanded');if(document.fullscreenElement)document.exitFullscreen().catch(()=>{});$('fullscreen').textContent='Fullscreen';stopVideo();clearInterval(codeTimer);$('classroom').hidden=true;$('access').hidden=false;$('login-form').hidden=false;$('otp-form').hidden=true;$('step-label').textContent='YOUR CLASSROOM CONNECTION';$('auth-error').textContent=message;$('otp').value='';
}
function videoUnavailable(message='The classroom feed is temporarily unavailable. Please try again.'){
  $('stream-status').textContent='Feed unavailable';$('stream-status').className='';$('video-message').hidden=false;$('video-message-text').textContent=message;$('retry').hidden=false;
}
function openVideo(){
  if(player){player.destroy();player=null}
  const video=$('video');video.pause();video.removeAttribute('src');video.load();
  $('stream-status').textContent='Connecting…';$('stream-status').className='';$('video-message').hidden=false;$('video-message-text').textContent='Opening your classroom window…';$('retry').hidden=true;
  if(window.Hls&&Hls.isSupported()){
    player=new Hls({maxBufferLength:10});
    player.on(Hls.Events.ERROR,(_event,data)=>{
      if(data.response?.code===401){signIn('Your viewing session has ended. Please sign in for a new code.');return}
      if(data.fatal){player?.destroy();player=null;videoUnavailable()}
    });
    player.loadSource('media/index.m3u8');player.attachMedia(video);
  }else if(video.canPlayType('application/vnd.apple.mpegurl')){video.src='media/index.m3u8'}
  else{videoUnavailable('This browser cannot play the classroom feed. Please use a current Chrome, Edge or Safari browser.')}
}
$('video').addEventListener('playing',()=>{$('video-message').hidden=true;$('stream-status').textContent='Live';$('stream-status').className='live'});
$('video').addEventListener('waiting',()=>{$('stream-status').textContent='Buffering…';$('stream-status').className=''});
$('video').addEventListener('error',()=>{if(!$('classroom').hidden)videoUnavailable()});
$('retry').addEventListener('click',openVideo);
async function showClassroom(){
  const user=await api('/api/me');clearInterval(codeTimer);$('access').hidden=true;$('classroom').hidden=false;$('class-name').textContent=user.className;$('account-email').textContent=user.email;$('view-error').textContent='';$('otp').value='';
  openVideo();clearInterval(expiryTimer);
  const update=()=>{const left=new Date(user.expiresAt)-Date.now();if(left<=0){signIn('Your viewing session has ended. Please sign in for a new code.');return}$('session-expiry').textContent=`Session ends in ${Math.ceil(left/60000)} min`};
  update();expiryTimer=setInterval(update,1000);
}
async function submit(form,work){const button=form.querySelector('button[type="submit"]');button.disabled=true;$('auth-error').textContent='';try{await work()}catch(error){$('auth-error').textContent=error.message}finally{button.disabled=false}}
$('login-form').addEventListener('submit',event=>{event.preventDefault();submit(event.currentTarget,async()=>{
  const result=await api('/api/login',{email:$('email').value});$('login-form').hidden=true;$('otp-form').hidden=false;$('step-label').textContent='ONE LAST STEP';$('otp-destination').textContent=result.destination;$('otp-description').textContent=result.teamApproval?'Your school team has been emailed a code. Contact them and enter the code they share with you.':'We have emailed your sign-in code. Enter it below to open your classroom.';$('otp').focus();
  clearInterval(codeTimer);const update=()=>{const left=Math.max(0,Math.ceil((new Date(result.expiresAt)-Date.now())/1000));$('code-expiry').textContent=left?`Code expires in ${Math.floor(left/60)}:${String(left%60).padStart(2,'0')}`:'This code has expired. Go back and sign in again.';if(!left)clearInterval(codeTimer)};update();codeTimer=setInterval(update,1000);
})});
function pasteCode(text){
  const code=text.replace(/[\s-]/g,'');
  if(!/^[0-9]{6}$/.test(code))return false;
  $('otp').value=code;$('auth-error').textContent='';$('otp').focus();return true;
}
$('otp').addEventListener('paste',event=>{
  if(pasteCode(event.clipboardData?.getData('text')||''))event.preventDefault();
});
$('paste-code').addEventListener('click',async()=>{
  const button=$('paste-code');button.disabled=true;
  try{
    if(!pasteCode(await navigator.clipboard.readText()))$('auth-error').textContent='Copy the six-digit code from your email first.';
  }catch{$('auth-error').textContent='Paste the code into the field, or type the six digits.'}
  finally{button.disabled=false;$('otp').focus()}
});
$('otp-form').addEventListener('submit',event=>{event.preventDefault();submit(event.currentTarget,async()=>{await api('/api/verify',{code:$('otp').value});await showClassroom()})});
async function logout(){await api('/api/logout',{});signIn();$('email').focus()}
$('restart').addEventListener('click',async()=>{try{await logout()}catch(error){$('auth-error').textContent=error.message}});
$('logout').addEventListener('click',async()=>{try{await logout()}catch(error){$('view-error').textContent=error.message}});
showClassroom().catch(error=>{signIn(error.status===401?'':error.message)});

$('fullscreen').addEventListener('click',async()=>{
  try{
    if(document.fullscreenElement)await document.exitFullscreen();
    else if($('viewer').requestFullscreen)await $('viewer').requestFullscreen();
    else {$('viewer').classList.toggle('expanded');$('fullscreen').textContent=$('viewer').classList.contains('expanded')?'Exit fullscreen':'Fullscreen'}
  }catch{$('view-error').textContent='Fullscreen is unavailable in this browser.'}
});
document.addEventListener('fullscreenchange',()=>{$('fullscreen').textContent=document.fullscreenElement?'Exit fullscreen':'Fullscreen'});

document.addEventListener('keydown',event=>{if(event.key==='Escape'){$('viewer').classList.remove('expanded');if(!document.fullscreenElement)$('fullscreen').textContent='Fullscreen'}});

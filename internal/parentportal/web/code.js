"use strict";
const value=document.getElementById('copy-value');
const button=document.getElementById('copy-button');
const status=document.getElementById('copy-status');
let code='',expires=0,timer;
function expired(){
  if(Date.now()<expires)return false;
  code='';value.value='';value.hidden=true;button.disabled=true;
  status.textContent='This code has expired. Return to your sign-in tab and request a fresh code.';
  clearInterval(timer);return true;
}
function readCode(){
  clearInterval(timer);
  const params=new URLSearchParams(location.hash.slice(1));
  code=params.get('code')||'';expires=Number(params.get('expires'))*1000;
  // The fragment is never sent to the server; remove it from the current history entry immediately.
  history.replaceState(null,'',location.pathname);
  value.value='';value.hidden=true;button.disabled=true;
  if(/^[0-9]{6}$/.test(code)&&Number.isSafeInteger(expires)&&expires>0){
    if(!expired()){
      value.value=code;value.hidden=false;button.disabled=false;
      status.textContent='Ready to copy. Keep this code private.';
      timer=setInterval(expired,1000);
    }
  }else{
    code='';status.textContent='This link is incomplete. Reopen it from your verification email, or copy the code directly from the email.';
  }
}
readCode();
window.addEventListener('hashchange',readCode);
button.addEventListener('click',async()=>{
  if(!code||expired())return;
  try{
    await navigator.clipboard.writeText(code);
    if(!expired())status.textContent='Copied! Return to your sign-in tab and paste the code.';
  }catch{
    if(expired())return;
    value.focus();value.select();
    status.textContent='Automatic copy is unavailable. Copy the selected code using your device’s Copy command.';
  }
});

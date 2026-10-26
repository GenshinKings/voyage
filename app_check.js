
const $=id=>document.getElementById(id);
const toast=(m,ms)=>{const t=$('toast');t.textContent=m;t.style.display='block';clearTimeout(t._h);t._h=setTimeout(()=>t.style.display='none',ms||2400)};
const esc=s=>String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
let STATE=null, CURRENT=null, LASTVERDICT=false;

async function api(path,body){const r=await fetch(path,body?{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}:{});const j=await r.json().catch(()=>({}));if(r.status>=400)throw new Error(j.error||j.Error||'请求失败');return j}
// 输入乱码防线（编码统一）：剪贴板编码不一致的文本带典型特征——提示但不拦发
function looksGarbled(t){
  if(!t)return false;
  if(t.indexOf('\uFFFD')>=0)return true;
  const dia=(t.match(/[♦◊◆]/g)||[]).length;
  const acc=(t.match(/[æøåÃâÅÞð]/g)||[]).length;
  return dia>=3&&acc>=1;
}
function garbleWarn(t){if(looksGarbled(t))toast('检测到疑似乱码输入（常见于剪贴板编码不一致）——已发送，但建议重新输入',4200)}

function fmtContent(raw){
  raw=raw||'';
  let hints='';
  // [PROPOSE-VOID]：旧提案作废（用户转向新方向）——收起提示
  if(raw.indexOf('[PROPOSE-VOID]')>=0){
    raw=raw.replace(/\[PROPOSE-VOID\]/g,'').trim();
    hints+='<span class="mark propose" style="color:var(--mut)">旧提案已收起——按新方向继续聊</span>';
  }
  // [PROPOSE] JSON 块折叠成一行提示（提案卡由方向视图单独渲染，消息流里不糊原始 JSON）
  let hint='';
  const i=raw.indexOf('[PROPOSE]');
  if(i>=0){
    let depth=0,end=-1,inStr=false;
    for(let j=i+9;j<raw.length;j++){
      const c=raw[j];
      if(inStr){if(c==='\\')j++;else if(c==='"')inStr=false;continue}
      if(c==='"')inStr=true;
      else if(c==='{')depth++;
      else if(c==='}'){depth--;if(depth===0){end=j+1;break}}
    }
    const before=raw.slice(0,i).replace(/\s+$/,'');
    const after=end>=0?raw.slice(end).replace(/^\s+/,''):'';
    raw=(before&&(before+'\n\n'))+after;
    hint='<span class="mark propose">📋 课程提案已生成'+(curView==='dir'?'——见下方卡片':'')+'</span>';
  }
  let html=esc(raw);
  html=html.replace(/\[PASS:partial\]/g,'<span class="mark partial">◐ 本节收尾（半懂也记录在案，继续往下走）</span>');
  html=html.replace(/\[PASS\]/g,'<span class="mark">✓ 本节收尾通过</span>');
  const body=hint+(html?'<br>'+html:'');
  return hints?hints+(body?'<br>'+body:''):body;
}
let curView='learn';
/* 小穗收敛轮②：实验室开关——默认只露主入口；⚗ 本会话展开，设置页「显示全部功能」持久展开 */
let LAB_OPEN=false;
try{LAB_OPEN=localStorage.getItem('voyage.lab')==='1'}catch(e){}
function applyLab(){
  document.querySelectorAll('.lab-extra').forEach(el=>el.classList.toggle('hidden',!LAB_OPEN));
  const t=document.getElementById('labToggle');
  if(t)t.classList.toggle('on',LAB_OPEN);
}
function toggleLab(){LAB_OPEN=!LAB_OPEN;try{localStorage.setItem('voyage.lab',LAB_OPEN?'1':'0')}catch(e){};applyLab()}
/* 版本自检：服务重启（发版）后驻留窗口自动刷新——用户永不需要手动 Ctrl+R */
let VER0=null;
async function checkVersion(){
  try{
    const v=(await api('/api/version')).started;
    if(VER0===null){VER0=v;return}
    if(v!==VER0){location.reload()}
  }catch(e){}
}
window.addEventListener('focus',checkVersion);
setInterval(checkVersion,30000);
checkVersion();
function updateChrome(v){
  // 「跳回今日知识点」只属于沉淀/关联两页；「今天的课」只在练习态（看完课的入口）
  const back=(v==='sediment'||v==='graph')&&STATE&&STATE.today;
  $('backToday').classList.toggle('hidden',!back);
  $('todayClass').classList.toggle('hidden',!(v==='learn'&&EXER&&!HIST));
  const kb=$('kicker');
  if(v==='sediment'){kb.textContent='SEDIMENT · 已经学会的';$('lessonTitle').textContent='沉淀'}
  else if(v==='dir'){kb.textContent='DIRECTION · 学习方向';$('lessonTitle').textContent='方向'}
  else if(v==='progress'){kb.textContent='PROGRESS · 我的进度';$('lessonTitle').textContent=PROG_TAB==='dungeon'?'实战副本':(PROG_TAB==='sediment'?'学会的':'知识地图')}
  else if(v==='graph'){kb.textContent='GRAPH · 知识地图';$('lessonTitle').textContent='关联'}
  else if(v==='set'){kb.textContent='SETTINGS · 模型与约束';$('lessonTitle').textContent='设置'}
  else if(HIST){kb.textContent='HISTORY · 对话历史'}
  else if(EXER){kb.textContent='PRACTICE · 实操练习'}
  else{kb.textContent='LEARNING · 对话式教学'}
  $('chatbox').style.display=(v==='learn'&&!HIST)?'flex':'none';
  const dis=!!HIST;$('input').disabled=dis;
  const b=$('chatbox').querySelector('button');if(b)b.disabled=dis;
  $('input').placeholder=HIST?'历史对话只读':(EXER?'写下解法/算式，或直接贴本地文件路径交作业…':(LASTVERDICT?'本节已收尾——可继续追问，或左侧选下一个':'回答检查题，或问点什么…（Enter 发送）'));
}
function switchTab(v){
  curView=v;
  document.querySelectorAll('.sbtn').forEach(t=>t.classList.toggle('on',t.dataset.v===v));
  $('chatOuter').classList.toggle('hidden',v!=='learn');
  $('dirView').classList.toggle('hidden',v!=='dir');
  $('progressView').classList.toggle('hidden',v!=='progress');
  if(v==='progress')applyProgTab();
  $('setView').classList.toggle('hidden',v!=='set');
  $('sediment').classList.toggle('hidden',v!=='sediment');
  $('graphWrap').classList.toggle('hidden',v!=='graph');
  updateChrome(v);
  if(v==='dir')renderDirection();
  if(v==='progress')renderProgress();
  if(v==='set')renderSettings();
  if(v==='sediment')renderSediment();
  if(v==='graph')renderGraph();
}

async function addSubject(){
  const name=$('subjectInput').value.trim();
  if(!name){toast('先输入科目名');return}
  const btn=$('subjectBox').querySelector('button');btn.disabled=true;btn.textContent='拆课中…';
  toast('AI 正在把「'+name+'」拆成课程…（约 20 秒）');
  try{
    await api('/api/subject',{name});
    $('subjectInput').value='';
    CURRENT=null;HIST=null;EXER=null;
    await loadState();
    toast('「'+name+'」课程就绪——第一个知识点已经丢给你',3600);
    if(STATE.today)openLesson(STATE.today.id); // 显式跳出新课，不等条件分支
  }catch(e){
    toast('开课失败：'+e.message+'（输入保留，可直接重试）',4200);
  }
  btn.disabled=false;btn.textContent='开课';
}

function findLessonMeta(nodeId){
  const ls=(STATE&&STATE.curriculum||{}).lessons||[];
  return ls.find(l=>l.id===nodeId)||null;
}

// 本地缓存直开（≤2ms 级）：已有会话不碰网络
function openCached(nodeId){
  leaveHistory();
  const sess=(STATE&&STATE.sessions||{})[nodeId];
  if(!sess)return false;
  CURRENT=nodeId;LASTVERDICT=false;
  renderSidebar();
  const meta=findLessonMeta(nodeId);
  $('lessonTitle').textContent=meta?meta.title:(sess.title||'');
  renderChat(sess.messages,sess.openingMeta);
  LASTVERDICT=endsClosed(sess);
  switchTab('learn');
  return true;
}

async function openLesson(nodeId){
  leaveHistory();
  if(openCached(nodeId))return;
  CURRENT=nodeId;LASTVERDICT=false;
  renderSidebar();
  $('lessonTitle').textContent='助教备课中…';
  try{
    const r=await api('/api/tutor/open',{nodeId});
    if(!STATE.sessions)STATE.sessions={};
    STATE.sessions[nodeId]=r.session;
    $('lessonTitle').textContent=r.session.title;
    renderChat(r.session.messages,r.session.openingMeta);
    switchTab('learn');
    LASTVERDICT=endsClosed(r.session);
    if(r.isNew)toast('新知识点丢给你了——跟着助教走');
  }catch(e){ $('lessonTitle').textContent='暂时打不开'; toast('失败：'+e.message) }
}
function endsClosed(sess){return (sess.messages||[]).some(m=>m.role==='assistant'&&/\[PASS/.test(m.content))}

// 开场指令特征（旧会话无 OpeningMeta 标记时的降级识别）
function isOpeningInstruction(m){
  return m&&m.role==='user'&&typeof m.content==='string'&&m.content.indexOf('知识点：《')===0&&m.content.indexOf('现在开始教学：')>=0;
}
function renderChat(msgs,meta){
  const c=$('chat');c.innerHTML='';
  // 开场元信息条（v3）：节序号+标题+承接——开场指令本身不渲染为用户气泡
  if(meta){
    c.insertAdjacentHTML('beforeend','<div class="meta-line"><span class="no">'+String(meta.index||1).padStart(2,'0')+'</span><span class="ti">'+esc(meta.title||'')+'<span class="why">'+esc(meta.why||'')+'</span></span><span class="op">开场</span></div>');
  }
  (msgs||[]).forEach(m=>{
    if(m.role==='tool')return;          // 工具结果帧：内部数据，不入聊天气泡
    if(!m.content)return;               // 纯工具调用帧（无文本）
    if(m.role==='user'&&isOpeningInstruction(m))return; // 开场指令（新旧会话一律）：不进气泡
    c.insertAdjacentHTML('beforeend','<div class="msg '+(m.role==='assistant'?'a':'u')+'"><div class="bubble">'+fmtContent(m.content)+'</div></div>');
  });
  $('view').scrollTop=$('view').scrollHeight;
}

async function send(){
  if(HIST)return;
  if(EXER)return sendExercise($('input').value.trim());
  if(!CURRENT){toast('先回答今天的检查题，或点一道练习');return}
  const text=$('input').value.trim();if(!text)return;
  garbleWarn(text);
  $('input').value='';
  const c=$('chat');c.insertAdjacentHTML('beforeend','<div class="msg u"><div class="bubble">'+esc(text)+'</div></div>');
  c.insertAdjacentHTML('beforeend','<div class="msg a"><div class="bubble" id="tmpwait">…</div></div>');
  $('view').scrollTop=$('view').scrollHeight;
  try{
    const r=await api('/api/tutor/send',{nodeId:CURRENT,text});
    const tmp=$('tmpwait');if(tmp)tmp.closest('.msg').remove();
    const sess=STATE.sessions[CURRENT];
    if(sess)sess.messages.push({role:'user',content:text},{role:'assistant',content:r.reply});
    c.insertAdjacentHTML('beforeend','<div class="msg a"><div class="bubble">'+fmtContent(r.reply)+'</div></div>');
    $('view').scrollTop=$('view').scrollHeight;
    if(r.verdict){LASTVERDICT=true;await loadState();renderVerdictActions(r.verdict)}
  }catch(e){const tmp=$('tmpwait');if(tmp)tmp.closest('.msg').remove();toast('失败：'+e.message)}
}

/* ---------- 小穗收敛轮①：今天三步（讲→练→副本），做完打勾，当前步高亮，主色唯一 ---------- */
function todaySteps(){
  const st=STATE||{};
  const today=st.today;
  // 步1 讲：今天的课存在=待讲；无（已收尾推进完）=✓
  const s1={done:!today,title:today?today.title:'今日已讲完',act:today?'继续讲':'明天再来'};
  // 步2 练：今日练习全过=✓；有未过=当前
  const exs=st.exercises||[];
  const doneEx=exs.filter(e=>e.status==='done').length;
  const s2={done:exs.length>0&&doneEx===exs.length,
    title:exs.length?(exs.find(e=>e.status!=='done')?exs.find(e=>e.status!=='done').title:'今日 '+exs.length+' 题已过'):'练习待生成',
    act:exs.find(e=>e.status!=='done')?'去做题':'已全部通过'};
  // 步3 副本：进行中=当前（继续）；无=待开（去进度页开影子）
  const act=(st.dungeons||[]).find(x=>x.status==='active');
  const s3=act
    ?{done:false,title:'影子副本',act:'继续 '+(act.cur||0)+'/'+((act.stages||[]).length)+' 关'}
    :{done:(st.trophies||[]).some(t=>(t.date||'').slice(0,10)===new Date().toISOString().slice(0,10)),
      title:'影子副本',act:'去开一个影子'};
  // 当前步：第一个未完成
  const steps=[{k:'讲',fn:'todayGoLearn()',...s1},{k:'练',fn:'todayGoPractice()',...s2},{k:'副本',fn:'todayGoDungeon()',...s3}];
  const cur=steps.findIndex(s=>!s.done);
  steps.forEach((s,i)=>{s.cur=(i===cur)});
  if((mood==='10'||mood==='30')&&steps[2]&&!steps[2].done){steps[2].light=true}
  return steps;
}
function isPaused(){
  const pu=STATE.pausedUntil;
  return pu&&new Date(pu)>new Date();
}
/* P0-3 体面暂停：我先停两周——不算断签、不推提醒、不显示落后；回来时一句欢迎 */
function setPaused(days){
  api('/api/pause',{days:days}).then(async r=>{
    await loadState();
    toast('已经为你按下暂停——到 '+r.pausedUntil+' 之前不推送、不断签。回来时一切都在。',4200);
    renderTodaySteps();
  }).catch(e=>toast('失败：'+e.message));
}
async function resumeNow(){
  await api('/api/pause/end',{});
  await loadState();
  toast('欢迎回来——我们从断点继续。');
  renderTodaySteps();
}
function renderTodaySteps(){
  const el=document.getElementById('planCard');if(!el)return;
  if(isPaused()){
    const until=new Date(STATE.pausedUntil).toLocaleDateString();
    el.innerHTML='<div class="pc" style="padding:16px 20px;text-align:center">'
      +'<div style="font-size:15px;font-weight:700;margin-bottom:4px">⏸ 暂停中（到 '+until+'）</div>'
      +'<div style="font-size:12px;color:var(--mut);margin-bottom:12px">这期间不算断签、不推送、没有落后——休息是计划的一部分。</div>'
      +'<button class="va primary" onclick="resumeNow()" style="border:1px solid var(--pink);background:var(--pink);color:#fff;padding:7px 18px;font-size:12.5px;border-radius:8px;cursor:pointer;font-weight:600">我回来了，从断点继续 →</button></div>';
    return;
  }
/* 小穗闭环P0-2 看见人：今天有多少时间？三选 → /api/mood 持久化 → 三步量随状态调整 */
let MOOD_ASKED=null;
try{MOOD_ASKED=localStorage.getItem('voyage.moodDate')===new Date().toISOString().slice(0,10)}catch(e){}
function moodPick(m){
  try{localStorage.setItem('voyage.moodDate',new Date().toISOString().slice(0,10))}catch(e){}
  MOOD_ASKED=true;
  try{localStorage.setItem('voyage.mood',m)}catch(e){}
  api('/api/mood',{mood:m,completed:false}).catch(()=>{});
  renderTodaySteps();
  toast(m==='10'?'只有 10 分钟——今天讲透一个点就够':(m==='30'?'半小时——讲一个 + 做一道':'有整块时间——三步全上'));
}
function moodRow(){
  if(MOOD_ASKED)return '';
  const b=(v,t)=>'<button class="va" onclick="moodPick('+JSON.stringify(v)+')" style="border:1px solid var(--line);background:var(--card);color:var(--ink);padding:7px 18px;font-size:12.5px;border-radius:9px;cursor:pointer">'+t+'</button>';
  return '<div style="background:var(--card);border:1px solid var(--line);border-radius:10px;padding:12px 16px;margin-bottom:10px">'
    +'<span style="font-size:13px;font-weight:600;margin-right:10px">今天有多少时间？</span>'
    +b('10','只有 10 分钟')+b('30','半小时')+b('90','有整块时间')
    +'<span style="font-size:10.5px;color:var(--mut);margin-left:8px">照实说就行——今天的量跟着你走</span></div>';
}
  /* P0-1 接续：隔天/冷启动 + 那节课还没收尾 → 一句话 + 一个按钮 */
  const ls=STATE.lastStop;
  const today=new Date().toISOString().slice(0,10);
  let resumeHTML='';
  if(ls&&ls.nodeId&&(ls.at||'').slice(0,10)!==today){
    const sess=(STATE.sessions||{})[ls.nodeId];
    const unfinished=!sess||!sess.verdict;
    if(unfinished){
      resumeHTML='<div style="background:var(--pink-soft,rgba(251,114,153,.12));border-radius:10px;padding:10px 14px;margin-bottom:10px;display:flex;align-items:center;gap:10px">'
        +'<span style="font-size:13px;flex:1">👋 上次你停在<b>「'+esc(ls.title)+'」</b>（'+esc(ls.step)+'）——今天先把这个补上。</span>'
        +'<button class="va primary" onclick="openLesson(\'+ls.nodeId+\')" style="border:1px solid var(--pink);background:var(--pink);color:#fff;padding:6px 16px;font-size:12px;border-radius:8px;cursor:pointer;font-weight:600">从那里继续 →</button></div>';
    }
  }
  const steps=todaySteps();
  const mood=(function(){try{return localStorage.getItem('voyage.mood')}catch(e){return null}})();
  let h='<div class="pc" style="padding:14px 20px">'+resumeHTML+moodRow()+'<div class="grouplabel" style="margin:0 0 10px">今天三步'+(mood?'<span style="letter-spacing:0;font-weight:500;text-transform:none;color:var(--mut);margin-left:8px">'+(mood==='10'?'· 只有 10 分钟：做第 1 步就够':(mood==='30'?'· 半小时：第 1+2 步':'· 整块时间：三步全上'))+'</span>':'')+'</div>';
  steps.forEach((s,i)=>{
    const primary=s.cur;
    h+='<div style="display:flex;align-items:center;gap:12px;padding:7px 0;'+(i<steps.length-1?'border-bottom:1px dashed var(--line);':'')+'">'
      +'<span style="width:22px;height:22px;flex:none;border-radius:50%;display:flex;align-items:center;justify-content:center;font-size:11px;font-weight:700;'
      +(s.done?'background:var(--green);color:#fff':(primary?'background:var(--pink);color:#fff':'background:var(--chip);color:var(--mut)'))+'">'+(s.done?'✓':i+1)+'</span>'
      +'<span style="flex:1;min-width:0;font-size:13.5px;font-weight:'+(primary?'600':'500')+'">'+esc(s.k)+' · '+esc(s.title)+(s.light?' <span style="font-size:10.5px;color:var(--mut)">（时间紧可跳过）</span>':'')+'</span>'
      +'<button class="'+(primary?'va primary':'va')+'" onclick="'+s.fn+'" style="border:1px solid var(--line);background:'+(primary?'var(--pink)':'var(--card)')+';color:'+(primary?'#fff':'var(--ink)')+';padding:6px 16px;font-size:12px;border-radius:8px;cursor:pointer;'+(primary?'border-color:var(--pink);font-weight:600;':'')+'">'+esc(s.act)+'</button>'
      +'</div>';
  });
  h+='<div style="margin-top:9px;font-size:10.5px;color:var(--mut)">做完打勾 · 当前三步会自动跟着你的进度走 · <span style="cursor:pointer;text-decoration:underline dotted" onclick="setPaused(14)">我先停两周</span></div></div>';
  el.innerHTML=h;
}
function todayGoLearn(){
  if(STATE.today){openLesson(STATE.today.id)}
  else{toast('今天的课已经讲完了——先做题或开影子副本')}
}
function todayGoPractice(){
  const ex=(STATE.exercises||[]).find(e=>e.status!=='done');
  if(ex){openExercise(ex.id)}
  else if((STATE.exercises||[]).length){toast('今天的练习已全部通过')}
  else{toast('练习在课程收尾后由助教生成')}
}
function todayGoDungeon(){
  const act=(STATE.dungeons||[]).find(x=>x.status==='active');
  if(act){openDungeon(act.id)}
  else{PROG_TAB='dungeon';switchTab('progress');toast('挑一个影子副本开门——通关才知道它是什么')}
}

/* P0-1 判定出口：再试一次 / 看解析 / 下一节（默认高亮下一节） */
function nextLessonId(){
  const ls=(STATE.curriculum||{}).lessons||[];
  const idx=ls.findIndex(l=>l.id===CURRENT);
  for(let i=idx+1;i<ls.length;i++){if(ls[i].status!=='done')return ls[i].id}
  for(let i=0;i<idx;i++){if(ls[i].status!=='done')return ls[i].id}
  return null;
}
function renderVerdictActions(verdict){
  const c=$('chat');
  const old=c.querySelector('.verdict-actions');if(old)old.remove();
  const nid=nextLessonId();
  const nl=((STATE.curriculum||{}).lessons||[]).find(l=>l.id===nid);
  const d=document.createElement('div');d.className='verdict-actions';
  d.innerHTML='<button class="va" onclick="retryVerdict()">↻ 再试一次</button>'
    +'<button class="va" onclick="explainVerdict()">📖 看解析</button>'
    +(nid?'<button class="va primary" onclick="gotoNext()">下一节：'+esc(nl?nl.title:'')+' →</button>':'')
    +'<span class="va-hint">'+(verdict==='partial'?'半懂已记录——再答一次可转全过':'Enter 直接继续')+'</span>';
  c.appendChild(d);
  $('view').scrollTop=$('view').scrollHeight;
}
function retryVerdict(){
  const ta=$('input');ta.focus();ta.placeholder='把同一道题再答一遍——半懂会转全过';
  toast('再答一次，助教会把履历更新为「知道」');
}
function explainVerdict(){
  const ta=$('input');ta.value='请把这道检查题的完整解析讲一遍——我哪里想对了，哪里还有偏差';
  send();
}
function gotoNext(){
  const nid=nextLessonId();
  if(nid){LASTVERDICT=null;openLesson(nid)}
  else toast('这一科已经全部收尾——去「我的进度」开个新影子副本试试');
}

// 练习作答：题目对话里发解法 → 助教批改 → [PASS] 记「用过」履历
async function sendExercise(text){
  if(!text)return;
  garbleWarn(text);
  $('input').value='';
  const c=$('chat');c.insertAdjacentHTML('beforeend','<div class="msg u"><div class="bubble">'+esc(text)+'</div></div>');
  c.insertAdjacentHTML('beforeend','<div class="msg a"><div class="bubble" id="tmpwait">批改中…</div></div>');
  $('view').scrollTop=$('view').scrollHeight;
  try{
    const r=await api('/api/exercise/answer',{id:EXER,text});
    const tmp=$('tmpwait');if(tmp)tmp.closest('.msg').remove();
    const ex=(STATE.exercises||[]).find(x=>x.id===EXER);
    if(ex)ex.messages.push({role:'user',content:text},{role:'assistant',content:r.reply});
    c.insertAdjacentHTML('beforeend','<div class="msg a"><div class="bubble">'+fmtContent(r.reply)+'</div></div>');
    $('view').scrollTop=$('view').scrollHeight;
    if(r.verdict){await loadState();toast('这道练习过了——履历记「用过」')}
  }catch(e){const tmp=$('tmpwait');if(tmp)tmp.closest('.msg').remove();toast('失败：'+e.message)}
}

async function loadState(){
  STATE=await api('/api/state');
  renderSidebar();
  renderToday();
  api('/api/review/due').then(d=>{FSDUE=d.cards||[];renderSidebar();}).catch(()=>{});
  api('/api/dungeon/list').then(d=>{STATE.dungeons=d.dungeons||[];renderSidebar();if(DN){const dd=(STATE.dungeons||[]).find(x=>x.id===DN);if(dd)renderDungeon(dd);}}).catch(()=>{});
  renderTodaySteps();
  if(!CURRENT&&!HIST&&!EXER&&STATE.today){if(!openCached(STATE.today.id))openLesson(STATE.today.id)}
}
/* ---------- FSRS 到期复习（评审 P1-5） ---------- */
let FSDUE=[],REV=null;
function openReview(id){
  const c=(FSDUE||[]).find(x=>x.id===id);if(!c)return;
  leaveHistory();
  REV=id;CURRENT=null;EXER=null;
  $('lessonTitle').textContent=c.title||c.id;
  $('kicker').textContent='REVIEW · 到期复习';
  const pv=c.previews||{};
  const btn=(r,label)=>"<button class=\"backbtn\" onclick=\"reviewRate('"+c.id+"',"+r+")\" style=\"margin:0 6px 8px 0\">"+label+(pv[String(r)]?"<div style=\"font-size:10.5px;color:var(--mut)\">下次 "+esc(pv[String(r)])+"</div>":"")+"</button>";
  $('chat').innerHTML='<div class="msg a"><div class="bubble"><b>《'+esc(c.title||c.id)+'》到期了。</b><br>先凭记忆想一想它讲了什么、能怎么用——然后诚实自评（自评准，安排才准）：<div style="margin-top:12px">'
    +btn(1,'忘了')+btn(2,'想不起来')+btn(3,'想起来')+btn(4,'轻松')+'</div>'
    +'<div class="mut" style="font-size:11.5px;margin-top:6px">想不起来的话，点左上回历史重看这节课的对话再评。</div></div></div>';
  switchTab('learn');
}
async function reviewRate(id,rating){
  try{
    await api('/api/complete-review',{id,rating});
    FSDUE=(FSDUE||[]).filter(x=>x.id!==id);
    REV=null;
    toast('复习完成——间隔已按你的记忆重新安排');
    renderSidebar();
    if(FSDUE.length)openReview(FSDUE[0].id);
    else{renderChat([{role:'assistant',content:'今天的到期复习清空了——遗忘防御上膛。继续推进新课或做练习都行。'}]);}
  }catch(e){toast('失败：'+e.message)}
}
async function completeMain(btn){
  const title=btn.dataset.t;
  try{
    await api('/api/complete-main',{title});
    toast('产出物推进完成——履历记「做过」');
    btn.disabled=true;btn.textContent='✓';
    await loadState();
  }catch(e){toast('失败：'+e.message)}
}
async function mood(wantMore){
  const completed=(STATE.exercises||[]).length>0&&(STATE.exercises||[]).every(e=>e.status==='done');
  try{
    await api('/api/mood',{mood:wantMore?'还想来':'今天够了',completed:completed});
    toast(wantMore?'好——明天继续。':'收工愉快，休息也是正当的。');
    renderToday();
  }catch(e){toast('失败：'+e.message)}
}
// 方向卡：我在哪 · 为什么学这个 · 接下来做什么（规则驱动，零延迟）
/* ---------- 学习页 v3：状态卡 + 今日折叠条 + 航路/清单（替换原方向卡+推进卡双卡） ---------- */
let TODAYFOLD=localStorage.getItem('todayFold')==='1';
function renderToday(){
  const gc=$('guideCard');if(!gc)return;
  const planEl=document.getElementById('planCard');if(planEl)planEl.innerHTML='';
  if(!STATE.curriculum||!(STATE.curriculum.lessons||[]).length){gc.innerHTML='';return}
  const lessons=STATE.curriculum.lessons||[];
  const doneN=lessons.filter(l=>l.status==='done').length;
  const today=STATE.today;
  const idx=lessons.findIndex(l=>today&&l.id===today.id);
  const cur=idx>=0?lessons[idx]:null;
  const prev=idx>0?lessons[idx-1]:null;
  const ex=STATE.exercises||[];
  const exLeft=ex.filter(e=>e.status!=='done').length;
  const dueN=(FSDUE||[]).length;
  const main=(STATE.plan&&STATE.plan.Main)||[];
  const goals=STATE.goals||[];
  let pct=0;
  if(goals.length>=3){
    const t0=new Date(new Date(goals[0].deadline).getTime()-21*864e5),t1=new Date(goals[2].deadline),now=new Date();
    if(t1>t0)pct=Math.max(0,Math.min(100,Math.round((now-t0)/(t1-t0)*100)));
  }
  const dateStr=new Date().toLocaleDateString('zh-CN',{month:'long',day:'numeric',weekday:'short'});
  const subj=(STATE.curriculum.subjects||[]).join('、');
  let h='<div class="status">'
    +'<span class="seg"><span class="k">日期</span><b>'+esc(dateStr)+'</b></span>'
    +'<span class="seg"><span class="k">在学</span><b>'+esc(subj)+'</b></span>'
    +'<span class="seg"><span class="k">课程进度</span><span class="big">'+(idx+1||doneN||1)+'<i> / '+lessons.length+'</i></span></span>'
    +'<span class="seg"><span class="k">冲刺</span><span class="big accent" style="font-size:16px">⛵ '+pct+'%</span></span>'
    +'</div>';
  h+='<div class="today">';
  h+='<div class="tcollapsed" onclick="toggleToday()">';
  h+='<div class="chip'+(cur?' current':'')+'"><span class="n">01</span>学 · '+esc(cur?cur.title:'开新课')+'</div><span class="asep">→</span>';
  h+='<div class="chip"><span class="n">02</span>练'+(exLeft?' <span class="cnt">'+exLeft+'</span>':'')+'</div><span class="asep">→</span>';
  h+='<div class="chip"><span class="n">03</span>复习'+(dueN?' <span class="cnt">'+dueN+'</span>':'')+'</div><span class="asep">→</span>';
  h+='<div class="chip"><span class="n">04</span>推进'+(main[0]&&main[0].Title?' · '+esc(String(main[0].Title).slice(0,10)):'')+'</div>';
  h+='<div class="tright"><div class="mroute"><span class="dot done"></span><span class="bar"></span><span class="ship">⛵</span><span class="bar"></span><span class="dot"></span><span class="bar"></span><span class="dot"></span><span style="margin-left:4px">'+pct+'%</span></div><span class="xbtn">'+(TODAYFOLD?'▴ 收起今日':'▾ 展开今日')+'</span></div>';
  h+='</div>';
  if(TODAYFOLD){
    h+='<div class="xexpanded">';
    h+='<div class="xhint"><span onclick="toggleToday()">▴ 收起今日</span></div>';
    if(goals.length>=3){
      const ps=[{p:0,done:pct>0,lb:'<b>启航</b>'},
                {p:33,done:pct>=33,lb:'<b>'+esc(goals[0].id)+' '+esc(goals[0].title)+'</b>'+String(goals[0].deadline).slice(5)},
                {p:66,done:pct>=66,lb:'<b>'+esc(goals[1].id)+' '+esc(goals[1].title)+'</b>'+String(goals[1].deadline).slice(5)},
                {p:100,done:pct>=100,lb:'<b>'+esc(goals[2].id)+' '+esc(goals[2].title)+'</b>'+String(goals[2].deadline).slice(5)}];
      h+='<div class="voyage"><div class="sea"></div>';
      ps.forEach(pt=>{h+='<div class="port'+(pt.done?' done':'')+'" style="left:'+pt.p+'%'+(pt.p===100?';transform:translateX(-100%)':'')+'"><span class="lb">'+pt.lb+'</span></div>';});
      h+='<div class="ship" style="left:'+pct+'%">⛵</div><div class="pct" style="left:'+pct+'%">'+pct+'%</div></div>';
    }
    const anchor=prev?(prev.status==='done'?'承接你已收尾的《'+esc(prev.title)+'》✓':'前一节《'+esc(prev.title)+'》还没收尾'):'这门课的起点';
    h+='<div class="tl primary"><div class="no">01</div><div class="bd"><div class="cat">学 · 对话教学</div><div class="ti">'+esc(cur?cur.title:'')+'</div><div class="why">'+anchor+'——'+esc(cur?cur.brief:'')+'</div></div><div class="act">'+(CURRENT?'':'<button class="btn primary" onclick="openLesson(STATE.today.id)">继续学习</button>')+'</div></div>';
    h+='<div class="tl"><div class="no">02</div><div class="bd"><div class="cat">练 · 实操练习</div><div class="ti">'+(exLeft?exLeft+' 道待做':'今天的练习已清空')+'</div><div class="why">'+(exLeft?'做完它们，收尾的知识点才落进关联图':'明天收尾新课后会有新的练习')+'</div></div>'+(exLeft?'<div class="act"><button class="btn ghost" onclick="switchTab(&#39;learn&#39;)">左侧选题</button></div>':'')+'</div>';
    h+='<div class="tl"><div class="no">03</div><div class="bd"><div class="cat">复习 · 到期 '+dueN+' 张</div><div class="ti">'+(dueN?esc((FSDUE[0]||{}).title||'')+(dueN>1?' 等 '+dueN+' 张':''):'暂无到期')+'</div><div class="why">凭记忆先想再评——间隔按你的遗忘曲线安排</div></div><div class="act">'+(dueN?'<button class="btn ghost" onclick="openReview(FSDUE[0].id)">开始复习</button>':'')+'</div></div>';
    main.slice(0,2).forEach((m,i)=>{
      const t=String(m.Title||'');
      h+='<div class="tl"><div class="no">0'+(4+i)+'</div><div class="bd"><div class="cat">推进 · 90 天冲刺</div><div class="ti">'+esc(t)+(m.Minutes?' · '+m.Minutes+' 分钟':'')+'</div></div><div class="act"><button class="btn" onclick="completeMain(this)" data-t="'+esc(t)+'">完成</button></div></div>';
    });
    h+='<div class="sline" id="statusLine"><span id="profileLine"></span><span id="fetchLine"></span><span id="notesLine"></span><span>孵化池 <b>'+((STATE.readings||[]).length)+'</b> 项目</span></div>';
    h+='</div>';
  }
  h+='</div>';
  gc.innerHTML=h;
  // 左缘装置同步冲刺进度
  const ep=document.getElementById('edgePill'),ev=document.getElementById('edgePct');
  if(ep)ep.style.height=Math.max(6,pct)+'%';
  if(ev)ev.textContent=pct+'%';
  if(TODAYFOLD){
    api('/api/profile').then(p=>{
      const lines=(p.content||'').split('\n').filter(l=>l.trim().indexOf('-')===0).map(l=>l.trim().slice(1).trim()).filter(Boolean);

      const el=document.getElementById('profileLine');
      if(el&&lines.length)el.innerHTML='🤖 助教记得你：<b>'+esc(lines.slice(0,2).join(' · '))+'</b>';
    }).catch(()=>{});
    api('/api/fetch').then(ft=>{
      const el=document.getElementById('fetchLine');
      if(!el)return;
      if(ft.failed){el.innerHTML='<span class="warn">昨日进货失败</span> · <span style="color:var(--pink);cursor:pointer;text-decoration:underline" onclick="runFetch()">重试</span>';return}
      if(ft.todayItems>0)el.innerHTML='今日进货 <b>'+ft.todayItems+' 份</b>（收尾时推荐）';
    }).catch(()=>{});
    api('/api/notes').then(n=>{
      const el=document.getElementById('notesLine');if(!el)return;
      const cnt=(n.list||[]).length;
      if(!cnt){el.innerHTML='还没有笔记——写进 <b>notes/</b> 助教每天帮你查';return}
      el.innerHTML='<span class="'+(n.noteReport&&n.noteReport.indexOf('⚠️')>=0?'warn':'')+'">笔记 <b>'+cnt+' 篇</b> · 助教'+(n.noteRun?'已检查'+(n.noteReport&&n.noteReport.indexOf('⚠️')>=0?'（有建议）':'（无问题）'):'待巡检')+'</span>';
    }).catch(()=>{});
  }
}
function toggleToday(){
  TODAYFOLD=!TODAYFOLD;
  localStorage.setItem('todayFold',TODAYFOLD?'1':'0');
  renderToday();
}
let GEN=0;
function renderSidebar(){
  const list=$('lessonList');list.innerHTML='';
  const subs=(STATE.curriculum&&STATE.curriculum.subjects)||[];
  $('subjFoot').innerHTML=subs.length?'在学：<b>'+subs.map(esc).join('</b> · <b>')+'</b>':'';
  // 副本进行中（AI 主导可见：入口永远在侧栏顶）
  const act=(STATE.dungeons||[]).find(x=>x.status==='active');
  if(act){
    list.innerHTML='<div class="lp" onclick="openDungeon(\''+act.id+'\')"><span class="st" style="color:var(--pink)">⚔</span><span>副本 · ？？？（'+((act.stages||[]).filter(sg=>sg.done).length)+'/'+((act.stages||[]).length)+' 关）</span></div>';
  }
  if(!STATE.curriculum||!(STATE.curriculum.lessons||[]).length){
    list.innerHTML='<div class="empty" style="margin-top:24px;padding:0 10px">输入想学的科目，<br>AI 会拆成课程，今天开始教</div>';return;
  }
  // 侧栏=到期复习（FSRS）+ 今天的实操练习（助教按实时进度出，点开一道做）
  const due=(FSDUE||[]).filter(c=>c.previews);
  if(due.length){
    list.innerHTML+='<div class="grouplabel" style="padding:8px 12px 0">到期复习 · '+due.length+'</div>';
    due.forEach(c=>{
      const el=document.createElement('div');
      el.className='lp rev-item'+(REV===c.id?' on':'');
      el.innerHTML='<span class="st" style="color:var(--pink)">↻</span><span>'+esc(c.title||c.id)+'<div class="lp-sub">到期 '+esc(String(c.due||'').slice(0,10))+'</div></span>';
      el.onclick=()=>openReview(c.id);
      list.appendChild(el);
    });
  }
  list.innerHTML+='<div class="grouplabel" style="padding:8px 12px 0">今天的练习</div>';
  const ex=STATE.exercises||[];
  if(!ex.length){
    list.innerHTML+='<div class="empty" style="margin-top:16px;padding:0 10px">'+(GEN?'助教正在出题…':(GENFAIL?'上完今天的课，练习就绪':'等助教出题…'))+'</div>';
    if(!GEN&&!GENFAIL)genExercises();
    return;
  }
  ex.forEach(e=>{
    const el=document.createElement('div');
    el.className='lp'+(e.status==='done'?' done':'')+(EXER===e.id?' on':'');
    const ref=(e.topic||'').trim();
    let sub='';
    if(ref)sub='主线：'+ref;
    if(e.context)sub+=(sub?' · ':'')+'语境：'+esc(e.context);
    el.innerHTML='<span class="st">'+(e.status==='done'?'✓':'·')+'</span><span>'+(e.review?'<span class="rtag">巩固</span>':'')+esc(e.title)+(sub?'<div class="lp-sub">'+sub+'</div>':'')+'</span>';
    el.onclick=()=>openExercise(e.id);
    list.appendChild(el);
  });
}
let GENFAIL=0;
async function genExercises(){
  GEN=1;
  try{
    const r=await api('/api/exercise/generate',{});
    STATE.exercises=r.exercises||[];
  }catch(e){GENFAIL=1;toast(e.message)}
  GEN=0;
  renderSidebar();
}
// 练习视图：像历史记录一样的对话排版，点开一道做一道
function openExercise(id){
  const e=(STATE.exercises||[]).find(x=>x.id===id);if(!e)return;
  leaveHistory();
  EXER=id;CURRENT=null;
  $('lessonTitle').textContent=e.title;
  $('kicker').textContent='PRACTICE · 实操练习'+(e.review?' · 错题回归':'');
  renderChat(e.messages);
  switchTab('learn');
}

/* ---------- 方向：和 AI 助教讨论学习方向，收敛后同步到学习 ---------- */
let DIRMSG=null,DIRPROPOSAL=null;
async function renderDirection(){
  const v=$('dirView');
  if(DIRMSG!==null){renderDirView();return}
  v.innerHTML='<div class="mut" style="margin:4px 0 12px">加载中…</div>';
  try{
    const d=await api('/api/direction/open');
    DIRMSG=d.messages||[];DIRPROPOSAL=d.proposal||null;
    renderDirView();
  }catch(e){v.innerHTML='<div class="empty">'+esc(e.message)+'</div>'}
}
function renderDirView(){
  const v=$('dirView');
  let h='<div class="mut" style="margin:4px 0 10px">和助教聊聊你想学什么、为什么、有多少时间——方向聊清楚了，课程就同步到「学习」。</div>';
  (DIRMSG||[]).forEach(m=>{h+='<div class="dir-msg '+(m.role==='assistant'?'a':'u')+'"><div class="bubble">'+fmtContent(m.content)+'</div></div>';});
  if(DIRPROPOSAL){
    const n=(DIRPROPOSAL.lessons||[]).length;
    h+='<div class="proposal-card"><h4>📋 课程提案 · '+esc(DIRPROPOSAL.subject)+'</h4><ol>';
    (DIRPROPOSAL.lessons||[]).forEach(l=>{h+='<li><b>'+esc(l.title)+'</b> — '+esc(l.brief||'')+'</li>';});
    h+='</ol><div class="mut" style="font-size:12px;margin:0 0 12px">共 '+n+' 节 · 每节约 30 分钟 · 收尾一节解锁下一节</div>';
    h+='<button class="backbtn" onclick="commitProposal()" style="background:var(--pink);color:#fff;border:none">✓ 同步到学习</button> <span class="mut" style="font-size:12px;margin-left:8px">想调整就继续在下面说</span></div>';
  }
  h+='<div class="dir-input"><textarea id="dirInput" placeholder="'+(DIRPROPOSAL?'想调整提案？直接说':'说说你想学什么，为什么想学…')+'"></textarea><button onclick="sendDirection()">发送</button></div>';
  v.innerHTML=h;
  const ta=document.getElementById('dirInput');
  ta.onkeydown=e=>{if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();sendDirection();}};
  v.scrollTop=v.scrollHeight;
}
async function sendDirection(){
  const ta=document.getElementById('dirInput');
  const text=ta.value.trim();if(!text)return;
  garbleWarn(text);
  ta.value='';
  DIRMSG.push({role:'user',content:text});
  DIRMSG.push({role:'assistant',content:'…'});
  renderDirView();
  try{
    const r=await api('/api/direction/send',{text});
    DIRMSG.splice(DIRMSG.length-1,1,{role:'assistant',content:r.reply});
    if(r.proposal!==undefined)DIRPROPOSAL=r.proposal; // null=旧提案已作废（转新方向），卡片收起
    renderDirView();
  }catch(e){DIRMSG.splice(DIRMSG.length-1,1);renderDirView();toast('失败：'+e.message)}
}
async function commitProposal(){
  if(!DIRPROPOSAL)return;
  try{
    await api('/api/direction/commit',{subject:DIRPROPOSAL.subject,lessons:DIRPROPOSAL.lessons});
    CURRENT=null;HIST=null;EXER=null;
    DIRMSG=null;DIRPROPOSAL=null;
    await loadState();
    switchTab('learn');
    toast('方向已同步——新课已丢给你');
  }catch(e){toast('失败：'+e.message)}
}

/* ---------- 设置：模型配置口 + AGENTS.md 约束 ---------- */
function renderSettings(){
  const v=$('setView');
  const oldRow=document.getElementById('remindRow');if(oldRow)oldRow.remove();
  v.innerHTML='<div class="mut" style="margin:4px 0 12px">加载中…</div>';
  setTimeout(appendRemindRow,50);
  api('/api/llm/profiles').then(d=>{
    let h='<div class="set-grid">';
    // 左列：模型配置
    h+='<div class="set-card"><div class="grouplabel">AI 助教模型</div>';
    const ps=d.profiles||[];
    ps.forEach(p=>{
      h+='<div class="lp'+(p.active?' on':'')+'" style="cursor:default"><span class="st">'+(p.active?'✓':'·')+'</span><span><b>'+esc(p.name)+'</b> · '+esc(p.model)+'<div class="lp-sub">'+esc(p.baseUrl)+' · key '+esc(p.keyMasked)+'</div></span>'+(p.active?'':'<span style="margin-left:auto"><button class="backbtn act-model" data-name="'+esc(p.name)+'" style="border-color:var(--line);background:var(--card);color:var(--ink)">启用</button></span>')+'</div>';
    });
    h+='<div class="gc-form" style="margin-top:12px">';
    h+='<div class="grouplabel" style="margin:0 0 6px">新增 / 更换模型（保存即启用）</div>';
    h+='<input id="pfName" placeholder="名称，如 deepseek / glm / 本地 ollama">';
    h+='<input id="pfUrl" placeholder="BaseURL，如 https://api.deepseek.com">';
    h+='<input id="pfModel" placeholder="模型名，如 deepseek-chat">';
    h+='<input id="pfKey" type="password" placeholder="API Key（编辑已有配置时留空=沿用旧 key）">';
    h+='<button class="backbtn" onclick="saveModel()" style="margin-top:8px">保存并启用</button></div>';
    h+='</div>';
    // 右列：内容进货
    h+='<div class="set-card"><div class="grouplabel">内容进货（AI 助教自动从 GitHub 抓学习材料）</div>';
    h+='<div id="fetchBox" style="font-size:12px;color:var(--mut)">加载中…</div></div>';
    // 右列下：笔记 + 迭代 叠放
    h+='<div class="set-card" style="margin-top:18px"><div class="grouplabel">笔记工作区（notes/ 笔记文件夹）——助教每天对照教学内容巡检</div>';
    h+='<div id="notesBox" style="font-size:12px;color:var(--mut)">加载中…</div></div>';
    h+='<div class="set-card" style="margin-top:18px"><div class="grouplabel">助教自迭代（低峰时段自主改进项目：反思→实现→测试→构建→提交）</div>';
    h+='<div id="iterBox" style="font-size:12px;color:var(--mut)">加载中…</div></div>';
    // wide：画像
    h+='<div class="set-card wide"><div class="grouplabel">学习者画像（每次教学/出题注入：按你的背景打比方、按目标定侧重；方向讨论同步后自动沉淀，可手编）</div>';
    h+='<textarea id="profileEdit" spellcheck="false"></textarea>';
    h+='<button class="backbtn" onclick="saveProfile()" style="margin-top:8px">保存画像</button></div>';
    // wide：AGENTS
    h+='<div class="set-card wide"><div class="grouplabel">AGENTS.md — 助教行为约束（每次调用注入，改完即生效）</div>';
    h+='<textarea id="agentsEdit" spellcheck="false">'+esc(d.agents||'')+'</textarea>';
    h+='<button class="backbtn" onclick="saveAgents()" style="margin-top:8px">保存约束</button></div>';
    h+='</div>'; // set-grid
    v.innerHTML=h;
    v.querySelectorAll('.act-model').forEach(b=>{b.onclick=()=>activateModel(b.dataset.name);});
    api('/api/profile').then(p=>{const el=document.getElementById('profileEdit');if(el)el.value=p.content||'';}).catch(()=>{});
    renderFetchBox();
    renderNotesBox();
    renderIterBox();
  }).catch(e=>{v.innerHTML='<div class="empty">'+esc(e.message)+'</div>';});
}
async function saveModel(){
  try{
    await api('/api/llm/profile',{name:val('pfName'),baseUrl:val('pfUrl'),model:val('pfModel'),key:val('pfKey')});
    toast('模型已切换为「'+val('pfName')+'」——教学与练习即刻生效');
    renderSettings();
  }catch(e){toast('失败：'+e.message)}
}
async function activateModel(name){
  try{
    await api('/api/llm/profile',{name:name,baseUrl:'-'});
    toast('已启用「'+name+'」');
    renderSettings();
  }catch(e){toast('失败：'+e.message)}
}
function val(id){const el=document.getElementById(id);return el?el.value.trim():''}
async function saveAgents(){
  try{
    await api('/api/agents',{content:document.getElementById('agentsEdit').value});
    toast('约束已保存——下一次 AI 调用即生效');
  }catch(e){toast('失败：'+e.message)}
}
async function saveProfile(){
  try{
    await api('/api/profile',{content:document.getElementById('profileEdit').value});
    toast('画像已保存——下一次教学/出题即生效');
  }catch(e){toast('失败：'+e.message)}
}
/* ---------- 内容进货：GitHub 自动抓取（定时+手动） ---------- */
function renderFetchBox(){
  const el=document.getElementById('fetchBox');if(!el)return;
  api('/api/fetch').then(d=>{
    const tools=d.toolsReady?'<span style="color:#3a9d5d">工具就绪</span>':'<span style="color:#c25b4e">未就绪（先配好模型）</span>';
    let h='<div>状态：'+(d.running?'<b>抓取进行中…</b>':tools)+' ｜ 每日 '+d.hour+' 点自动抓取</div>';
    h+='<div style="margin-top:6px;display:flex;gap:6px;flex-wrap:wrap;align-items:center">';
    h+='<input id="fetchProxy" placeholder="HTTP 代理（如 http://127.0.0.1:7897，直连留空）" style="flex:1;min-width:200px" value="'+esc(d.proxy||'')+'">';
    h+='<input id="fetchHour" type="number" min="0" max="23" value="'+(d.hour??7)+'" style="width:52px" title="每日抓取时刻">';
    h+='<button class="backbtn" onclick="saveFetchConf()">保存设置</button>';
    h+='<button class="backbtn" onclick="runFetch()" '+(d.running?'disabled':'')+' style="border-color:var(--pink);color:var(--pink)">'+(d.running?'抓取中…':'立即抓取')+'</button></div>';
    if(d.lastRun)h+='<div style="margin-top:6px">上次进货 '+esc(d.lastRun)+'：</div><pre style="white-space:pre-wrap;font-size:11.5px;color:var(--ink);background:var(--card);border:1px solid var(--line);border-radius:8px;padding:8px;margin-top:4px">'+esc(d.lastReport||'（无报告）')+'</pre>';
    if(d.failed)h+='<div style="margin-top:8px;display:flex;gap:10px;align-items:center;background:#fdecec;border:1px solid #f3c1c1;border-radius:8px;padding:8px 12px"><span style="color:#c0392b;font-weight:600">上次进货失败</span><button class="backbtn hot" onclick="runFetch()">重试抓取</button></div>';
    el.innerHTML=h;
  }).catch(e=>{el.innerHTML='<div class="empty">'+esc(e.message)+'</div>'});
}
async function saveFetchConf(){
  try{
    await api('/api/fetch/config',{proxy:val('fetchProxy'),hour:parseInt(val('fetchHour'))||7});
    toast('进货设置已保存');renderFetchBox();
  }catch(e){toast('失败：'+e.message)}
}
async function runFetch(){
  try{
    await api('/api/fetch/run',{});
    toast('进货已开始——AI 助教正在抓 GitHub，完成后报告出现在下方');
    setTimeout(renderFetchBox,1500);
  }catch(e){toast('失败：'+e.message)}
}
function renderNotesBox(){
  const el=document.getElementById('notesBox');if(!el)return;
  api('/api/notes').then(d=>{
    const list=d.list||[];
    let h='<div>共 '+list.length+' 篇'+(d.noteRun?' ｜ 助教上次巡检 '+esc(d.noteRun):' ｜ 尚未巡检')+' <button class="backbtn" onclick="runNoteReview()" style="margin-left:8px">立即巡检</button></div>';
    if(list.length)h+='<div style="margin-top:4px;color:var(--mut)">'+list.slice(0,6).map(x=>esc(x.name)).join(' · ')+(list.length>6?' 等 '+list.length+' 篇':'')+'</div>';
    if(d.noteReport)h+='<pre style="white-space:pre-wrap;font-size:11.5px;color:var(--ink);background:var(--card);border:1px solid var(--line);border-radius:8px;padding:8px;margin-top:6px">'+esc(d.noteReport)+'</pre>';
    el.innerHTML=h;
  }).catch(e=>{el.innerHTML='<div class="empty">'+esc(e.message)+'</div>'});
}
async function runNoteReview(){
  try{
    await api('/api/notes/review',{});
    toast('助教开始巡检笔记——对照今天的教学内容检查问题与改进点');
    setTimeout(renderNotesBox,1500);
  }catch(e){toast('失败：'+e.message)}
}
/* ---------- 助教自迭代 ---------- */
function renderIterBox(){
  const el=document.getElementById('iterBox');if(!el)return;
  api('/api/backlog').then(d=>{
    const ic=d.iter||{};
    let h='<div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">';
    h+='<label style="cursor:pointer"><input type="checkbox" id="iterEnabled" '+(ic.enabled?'checked':'')+'> 启用</label>';
    h+='低峰时刻 <input id="iterHour" type="number" min="0" max="23" value="'+(ic.hour??3)+'" style="width:52px"> 点';
    h+='<button class="backbtn" onclick="saveIterConf()">保存</button>';
    h+='<button class="backbtn" onclick="runReflect()" style="margin-left:auto">今日反思</button>';
    h+='<button class="backbtn" onclick="runIterate()" '+(d.iterating?'disabled':'')+' style="border-color:var(--pink);color:var(--pink)">'+(d.iterating?'迭代中…':'立即迭代一条')+'</button></div>';
    const obs=d.observations||[];
    if(obs.length){
      h+='<div style="margin-top:8px">';
      obs.forEach(o=>{
        const st={open:'待做',done:'✓已迭代',failed:'✗失败'}[o.status]||o.status;
        const col=o.status==='done'?'#3a9d5d':(o.status==='failed'?'#c25b4e':'var(--pink)');
        h+='<div style="border:1px solid var(--line);border-radius:8px;padding:6px 10px;margin-top:5px;font-size:11.5px">';
        h+='<div><span style="color:'+col+';font-weight:600">['+st+']</span> '+esc(o.observation)+'</div>';
        h+='<div style="color:var(--mut);margin-top:2px">缺 '+esc(o.gap)+' → 改：'+esc(o.suggestion)+'</div>';
        if(o.report)h+='<div style="color:var(--mut);margin-top:2px">报告：'+esc(o.report.slice(0,140))+(o.report.length>140?'…':'')+'</div>';
        h+='</div>';
      });
      h+='</div>';
    } else h+='<div style="margin-top:6px;color:var(--mut)">观察清单为空——助教每日教学后反思会自动补充</div>';
    if(ic.lastRun)h+='<div style="margin-top:6px;color:var(--mut)">上次迭代 '+esc(ic.lastRun)+(ic.lastReport?'：'+esc(String(ic.lastReport).slice(0,160)):'')+'</div>';
    el.innerHTML=h;
  }).catch(e=>{el.innerHTML='<div class="empty">'+esc(e.message)+'</div>'});
}
async function saveIterConf(){
  try{
    await api('/api/fetch/config',{iterEnabled:document.getElementById('iterEnabled').checked,iterHour:parseInt(val('iterHour'))||3});
    toast('自迭代设置已保存——助教会在低峰时段自主改进项目');renderIterBox();
  }catch(e){toast('失败：'+e.message)}
}
async function runReflect(){
  try{
    await api('/api/reflect/run',{});
    toast('助教开始今日教学反思——缺漏会进观察清单');setTimeout(renderIterBox,2000);
  }catch(e){toast('失败：'+e.message)}
}
async function runIterate(){
  try{
    await api('/api/iterate/run',{});
    toast('助教开始自主迭代：实现→测试→构建→提交（闸门全绿才算数）');setTimeout(renderIterBox,2000);
  }catch(e){toast('失败：'+e.message)}
}

/* ---------- 沉淀：已经学会的 ---------- */
function renderSediment(){
  const s=$('sediment');const st=STATE;if(!st)return;
  let html='<div class="counts"><div class="c"><div class="n">'+st.counts.known+'</div><div class="l">知道</div></div><div class="c"><div class="n">'+st.counts.used+'</div><div class="l">用过</div></div><div class="c"><div class="n">'+st.counts.done+'</div><div class="l">做过</div></div></div>';
  // 按学习科目分组（来源 tutor:科目 → 科目名；practice → 实操练习）
  const groups={};const order=[];
  (st.ledger||[]).forEach(k=>{
    const src=k.source||'';
    const g=(src.indexOf('tutor:')===0)?src.slice(6):'实操练习';
    if(!groups[g]){groups[g]=[];order.push(g)}
    groups[g].push(k);
  });
  order.forEach(g=>{
    html+='<div class="grouplabel" style="margin:14px 0 2px">'+esc(g)+'</div>';
    groups[g].forEach(k=>{
      const clickable=k.id&&(st.sessions||{})[k.id];
      html+='<div class="item d'+k.depth+(clickable?' clickable':'')+'"'+(clickable?' onclick=\'jumpLesson("'+k.id+'")\' title="看这段对话的完整历史"':'')+'><div class="t">'+esc(k.title)+'</div><div class="m">'+(k.depth===3?'做过':k.depth===2?'用过':'知道')+' · 首学 '+esc((k.firstAt||'').slice(0,10))+(clickable?' · 点击看完整对话':'')+'</div></div>';
    });
  });
  if(!(st.ledger||[]).length)html+='<div class="mut">履历还是空的。完成第一节的检查题，这里就长出第一条。</div>';
  /* 连锚：纪念卡与铸成它的知识点（副本 v2） */
  if((st.trophies||[]).length){
    html+='<div class="grouplabel" style="margin:26px 0 10px">连锚 —— 这些知识点共同铸成了你的作品</div>';
    st.trophies.forEach(tr=>{html+=anchorSVG(tr)});
  }
  html+='<div style="margin-top:24px"><a href="/evidence.html" target="_blank">对外证据页 →</a></div>';
  s.innerHTML=html;
}
/* 历史对话：从沉淀/关联进场，只读展示 */
let HIST=null;
let EXER=null; // 当前作答中的实操练习 id
function enterHistory(){HIST=true;updateChrome(curView||'learn')}
function leaveHistory(){HIST=null;EXER=null;updateChrome(curView||'learn')}
function backToday(){leaveHistory();if(STATE.today)openLesson(STATE.today.id)}
// 练习态切回今天的知识点教学对话
function openTodayClass(){EXER=null;if(STATE.today)openLesson(STATE.today.id)}
function jumpLesson(nodeId){
  const sess=(STATE&&STATE.sessions||{})[nodeId];
  if(!sess){toast('还没轮到它——到日子我自然丢给你');return}
  CURRENT=null;enterHistory();
  const meta=findLessonMeta(nodeId);
  $('lessonTitle').textContent=meta?meta.title:(sess.title||'');
  renderChat(sess.messages,sess.openingMeta);
  switchTab('learn');
}

/* ---------- 关联图：力导向 ---------- */
/* ---------- 关联图：弧线图（课程序为轴，练习真实调用为弧） ---------- */
let GNODES=[];
/* ---------- 关联图 v3：Obsidian 式视觉编码（大小=热度 · hover=聚焦邻域 · 曲线边=关系） ---------- */
let GRAPH_MOUSE=null; // hover 状态（mousemove 触发重绘）
async function renderGraph(){
  GNODES=[];
  GRAPH_MOUSE=null;
  const g=await api('/api/graph');
  // 入图资格=教学收尾（status done）
  const learned={};
  ((STATE.curriculum||{}).lessons||[]).forEach(l=>{if(l.status==='done')learned[l.id]=true});
  const nodes=(g.nodes||[]).filter(n=>learned[n.id]).filter((n,i,arr)=>arr.findIndex(m=>m.id===n.id)===i);
  const ctxEdges=(g.edges||[]).filter(e=>e.type==='context'&&learned[e.from]&&learned[e.to]);
  const seqEdges=(g.edges||[]).filter(e=>e.type==='sequence'&&learned[e.from]&&learned[e.to]);
  try{
    window.__graphErr='drawing';
    await new Promise(r=>setTimeout(r,60)); // 布局兜底。不用 rAF：后台页 rAF 会被节流
    const cv=$('graphCanvas');
    if(!cv||!cv.clientWidth){await new Promise(r=>setTimeout(r,60));}
    if(!cv||!cv.clientWidth)throw new Error('画布无尺寸');
    const dpr=window.devicePixelRatio||1;
    cv.width=cv.clientWidth*dpr;cv.height=cv.clientHeight*dpr;
    cv.style.width=cv.clientWidth+'px';cv.style.height=cv.clientHeight+'px';
    const ctx=cv.getContext('2d');
    // ---- 布局：FR 力导向（合力+退火位移，数学有界）----
    const W=cv.clientWidth,H=cv.clientHeight;
    nodes.forEach((n,i)=>{const ang=i/nodes.length*Math.PI*2;n.x=W/2+Math.cos(ang)*W*0.26;n.y=H/2+Math.sin(ang)*H*0.26;});
    const byId={};nodes.forEach(n=>byId[n.id]=n);
    nodes.forEach((n,i)=>{n.idx=i;}); // 力循环索引（丢失时 fX[undefined] 会把全场坐标污染成 NaN）
    // 度=真实调用热度（强边权重和）——节点大小与标签亮度都编码它
    const heat={};nodes.forEach(n=>heat[n.id]=0);
    ctxEdges.forEach(e=>{if(heat[e.from]!==undefined)heat[e.from]+= (e.weight||1);if(heat[e.to]!==undefined)heat[e.to]+=(e.weight||1);});
    const rad=id=>{const h=heat[id]||0;return 4.5+Math.min(h,8)*0.35;};
    const fX=nodes.map(()=>0),fY=nodes.map(()=>0);
    for(let it=0;it<380;it++){
      const temp=90*(1-it/380)+5;
      fX.fill(0);fY.fill(0);
      for(let i=0;i<nodes.length;i++)for(let j=i+1;j<nodes.length;j++){
        const dx=nodes[j].x-nodes[i].x,dy=nodes[j].y-nodes[i].y;
        let d=Math.hypot(dx,dy);if(d<1)d=1;
        const f=(9000+(rad(nodes[i].id)+rad(nodes[j].id))*90)/(d*d),ux=dx/d,uy=dy/d;
        fX[i]-=ux*f;fY[i]-=uy*f;fX[j]+=ux*f;fY[j]+=uy*f;
      }
      ctxEdges.forEach(e=>{
        const a=byId[e.from],b=byId[e.to];if(!a||!b)return;
        const dx=b.x-a.x,dy=b.y-a.y;
        let d=Math.hypot(dx,dy);if(d<1)d=1;
        const f=(d-170)*0.016;
        fX[a.idx]+=dx/d*f;fY[a.idx]+=dy/d*f;
        fX[b.idx]-=dx/d*f;fY[b.idx]-=dy/d*f;
      });
      seqEdges.forEach(e=>{
        const a=byId[e.from],b=byId[e.to];if(!a||!b)return;
        const dx=b.x-a.x,dy=b.y-a.y;
        let d=Math.hypot(dx,dy);if(d<1)d=1;
        const f=(d-160)*0.010;
        fX[a.idx]+=dx/d*f;fY[a.idx]+=dy/d*f;
        fX[b.idx]-=dx/d*f;fY[b.idx]-=dy/d*f;
      });
      nodes.forEach(n=>{
        const df=Math.hypot(fX[n.idx],fY[n.idx]);
        const cl=df>temp?temp/df:1;
        n.x+=fX[n.idx]*cl;n.y+=fY[n.idx]*cl;
        n.x+=(W/2-n.x)*0.004;n.y+=(H/2-n.y)*0.004;
        n.x=Math.max(rad(n.id)+16,Math.min(W-rad(n.id)-16,n.x));
        n.y=Math.max(rad(n.id)+58,Math.min(H-rad(n.id)-70,n.y));
      });
    }
    // ---- 绘制（可被 hover 状态重复调用）----
    const draw=()=>{
      ctx.clearRect(0,0,W,H);
      const focus=GRAPH_MOUSE;
      const adj=id=>focus&&(id===focus||ctxEdges.some(e=>(e.from===focus&&e.to===id)||(e.to===focus&&e.from===id))||seqEdges.some(e=>(e.from===focus&&e.to===id)||(e.to===focus&&e.from===id)));
      // 边：默认若有若无；hover 时焦点连线变蓝
      const edge=(e,color,width)=>{
        const a=byId[e.from],b=byId[e.to];if(!a||!b)return;
        const on=focus&&(e.from===focus||e.to===focus);
        ctx.globalAlpha=focus?(on?1:0.06):1;
        ctx.strokeStyle=on?'#0071e3':color;
        ctx.lineWidth=on?1.6:width;
        ctx.beginPath();ctx.moveTo(a.x,a.y);ctx.lineTo(b.x,b.y);ctx.stroke();
        ctx.globalAlpha=1;
      };
      seqEdges.forEach(e=>edge(e,'rgba(0,0,0,.07)',1));
      ctxEdges.forEach(e=>edge(e,'rgba(0,0,0,.16)',1));
      // 节点：小圆点 + 下方细字标签（未 hover 时安静灰）
      GNODES=[];
      nodes.forEach(n=>{
        const r=rad(n.id);
        const on=focus&&(n.id===focus||adj(n.id));
        const dim=focus&&!on;
        ctx.globalAlpha=focus?(on?1:0.1):0.85;
        ctx.beginPath();ctx.arc(n.x,n.y,r,0,7);
        ctx.fillStyle=focus&&n.id===focus?'#0071e3':(on?'#3b82c4':'#98a1ab');
        ctx.fill();
        const t=String(n.title||'');
        const label=t.length>16?t.slice(0,16)+'…':t;
        ctx.font=(focus&&n.id===focus)?'600 12.5px system-ui':'12px system-ui';
        ctx.fillStyle=focus?(n.id===focus?'#1d1d1f':(adj(n.id)?'#0071e3':'rgba(0,0,0,.3)')):'#6f7883';
        ctx.textAlign='center';
        ctx.fillText(label,n.x,n.y+r+14);
        ctx.globalAlpha=1;
        GNODES.push({id:n.id,x:n.x,y:n.y,r:r});
      });
    };
    draw();
    // ---- hover 交互：聚焦邻域 + DOM tooltip ----
    let tip=document.getElementById('graphTip');
    if(!tip){
      tip=document.createElement('div');tip.id='graphTip';
      tip.style.cssText='position:absolute;pointer-events:none;display:none;background:rgba(28,28,30,.88);backdrop-filter:blur(10px);color:#fff;font:12.5px system-ui;padding:7px 11px;border-radius:9px;z-index:5;white-space:nowrap;box-shadow:0 6px 18px rgba(0,0,0,.2)';
      cv.parentElement.appendChild(tip);
    }
    cv.onmousemove=ev=>{
      const rect=cv.getBoundingClientRect();
      const mx=ev.clientX-rect.left,my=ev.clientY-rect.top;
      const hit=GNODES.find(n=>Math.hypot(n.x-mx,n.y-my)<=Math.max(n.r+8,12));
      const changed=(hit?hit.id:null)!==GRAPH_MOUSE;
      GRAPH_MOUSE=hit?hit.id:null;
      if(changed)draw();
      if(hit){
        const t=byId[hit.id]||{};
        tip.innerHTML='<b>'+t.title.replace(/</g,'&lt;')+'</b><br>已收尾 · 被练习调用 '+ (heat[hit.id]||0) +' 次';
        tip.style.display='block';
        tip.style.left=(ev.clientX-rect.left+16)+'px';
        tip.style.top=(ev.clientY-rect.top+10)+'px';
        cv.style.cursor='pointer';
      }else{
        tip.style.display='none';
        cv.style.cursor='default';
      }
    };
    cv.onmouseleave=()=>{GRAPH_MOUSE=null;draw();tip.style.display='none';};
    window.__graphErr='';
  }catch(err){const m=String(err&&err.message||err);window.__graphErr=m;toast('关联图渲染失败：'+m)}
  // 点击命中 → 回到对应对话
  const cv2=$('graphCanvas');
  cv2.onclick=ev=>{
    const rect=cv2.getBoundingClientRect();
    const mx=ev.clientX-rect.left,my=ev.clientY-rect.top;
    const hit=GNODES.find(n=>Math.hypot(n.x-mx,n.y-my)<=Math.max(n.r+8,12));
    if(hit)jumpLesson(hit.id);else toast('这个知识点还没开课——完成前序知识它就来了');
  };
}

/* 启动 */
$('input').addEventListener('keydown',e=>{if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();send()}});
document.querySelectorAll('.sbtn[data-v]').forEach(t=>t.onclick=()=>switchTab(t.dataset.v));
applyLab();
/* ---------- 副本学习（用已学知识复现真实项目，通关揭晓恍然大悟） ---------- */
let DN=null; // 当前打开的副本 id
function openDungeon(id){
  const d=(STATE.dungeons||[]).find(x=>x.id===id);if(!d)return;
  leaveHistory();
  DN=id;CURRENT=null;EXER=null;
  $('lessonTitle').textContent=d.status==='active'?'副本 · ？？？（通关揭晓）':'副本 · '+d.title;
  $('kicker').textContent='DUNGEON · 副本复现';
  renderDungeon(d);
  switchTab('learn');
}
function renderDungeon(d){
  // 影子头部：？？？？？（通关揭晓）+ pip 进度轨
  let st='<div style="margin-bottom:16px"><div style="font-size:30px;font-weight:800;letter-spacing:8px">'+(d.status==='active'?'？？？？？':esc(d.title||''))+'</div>'
    +'<div style="font-size:11.5px;color:var(--mut);margin-top:2px">'+(d.status==='active'?'影子已开门 · 是什么，打完才知道':'已揭晓')+'</div>'
    +'<div class="pip-track" style="max-width:560px">'
    +(d.stages||[]).map((sg,i)=>'<div class="pip '+(i<d.cur?'done':(i===d.cur&&d.status==='active'?'cur':''))+'"></div>').join('')+'</div></div>';
  let h=st;
  // 已通关横幅
  if(d.status==='cleared'){
    h+='<div class="dn-clear">🎉 恭喜——你实现了「'+esc(d.project||d.title)+'」。回头看：它只用了你已经学会的东西。</div>';
  } else if(d.status==='abandoned'){
    h+='<div class="dn-clear" style="border-color:var(--line);color:var(--mut)">副本已放弃——随时可以再回来（项目与关卡还在）。</div>';
  }
  // 对话流
  h+='<div id="dnChat" class="dn-chat">';
  (d.messages||[]).forEach(m=>{
    if(!m.content)return;
    h+='<div class="msg '+(m.role==='assistant'?'a':'u')+'"><div class="bubble">'+fmtContent(m.content)+'</div></div>';
  });
  h+='</div>';
  // 输入与放弃
  if(d.status==='active'){
    h+='<div class="dir-input" style="max-width:none"><textarea id="dnInput" placeholder="提交这一关的成果 / 说卡住了 / 问点什么…"></textarea><button onclick="sendDungeon()">提交</button></div>';
    h+='<div style="text-align:right;margin-top:6px"><span class="mut" style="font-size:11px;cursor:pointer" onclick="abandonDungeon(\''+d.id+'\')">放弃副本（可随时回来）</span></div>';
  }
  const c=$('chat');
  c.innerHTML=h;
  const ta=document.getElementById('dnInput');
  if(ta)ta.onkeydown=e=>{if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();sendDungeon();}};
  $('view').scrollTop=$('view').scrollHeight;
}
async function sendDungeon(){
  const ta=document.getElementById('dnInput');if(!ta)return;
  const text=ta.value.trim();if(!text)return;
  const d=(STATE.dungeons||[]).find(x=>x.id===DN);if(!d)return;
  ta.value='';
  d.messages.push({role:'user',content:text},{role:'assistant',content:'…'});
  renderDungeon(d);
  try{
    const r=await api('/api/dungeon/send',{id:DN,text});
    const nd=r.dungeon;
    const old=d.messages;
    Object.assign(d,nd);
    if(!nd.messages||!nd.messages.length)d.messages=old;
    renderDungeon(d);
    if(r.reveal){ // 一锤揭晓：纪念卡生成，仪式开始
      setTimeout(()=>showReveal(r.reveal,async()=>{DN=null;await loadState();switchTab('dungeon');}),900);
    }
  }catch(e){d.messages.splice(d.messages.length-2,2);renderDungeon(d);toast('失败：'+e.message)}
}
async function abandonDungeon(id){
  try{
    await api('/api/dungeon/abandon',{id});
    const d=(STATE.dungeons||[]).find(x=>x.id===id);if(d)d.status='abandoned';
    DN=null;renderSidebar();switchTab('dungeon');
    toast('副本已收起——项目与关卡都留着，随时回来');
  }catch(e){toast('失败：'+e.message)}
}
// AI 导读：助教深入项目源码后产出结构化消化（是什么/核心概念/怎么用/复现路径）
/* ---------- 影子副本 v2（demo/shadow-dungeon 移植） ---------- */
/* 影子候选卡 / 进行中卡 / 收藏墙 / 雾散开门 / 一锤揭晓 / 沉淀连锚 */

let PROG_TAB='dungeon';
function renderProgress(){
  applyProgTab();
  (function(){
    const el=document.getElementById('progStats');if(!el)return;
    const st=STATE.stats||{};
    const today=new Date().toISOString().slice(0,10);
    const ds=(st.daySteps||{})[today]||[0,3];
    let avg='—';
    if((st.stepMins||[]).length){avg=Math.round(st.stepMins.reduce((a,b)=>a+b,0)/st.stepMins.length)+' 分钟'}
    if(isPaused()){
      el.textContent='暂停中——休息是计划的一部分，回来时我们从断点继续。';
    }else{
      if((st.streakCount||0)>=1){
        el.textContent='今天第 '+Math.min(ds[0]+1,ds[1])+'/'+ds[1]+' 步 · 已经一起走过 '+(st.streakCount||0)+' 天 · 平均每步 '+avg;
      }else{
        el.textContent='今天第 '+Math.min(ds[0]+1,ds[1])+'/'+ds[1]+' 步 · 新的开始，按你的节奏来 · 平均每步 '+avg;
      }
    }
  })();
  if(PROG_TAB==='dungeon')renderDunHall();
  else if(PROG_TAB==='sediment')renderSediment();
  else if(PROG_TAB==='opp')renderOpp();
  else renderGraph();
}
function applyProgTab(){
  const pv=$('progressView');
  if(!pv||pv.classList.contains('hidden'))return;
  pv.querySelectorAll('.ptab').forEach(t=>t.classList.toggle('on',t.dataset.pt===PROG_TAB));
  $('dunHallView').classList.toggle('hidden',PROG_TAB!=='dungeon');
  $('sediment').classList.toggle('hidden',PROG_TAB!=='sediment');
  $('graphWrap').classList.toggle('hidden',PROG_TAB!=='graph');
  const ov=document.getElementById('oppView');
  if(ov)ov.classList.toggle('hidden',PROG_TAB!=='opp');
}
function switchProgTab(t){PROG_TAB=t;if(curView!=='progress')switchTab('progress');else{applyProgTab();renderProgress();updateChrome('progress')}}
/* P1 去处：机会页（手工维护 opportunities.json——真实 issue/实习/接单入口） */
let OPPS=null;
async function renderOpp(){
  const v=$('oppView');
  if(OPPS){drawOpp();return}
  v.innerHTML='<div class="mut" style="padding:20px 0">加载中…</div>';
  try{
    const r=await api('/api/opportunities');
    OPPS=r.list||[];
  }catch(e){OPPS=[]}
  drawOpp();
}
function drawOpp(){
  const v=$('oppView');if(!v)return;
  const meta={issue:['真实 issue','🪡'],live-issue:['真实 issue · 按你已学的拉','🟢'],intern:['实习','🎓'],gig:['接单','💼']};
  let h='<div class="grouplabel">去处 —— 学的东西通向真实世界</div>';
  h+='<div style="font-size:11.5px;color:var(--mut);margin-bottom:12px">每一条都是现在就能点开看的东西。不着急——练到哪一步，哪一步的门口就为你开着。</div>';
  (OPPS||[]).forEach(o=>{
    const m=meta[o.type]||['机会','•'];
    h+='<div class="dh-item"><div><div class="dh-t">'+m[1]+' '+esc(o.title)+' <span class="chip" style="margin-left:6px">'+m[0]+'</span></div>'
      +'<div class="dh-sub">'+esc(o.note||'')+'</div></div>'
      +'<a class="btn ghost" href="'+esc(o.link)+'" target="_blank">去看看 →</a></div>';
  });
  if(!(OPPS||[]).length)h+='<div class="empty" style="margin-top:30px">机会清单待补——教务会手工维护真实入口。</div>';
  v.innerHTML=h;
}
function starsHTML(n){let h='';for(let i=1;i<=3;i++)h+=i<=n?'★':'<span class="off">★</span>';return '<span class="stars">'+h+'</span>'}
function tierHTML(t){const names=['巩固','拉伸','跃迁'];return '<span class="tier t'+t+'">'+(names[t]||'巩固')+'</span>'}
function reqHTML(req){return '<div class="req">'+(req||[]).map(r=>'<span class="chip'+(r.miss?' miss':'')+'">'+esc(r.n)+(r.miss?' · 未掌握':'')+'</span>').join('')+'</div>'}

/* 大厅：进行中 / 影子候选 / 收藏墙（数据源全部影子化，无项目名——ADR 0001） */

/* 孵化影子（未导读项目 → 助教导读+定档，一次点完） */
async function hatchShadow(id,btn){
  btn.disabled=true;btn.textContent='助教深入项目中…（1-2 分钟）';
  try{
    const r=await api('/api/reading/brief',{id});
    toast('影子已孵化——看看它需要什么');
    await loadState();
    renderDunHall();
  }catch(e){btn.disabled=false;btn.textContent='孵化影子';toast('失败：'+e.message)}
}

/* 开门雾散 → 进入副本 */
async function startDungeon(readingId,btn){
  btn=btn||event.target;
  btn.disabled=true;btn.textContent='开门中…';
  let fog=document.getElementById('shadowFog');
  if(!fog){fog=document.createElement('div');fog.className='fog';fog.id='shadowFog';fog.innerHTML='<div class="fog-q">？？？？？</div>';document.body.appendChild(fog);}
  fog.classList.remove('play');void fog.offsetWidth;fog.classList.add('play');
  setTimeout(async()=>{
    try{
      const r=await api('/api/dungeon/start',{readingId});
      if(!STATE.dungeons)STATE.dungeons=[];
      STATE.dungeons.push(r.dungeon);
      DN=r.dungeon.id;
      openDungeon(DN);
    }catch(e){toast(e.message);if(btn){btn.disabled=false;btn.textContent='开门'}}
  },470);
  setTimeout(()=>fog.classList.remove('play'),1050);
}

/* 一锤揭晓仪式（sendDungeon 响应带 reveal 时触发） */
function showReveal(rv,done){
  let ov=document.getElementById('shadowReveal');
  if(!ov){
    ov=document.createElement('div');ov.className='reveal';ov.id='shadowReveal';
    ov.innerHTML='<div class="rv-qs"><span>？</span><span>？</span><span>？</span><span>？</span><span>？</span></div>'
      +'<div class="rv-card" id="rvCard"></div><div class="rv-text" id="rvText"></div>'
      +'<button class="rv-go" id="rvGo">收入收藏墙 →</button>';
    document.body.appendChild(ov);
    const card=ov.querySelector('#rvCard');
    for(let i=0;i<16;i++){
      const m=document.createElement('i');m.className='rv-mote';
      m.style.left=(12+Math.random()*76)+'%';m.style.top=(30+Math.random()*45)+'%';
      m.style.animationDelay=(0.7+Math.random()*1.1)+'s';
      m.style.width=m.style.height=(2+Math.random()*3)+'px';
      card.appendChild(m);
    }
  }
  ov.querySelector('#rvCard').innerHTML='<div class="t-date">复现<br>'+esc(rv.short)+'<br>2026</div>'
    +'<div class="t-badge">🏆 复现成功 · 影子揭晓</div><div class="t-name">'+esc(rv.name)+'</div>'
    +'<div class="t-impact">'+esc(rv.impact)+'</div>'
    +'<div class="t-meta">'+(rv.nodes||[]).map(n=>'<span>'+esc(n)+'</span>').join('')+'</div>';
  ov.querySelector('#rvText').innerHTML='<b>恭喜——你做出了「'+esc(rv.name)+'」。</b><small>回头看：它只用了你已经学会的东西。</small>';
  ov.classList.add('show');ov.classList.remove('revealing');
  requestAnimationFrame(()=>requestAnimationFrame(()=>ov.classList.add('revealing')));
  ov.querySelector('#rvGo').onclick=()=>{
    ov.classList.remove('show','revealing');
    done&&done();
  };
}

/* 纪念卡详情（flip 弹入） */
/* 小穗二轮建议3：预览优先——先在页面里看简历，再决定要不要落盘打开（半夜不弹窗） */
async function exportResume(btn){
  if(btn){btn.disabled=true;btn.textContent='生成中…'}
  try{
    const r=await api('/api/resume/doc');
    resumePreview(r.markdown||'');
  }catch(e){toast(e.message)}
  if(btn){btn.disabled=false;btn.textContent='📜 生成实践简历（Markdown，每条挂可点开的证据）'}
}
function resumePreview(md){
  let ov=document.getElementById('resumePv');
  if(ov)ov.remove();
  ov=document.createElement('div');ov.className='tdetail';ov.id='resumePv';
  ov.innerHTML='<div class="td-card" style="width:560px;max-height:82vh;overflow-y:auto">'
    +'<button class="td-close" onclick="this.closest(\'.tdetail\').remove()">✕</button>'
    +'<div class="t-badge">📜 实践简历 · 预览</div>'
    +'<pre style="white-space:pre-wrap;font-size:12px;line-height:1.75;background:var(--paper);border:1px solid var(--line);border-radius:10px;padding:14px;margin-top:10px">'+esc(md)+'</pre>'
    +'<div style="display:flex;gap:10px;justify-content:flex-end;margin-top:12px">'
    +'<button class="btn ghost" onclick="this.closest(\'.tdetail\').remove()">关闭</button>'
    +'<button class="btn primary" id="rvOpenBtn">打开文件（生成 resume.md 并定位）</button></div></div>';
  document.body.appendChild(ov);
  ov.querySelector('#rvOpenBtn').onclick=async e=>{
    e.target.disabled=true;e.target.textContent='已生成并打开 ✓';
    try{const r=await api('/api/resume/export',{});
      toast('已落盘并定位：'+(r.path||'resume.md'),4200);
      setTimeout(()=>{ov.remove()},800);
    }catch(err){toast('失败：'+err.message);e.target.disabled=false}
  };
  ov.addEventListener('click',e=>{if(e.target===ov)ov.remove()});
}
function trophyDetail(name){
  const tr=(STATE.trophies||[]).find(x=>x.name===name);if(!tr)return;
  const ov=document.createElement('div');ov.className='tdetail';
  ov.innerHTML='<div class="td-card"><button class="td-close">✕</button>'
    +'<div class="t-badge">🏆 复现成功 · 纪念卡</div><div class="td-name">'+esc(tr.name)+'</div>'
    +'<div class="td-repo">参考来源（被复现的项目）：'+esc(tr.repo)+'</div><div class="td-impact">'+esc(tr.impact)+'</div>'
    +'<div class="grouplabel" style="margin:16px 0 8px">我的证据（写进简历——你的仓库 / 本地文件路径）</div>'
    +'<div style="display:flex;gap:8px"><input id="tdEv" placeholder="'+esc(tr.userEvidence||'https://github.com/你的用户名/你的作品 或 本地文件路径')+'" style="flex:1;border:1px solid var(--line);border-radius:8px;padding:8px 12px;font:inherit;font-size:12.5px;background:var(--card);color:var(--ink)"><button class="btn ghost" id="tdEvSave">保存</button></div>'
    +(tr.userEvidence?'<div style="font-size:11px;color:var(--green);margin-top:6px">✓ 已挂证据：'+esc(tr.userEvidence)+'</div>':'<div style="font-size:11px;color:var(--mut);margin-top:6px">还没挂——挂上它简历才有说服力（这是你做出来的东西）</div>')
    +'<div class="grouplabel" style="margin:16px 0 8px">用到的知识点（连锚已写入沉淀）</div>'
    +(tr.nodes||[]).map(n=>'<span class="td-node" onclick="switchTab(\'graph\')">'+esc(n)+'</span>').join('')
    +'<div style="margin-top:18px;display:flex;justify-content:space-between;align-items:center"><span class="td-cleared">复现于 '+esc(tr.date)+'</span><button class="btn ghost" onclick="this.closest(\'.tdetail\').remove()">关闭</button></div></div>';
  document.body.appendChild(ov);
  ov.querySelector('#tdEvSave').onclick=async()=>{
    const v=ov.querySelector('#tdEv').value.trim();
    if(!v){toast('贴上你的仓库链接或本地文件路径');return}
    try{
      await api('/api/trophy/evidence',{dungeonId:tr.dungeonId,evidence:v});
      await loadState();
      ov.remove();trophyDetail(name);
      toast('证据已挂进这张纪念卡——简历里会引用它');
    }catch(e){toast('失败：'+e.message)}
  };
  ov.addEventListener('click',e=>{if(e.target===ov||e.target.classList.contains('td-close'))ov.remove()});
}

/* 沉淀连锚图（显式布局表——小规模离散布局直接查表，防节点重叠） */
function anchorSVG(tr){
  const nodes=tr.nodes||[];
  const W=660,H=210,midY=105,cx=500,cy=midY;
  const LAYOUTS={1:[[110,100]],2:[[110,62],[150,140]],3:[[110,44],[160,105],[110,166]],4:[[95,44],[210,74],[95,150],[210,166]]};
  const layout=LAYOUTS[nodes.length]||LAYOUTS[3];
  const pos=nodes.map((n,i)=>({n,x:layout[i]?layout[i][0]:110,y:layout[i]?layout[i][1]:100}));
  let s='<svg class="anchor-svg" viewBox="0 0 '+W+' '+H+'" role="img" aria-label="'+esc(nodes.join('、'))+'连向'+esc(tr.name)+'">';
  pos.forEach((p,i)=>{
    const y2=cy+(i-(pos.length-1)/2)*16;
    s+='<line class="an-earned" x1="'+(p.x+80)+'" y1="'+p.y+'" x2="'+(cx-92)+'" y2="'+y2+'"/>';
  });
  if(pos.length>1)s+='<path class="an-ctx" d="M '+(pos[0].x+62)+' '+(pos[0].y+14)+' C '+(pos[0].x+120)+' '+(pos[0].y+26)+', '+(pos[1].x-60)+' '+(pos[1].y-26)+', '+(pos[1].x+18)+' '+(pos[1].y-20)+'"/>';
  pos.forEach(p=>{
    s+='<g><rect class="an-node" x="'+(p.x-8)+'" y="'+(p.y-17)+'" width="78" height="32" rx="9"/><text class="an-label" x="'+(p.x+31)+'" y="'+(p.y+4)+'" text-anchor="middle">'+esc(p.n)+'</text></g>';
  });
  s+='<g><rect class="an-tro" x="'+(cx-84)+'" y="'+(cy-46)+'" width="168" height="92" rx="14" transform="rotate(45 '+cx+' '+cy+')"/>'
    +'<text class="an-label" x="'+cx+'" y="'+(cy-4)+'" text-anchor="middle" font-weight="700">'+esc(tr.name)+'</text>'
    +'<text class="an-sub" x="'+cx+'" y="'+(cy+15)+'" text-anchor="middle">🏆 '+esc(tr.short)+'</text></g>';
  s+='<text class="an-note" x="'+((cx+60)/2)+'" y="'+(cy-58)+'" text-anchor="middle">共同铸成 →</text></svg>';
  return s;
}

/* 大厅：进行中 / 影子候选 / 收藏墙（数据源全部影子化，无项目名——ADR 0001） */
function renderDunHall(){
  const v=$('dunHallView');
  const duns=STATE.dungeons||[];
  const act=duns.find(x=>x.status==='active');
  const abandoned=duns.filter(x=>x.status==='abandoned');
  const pool=(STATE.readings||[]).filter(r=>r.project);
  const ready=pool.filter(r=>r.brief&&r.req);
  const unhatched=pool.filter(r=>!r.brief);
  const used=new Set(duns.map(x=>x.readingId));
  const trophies=STATE.trophies||[];

  let h='<div style="max-width:880px;margin:0 auto;padding:8px 28px 130px">';
  if(act){
    const tot=(act.stages||[]).length;
    h+='<div class="grouplabel">进行中</div><div class="active-card">'
      +'<div class="ac-top"><span class="ac-tag">⚔ 进行中</span><span class="ac-title">？？？？？</span><span style="margin-left:auto" class="chip pink">'+act.cur+'/'+tot+' 关</span></div>'
      +'<div class="pip-track">'+(act.stages||[]).map((sg,i)=>'<div class="pip '+(i<act.cur?'done':(i===act.cur?'cur':''))+'"></div>').join('')+'</div>'
      +'<div class="ac-actions"><button class="btn primary" onclick="openDungeon(\''+act.id+'\')">继续打副本</button><button class="btn ghost" onclick="abandonDungeon(\''+act.id+'\')">收起</button></div></div>';
  }
  h+='<div class="grouplabel">'+(act?'更多影子（打完当前的再开）':'可开启的影子副本 —— 只告诉你需要什么，不告诉你是什么')+'</div>';
  const free=ready.filter(r=>!used.has(r.id)&&!r.done);
  if(free.length){
    free.forEach(r=>{
      h+='<div class="shadow-card"><div class="sh-body">'
        +'<div class="sh-title"><span>⚔ 影子副本 · ？？？？？</span>'+tierHTML(r.tier||0)+starsHTML(r.stars||2)+'</div>'
        +reqHTML(r.req)
        +'<div class="sh-sub">'+((r.req||[]).some(x=>x.miss)?'开门后先由助教补上缺的点，再进关':'所需知识点你全部已收尾 · 通关揭晓它是什么')+'</div></div>'
        +'<div class="sh-actions"><button class="sh-go" onclick="startDungeon(\''+r.id+'\',this)" '+(act?'disabled':'')+'>开门</button>'
        +'<span class="sh-hint">'+(act?'先打完当前的影子':'通关揭晓它是什么')+'</span></div></div>';
    });
  } else if(!unhatched.length&&!act){
    h+='<div class="silo-slot" style="min-height:76px"><small>没有待开的影子——去收尾几个新知识点，孵化器会带来新的</small></div>';
  }
  if(unhatched.length){
    unhatched.forEach(r=>{
      h+='<div class="dh-item dim" style="list-style:none"><div><div class="dh-t" style="filter:blur(.4px)">⚔ ？？？（未孵化）</div><div class="dh-sub">助教先深入项目导读+定档，你确认感兴趣再开门</div></div><button class="btn ghost" onclick="hatchShadow(\''+r.id+'\',this)">孵化影子</button></div>';
    });
  }
  /* 收藏墙 */
  h+='<div class="grouplabel">收藏墙 —— 你亲手做出的东西</div><div class="wall-grid">';
  (trophies||[]).forEach(tr=>{
    h+='<div class="tcard" onclick="trophyDetail(\''+esc(tr.name)+'\')">'
      +'<div class="t-date">复现<br>'+esc(tr.short)+'<br>2026</div>'
      +'<div class="t-badge">🏆 已复现</div><div class="t-name">'+esc(tr.name)+'</div>'
      +'<div class="t-impact">'+esc(tr.repo)+'</div>'
      +'<div class="t-meta"><span>'+tr.stages+' 关</span><span>'+((tr.nodes||[]).length)+' 知识点</span></div></div>';
  });
  const siloN=Math.max(0,2-trophies.length);
  for(let i=0;i<siloN;i++)h+='<div class="silo-slot"><div class="silo"></div><span class="q">？</span><small>？？？（未遇见）</small></div>';
  h+='</div>';
  if(trophies.length){
    h+='<div style="margin-top:12px"><button class="btn ghost" onclick="exportResume(this)">📜 生成实践简历（Markdown，每条挂可点开的证据）</button></div>';
  }
  /* 已收起 */
  if(abandoned.length){
    h+='<div class="grouplabel">已收起</div>';
    abandoned.forEach(d=>{
      h+='<div class="dh-item dim"><div><div class="dh-t">⚔ ？？？？？</div><div class="dh-sub">已收起——影子与关卡都还在</div></div><button class="btn ghost" onclick="reopenDungeon(\''+d.id+'\')">重新开门</button></div>';
    });
  }
  h+='</div>';
  v.innerHTML=h;
}

/* ---------- 副本大厅（一级视图：进行中 / 可开启 / 已通关） ---------- */
async function reopenDungeon(id){
  try{
    await api('/api/dungeon/reopen',{id});
    await loadState();
    openDungeon(id);
  }catch(e){toast('失败：'+e.message)}
}
// 快捷键 1-3：今天 / 我的进度 / 设置
const VIEW_ORDER=['learn','progress','set'];
document.addEventListener('keydown',e=>{
  if(e.target&&(e.target.tagName==='INPUT'||e.target.tagName==='TEXTAREA'))return;
  const n=parseInt(e.key);
  if(n>=1&&n<=VIEW_ORDER.length)switchTab(VIEW_ORDER[n-1]);
});
loadState();
/* ---------- 桌面端专项：Ctrl+K 命令面板 / Esc 弹层 / 页面与窗口记忆 / 今日提醒 ---------- */

/* 命令面板 */
let PAL_ITEMS=[],PAL_HOT=0;
function palSource(){
  const items=[
    {ico:'◈',t:'今天 · 继续学习',sub:'视图',run:()=>switchTab('learn')},
    {ico:'◎',t:'我的进度',sub:'视图',run:()=>switchTab('progress')},
    {ico:'⚔',t:'实战副本',sub:'进度',run:()=>{PROG_TAB='dungeon';switchTab('progress')}},
    {ico:'✓',t:'学会的',sub:'进度',run:()=>{PROG_TAB='sediment';switchTab('progress')}},
    {ico:'🕸',t:'知识地图',sub:'进度',run:()=>{PROG_TAB='graph';switchTab('progress')}},
    {ico:'⚙',t:'设置',sub:'视图',run:()=>switchTab('set')},
  ];
  (STATE.curriculum||{}).lessons?.forEach(l=>{
    items.push({ico:'▸',t:l.title,sub:l.status==='done'?'已收尾':'课程',run:()=>openLesson(l.id)});
  });
  return items;
}
function openPalette(){
  let ov=document.getElementById('palette');
  if(ov){closePalette();return}
  ov=document.createElement('div');ov.className='palette';ov.id='palette';
  ov.innerHTML='<div class="pal-box"><input id="palInput" placeholder="跳到课程、视图或动作…">'
    +'<div class="pal-list" id="palList"></div>'
    +'<div class="pal-foot"><span><kbd>↑</kbd><kbd>↓</kbd> 选择</span><span><kbd>Enter</kbd> 执行</span><span><kbd>Esc</kbd> 关闭</span></div></div>';
  document.body.appendChild(ov);
  ov.addEventListener('click',e=>{if(e.target===ov)closePalette()});
  const inp=ov.querySelector('#palInput');
  PAL_ITEMS=palSource();PAL_HOT=0;
  const draw=()=>{
    const q=inp.value.trim().toLowerCase();
    const list=PAL_ITEMS.filter(x=>!q||x.t.toLowerCase().includes(q));
    ov.querySelector('#palList').innerHTML=list.length?list.map((x,i)=>
      '<div class="pal-item'+(i===PAL_HOT?' hot':'')+'" data-i="'+PAL_ITEMS.indexOf(x)+'"><span class="pi-ico">'+x.ico+'</span>'+esc(x.t)+'<span class="pi-sub">'+x.sub+'</span></div>').join('')
      :'<div class="pal-empty">没有匹配项</div>';
    ov.querySelectorAll('.pal-item').forEach(el=>el.onclick=()=>{const it=PAL_ITEMS[+el.dataset.i];closePalette();it.run()});
  };
  inp.oninput=()=>{PAL_HOT=0;draw()};
  inp.onkeydown=e=>{
    const q=inp.value.trim().toLowerCase();
    const list=PAL_ITEMS.filter(x=>!q||x.t.toLowerCase().includes(q));
    if(e.key==='ArrowDown'){e.preventDefault();PAL_HOT=Math.min(PAL_HOT+1,list.length-1);draw()}
    else if(e.key==='ArrowUp'){e.preventDefault();PAL_HOT=Math.max(PAL_HOT-1,0);draw()}
    else if(e.key==='Enter'){e.preventDefault();if(list[PAL_HOT]){closePalette();list[PAL_HOT].run()}}
  };
  draw();
  setTimeout(()=>inp.focus(),30);
}
function closePalette(){const ov=document.getElementById('palette');if(ov)ov.remove()}
/* Esc 关弹层（面板/纪念卡详情/揭晓——揭晓不关，是仪式） */
document.addEventListener('keydown',e=>{
  if(e.key==='Escape'){
    closePalette();
    const td=document.querySelector('.tdetail');if(td)td.remove();
  }else if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='k'){
    e.preventDefault();openPalette();
  }
});

/* 页面记忆：刷新/重开回到上次所在视图与进度 tab */
(function(){
  const savedView=localStorage.getItem('voyage.lastView');
  const savedTab=localStorage.getItem('voyage.lastProgTab');
  if(savedTab)PROG_TAB=savedTab;
  const _switchTab=switchTab;
  window.switchTab=function(v){
    localStorage.setItem('voyage.lastView',v);
    if(v==='progress')localStorage.setItem('voyage.lastProgTab',PROG_TAB);
    _switchTab(v);
  };
  const _switchProgTab=window.switchProgTab;
  if(_switchProgTab)window.switchProgTab=function(t){localStorage.setItem('voyage.lastProgTab',t);_switchProgTab(t)};
  if(savedView&&['learn','progress','set'].includes(savedView)){
    setTimeout(()=>{try{switchTab(savedView)}catch(e){}},400);
  }
})();

/* 窗口大小/位置记忆（App 窗口） */
(function(){
  try{
    const w=localStorage.getItem('voyage.win');
    if(w){
      const o=JSON.parse(w);
      if(o.w&&o.h){
        window.resizeTo(o.w,o.h);
        if(o.x!=null&&window.moveTo)try{window.moveTo(o.x,o.y)}catch(e){}
      }
    }
    let t=null;
    window.addEventListener('resize',()=>{
      clearTimeout(t);
      t=setTimeout(()=>{
        try{localStorage.setItem('voyage.win',JSON.stringify({w:window.outerWidth||window.innerWidth,h:window.outerHeight||window.innerHeight}))}catch(e){}
      },600);
    });
  }catch(e){}
})();

function appendRemindRow(){
  const sv=document.getElementById('setView');
  if(!sv||document.getElementById('remindRow'))return;
  const on=localStorage.getItem('voyage.remind')==='1';
  const theme=localStorage.getItem('voyage.theme')||'';
  const row=document.createElement('div');row.id='remindRow';row.className='set-card';
  row.style.maxWidth='min(920px,100%)';row.style.margin='0 auto 16px';
  row.innerHTML='<div class="grouplabel">桌面体验</div>'
    +'<label style="display:flex;align-items:center;gap:10px;font-size:13px;cursor:pointer"><input type="checkbox" '+(on?'checked':'')+' onchange="window.setDailyRemind&&setDailyRemind(this.checked)">今日步骤提醒（窗口在后台时通知「还差 N 步」，每天最多一次，可随时关）</label>'
    +'<div style="display:flex;align-items:center;gap:10px;font-size:13px;margin-top:8px">外观：<select onchange="setTheme(this.value)" style="font:inherit;padding:4px 8px;border-radius:7px;border:1px solid var(--line);background:var(--card);color:var(--ink)">'
    +'<option value=""'+(theme===''?' selected':'')+'>跟随系统</option><option value="light"'+(theme==='light'?' selected':'')+'>浅色</option><option value="dark"'+(theme==='dark'?' selected':'')+'>深色</option></select>'
    +'<label style="display:flex;align-items:center;gap:10px;font-size:13px;cursor:pointer;margin-top:8px"><input type="checkbox" '+(LAB_OPEN?'checked':'')+' onchange="setLab(this.checked)">显示全部功能（方向 / 实战副本 / 学会的 / 知识地图 / 设置）</label>'
    +'<label style="display:flex;align-items:center;gap:10px;font-size:13px;cursor:pointer;margin-top:8px"><input type="checkbox" '+(STATE.expElo?'checked':'')+' onchange="setExp(&#39;elo&#39;,this.checked)">实验：能力估计（Elo 难度带出题——默认关，出题按收尾顺序）</label>'
    +'<label style="display:flex;align-items:center;gap:10px;font-size:13px;cursor:pointer;margin-top:8px"><input type="checkbox" '+(STATE.expBandit?'checked':'')+' onchange="setExp(&#39;bandit&#39;,this.checked)">实验：题目选择（Thompson 采样——默认关，选题确定性）</label>'
    +'<span style="font-size:11px;color:var(--mut);display:block;margin-top:6px">快捷键：Ctrl+K 命令面板 · Esc 关弹层 · Shift+Enter 换行 · 1/2/3 切主视图</span></div>';
  sv.prepend(row);
}
async function setExp(k,on){
  try{
    const body={};body[k]=on;
    const r=await api('/api/config/exp',body);
    STATE.expElo=r.expElo;STATE.expBandit=r.expBandit;
    toast('实验开关已保存——默认关闭，打开才启用');
  }catch(e){toast('失败：'+e.message)}
}
function setLab(on){
  LAB_OPEN=!!on;
  try{localStorage.setItem('voyage.lab',on?'1':'0')}catch(e){}
  applyLab();
}
function setTheme(v){
  localStorage.setItem('voyage.theme',v);
  if(v)document.documentElement.setAttribute('data-theme',v);
  else document.documentElement.removeAttribute('data-theme');
}
try{const _t=localStorage.getItem('voyage.theme');if(_t)document.documentElement.setAttribute('data-theme',_t)}catch(e){}
/* 小穗评审验收演示钩子：?demo=verdict 拍判定出口 / ?demo=narrow 拍侧栏图标模式（真实函数真实渲染） */
try{
  const _dp=new URLSearchParams(location.search).get('demo');
  if(_dp==='verdict')setTimeout(()=>{renderVerdictActions('partial')},1400);
  if(_dp==='narrow')setTimeout(()=>{try{window.resizeTo(1100,880)}catch(e){}},900);
  if(_dp==='settings')setTimeout(()=>{try{switchTab('set')}catch(e){}},1600);
  if(_dp==='dark')setTimeout(()=>{try{setTheme('dark');switchTab('progress')}catch(e){}},1600);
}catch(e){}
/* 今日提醒（可关闭）：服务端今日计划未完成时，每 30 分钟最多提醒一次 */
(function(){
  let timer=null;
  window.setDailyRemind=function(on){
    localStorage.setItem('voyage.remind',on?'1':'0');
    if(timer){clearInterval(timer);timer=null}
    if(on){
      checkRemind();
      timer=setInterval(checkRemind,30*60*1000);
      toast('今日提醒已开启——还差步骤时会通知你');
    }else toast('今日提醒已关闭');
  };
  async function checkRemind(){
    if(!('Notification' in window)||Notification.permission!=='granted')return;
    try{const s0=STATE||{};if(s0.pausedUntil&&new Date(s0.pausedUntil)>new Date())return}catch(e){} // 暂停期不推
    if(document.visibilityState==='visible')return; /* 窗口在前台就不用打扰 */
    const last=localStorage.getItem('voyage.remindAt');
    const today=new Date().toDateString();
    if(last===today+':sent')return;
    try{
      const st=await api('/api/state');
      const plan=st.plan||{};
      const total=(plan.main||[]).length;
      const doneN=(st.done&&st.done.main?st.done.main:[]).length;
      const left=total-doneN;
      if(left>0){
        new Notification('前哨 · 今天还差 '+left+' 步', {body:'上次你停在「'+((st.today||{}).title||'今天的课')+'」——点开继续。'});
        localStorage.setItem('voyage.remindAt',today+':sent');
      }
    }catch(e){}
  }
})();


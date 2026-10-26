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
  return steps;
}
function renderTodaySteps(){
  const el=document.getElementById('planCard');if(!el)return;
  const steps=todaySteps();
  let h='<div class="pc" style="padding:14px 20px"><div class="grouplabel" style="margin:0 0 10px">今天三步</div>';
  steps.forEach((s,i)=>{
    const primary=s.cur;
    h+='<div style="display:flex;align-items:center;gap:12px;padding:7px 0;'+(i<steps.length-1?'border-bottom:1px dashed var(--line);':'')+'">'
      +'<span style="width:22px;height:22px;flex:none;border-radius:50%;display:flex;align-items:center;justify-content:center;font-size:11px;font-weight:700;'
      +(s.done?'background:var(--green);color:#fff':(primary?'background:var(--pink);color:#fff':'background:var(--chip);color:var(--mut)'))+'">'+(s.done?'✓':i+1)+'</span>'
      +'<span style="flex:1;min-width:0;font-size:13.5px;font-weight:'+(primary?'600':'500')+'">'+esc(s.k)+' · '+esc(s.title)+'</span>'
      +'<button class="'+(primary?'va primary':'va')+'" onclick="'+s.fn+'" style="border:1px solid var(--line);background:'+(primary?'var(--pink)':'var(--card)')+';color:'+(primary?'#fff':'var(--ink)')+';padding:6px 16px;font-size:12px;border-radius:8px;cursor:pointer;'+(primary?'border-color:var(--pink);font-weight:600;':'')+'">'+esc(s.act)+'</button>'
      +'</div>';
  });
  h+='<div style="margin-top:9px;font-size:10.5px;color:var(--mut)">做完打勾 · 当前三步会自动跟着你的进度走</div></div>';
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

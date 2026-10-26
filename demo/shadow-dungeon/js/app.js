/* ============================================================
   app.js — 路由 + 事件委托 + 启动
   视图：hall（影子大厅）/ dungeon（打关）/ wall（收藏墙+连锚）
   仪式：开门雾散 Effects.fogPlay / 揭晓 Effects.showReveal
   ============================================================ */
(function(){
  const $=(id)=>document.getElementById(id);
  const KICK={hall:'DUNGEON · 副本复现',dungeon:'DUNGEON · 副本复现',wall:'SEDIMENT · 收藏与连锚'};
  const TITLE={hall:'副本',dungeon:'副本',wall:'沉淀'};

  let toastT=null;
  window.toast=function(msg){
    const t=$('toast');t.textContent=msg;t.classList.add('show');
    clearTimeout(toastT);toastT=setTimeout(()=>t.classList.remove('show'),2600);
  };

  function render(){
    const st=Store.state;
    const v=st.view;
    ['hall','dungeon','wall'].forEach(id=>$('view-'+id).hidden=(id!==v));
    const el=$('view-'+v);
    el.classList.remove('v-enter');void el.offsetWidth;el.classList.add('v-enter');
    $('kicker').textContent=KICK[v]||'';
    $('h1').textContent=TITLE[v]||'';
    document.querySelectorAll('.sbtn[data-nav]').forEach(b=>b.classList.toggle('on',b.dataset.nav===v));
    if(v==='hall')Views.hall(el,st);
    else if(v==='dungeon')Views.dungeon(el,st);
    else if(v==='wall')Views.wall(el,st);
    $('prog').innerHTML=st.trophies.length?`已复现 <b>${st.trophies.length}</b> 个作品`:'尚未复现作品';
  }

  function go(v){const st=Store.state;st.view=v;Store.save();render();}
  window.__go=go; /* views.js 通关后跳收藏墙 */

  /* ---------- 事件委托 ---------- */
  document.addEventListener('click',e=>{
    const st=Store.state;
    const nav=e.target.closest('.sbtn[data-nav]');
    if(nav){
      const v=nav.dataset.nav;
      if(v==='hall'||v==='wall'){go(v);}
      else if(v==='dungeon'){st.active?go('dungeon'):toast('演示只开放「副本」与「沉淀」两个视图');}
      else toast('演示只开放「副本」与「沉淀」两个视图');
      return;
    }
    if(e.target.closest('[data-go]')){go(e.target.closest('[data-go]').dataset.go);return;}
    if(e.target.closest('[data-reset]')){Store.reset();go('hall');toast('演示旅程已重置');return;}

    /* 大厅：开门（雾散 → 进入副本） */
    const openBtn=e.target.closest('[data-open]');
    if(openBtn&&!openBtn.disabled){
      if(st.active){toast('一次只打一个影子');return;}
      Effects.fogPlay(()=>{
        Store.openDoor(openBtn.dataset.open);
        go('dungeon');
      });
      return;
    }
    if(e.target.closest('[data-abandon]')){
      const a=st.active;
      st.shadows.push(a.sh); /* 影子回到候选池——项目与关卡都留着 */
      st.active=null;Store.save();go('hall');
      toast('副本已收起——影子回到候选池，随时回来');
      return;
    }
    /* 副本：交作业 / 求助 */
    if(e.target.closest('#pass-btn')){Views.passNext();return;}
    if(e.target.closest('#stuck-btn')){
      Views.stuckHint();toast('提示已给——一次只往前一小步');return;
    }
    /* 收藏卡详情 */
    const tc=e.target.closest('.tcard[data-tro]');
    if(tc){Views.tdetail(tc.dataset.tro);return;}
  });

  /* ---------- 启动 ---------- */
  render();
})();

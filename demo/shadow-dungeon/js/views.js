/* ============================================================
   views.js — 视图渲染：大厅 / 副本进行中 / 收藏墙+连锚
   ============================================================ */
(function(){
  const $=(id)=>document.getElementById(id);
  const esc=s=>String(s).replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));

  function starsHTML(n){
    let h='';
    for(let i=1;i<=3;i++)h+=i<=n?'★':'<span class="off">★</span>';
    return `<span class="stars">${h}</span>`;
  }

  /* ---------- ① 大厅 ---------- */
  function hall(root,st){
    let h='';
    /* 进行中 */
    if(st.active){
      const a=st.active;
      h+=`<div class="grouplabel">进行中</div>
      <div class="active-card">
        <div class="ac-top"><span class="ac-tag">⚔ 进行中</span><span class="ac-title">？？？？？</span>
          <span style="margin-left:auto" class="chip pink">${a.stage}/${a.sh.stages.length} 关</span></div>
        <div class="stage-track">${a.sh.stages.map((sg,i)=>`
          <div class="st-pip ${i<a.stage?'done':(i===a.stage?'cur':'')}"><span class="no">${i<a.stage?'✓':i+1}</span><span>${esc(sg.t)}</span></div>`).join('')}
        </div>
        <div class="ac-actions"><button class="btn primary" data-go="dungeon">继续打副本</button>
          <button class="btn ghost" data-abandon>收起</button></div>
      </div>`;
    }
    /* 影子候选 */
    h+=`<div class="grouplabel">可开启的影子副本 —— 只告诉你需要什么，不告诉你是什么</div>`;
    if(st.shadows.length){
      st.shadows.forEach(sh=>{
        const chips=sh.req.map(r=>`<span class="chip ${r.miss?'miss':''}">${esc(r.n)}${r.miss?' · 未掌握':''}</span>`).join('');
        const sub=sh.tier===0?'所需知识点你全部已收尾 · 通关揭晓它是什么'
          :`开门后先由助教补上缺的点，再进关`;
        h+=`<div class="shadow-card">
          <div class="sh-body">
            <div class="sh-title"><span class="q">⚔ 影子副本 · ？？？？？</span>
              <span class="tier t${sh.tier}">${sh.tiers[sh.tier]}</span>${starsHTML(sh.stars)}</div>
            <div class="req">${chips}</div>
            <div class="sh-sub">${sub}</div>
          </div>
          <div class="sh-actions">
            <button class="sh-go" data-open="${sh.id}" ${st.active?'disabled':''}>开门</button>
            <span class="sh-hint">${st.active?'先打完当前的影子':''}</span>
          </div>
        </div>`;
      });
    }else{
      h+=`<div class="silo-slot" style="min-height:76px"><small>没有待开的影子——去收尾几个新知识点，孵化器会带来新的</small></div>`;
    }
    /* 剪影行（未遇见） */
    if(st.silos>0&&!st.trophies.length){
      h+=`<div class="grouplabel">收藏墙</div><div class="silo-row">`;
      for(let i=0;i<st.silos;i++)h+=`<div class="silo-slot"><div class="silo"></div><span class="q">？</span><small>？？？（未遇见）</small></div>`;
      h+=`</div>`;
    }
    /* 已有收藏 → 收藏墙预览 */
    if(st.trophies.length){
      h+=`<div class="grouplabel">收藏墙 —— 你做出的东西</div><div class="wall-grid">`;
      st.trophies.forEach(tr=>{
        h+=`<div class="tcard" data-tro="${esc(tr.name)}">
          <div class="t-date">复现<br>${esc(tr.short)}<br>2026</div>
          <div class="t-badge">🏆 已复现</div><div class="t-name">${esc(tr.name)}</div>
          <div class="t-impact">${esc(tr.repo)}</div>
          <div class="t-meta"><span>${tr.stages} 关</span><span>${tr.nodes.length} 知识点</span></div>
        </div>`;
      });
      for(let i=0;i<st.silos;i++)h+=`<div class="silo-slot"><div class="silo"></div><span class="q">？</span><small>？？？（未遇见）</small></div>`;
      h+=`</div>
      <div style="margin-top:14px"><button class="btn ghost" data-go="wall">在沉淀视角查看连锚图谱 →</button></div>`;
    }
    root.innerHTML=h;
  }

  /* ---------- ② 副本进行中 ---------- */
  function dungeon(root,st){
    const a=st.active;
    let h=`<div class="dn-head"><div class="dn-q">？？？？？</div>
      <div class="dn-sub">影子已开门 · 是什么，打完才知道</div>
      <div class="pip-track">${a.sh.stages.map((_,i)=>`<div class="pip ${i<a.stage?'done':(i===a.stage?'cur':'')}" id="pip-${i}"></div>`).join('')}</div>
    </div><div class="flow" id="flow"></div>
    <div class="dn-actions"><button class="btn primary" id="pass-btn">交作业 · 本关完成</button>
      <button class="dn-stuck" id="stuck-btn">卡住了，向助教要一步提示</button>
      <button class="btn ghost" data-go="hall">暂离</button></div>`;
    root.innerHTML=h;
    const flow=$('flow');
    const put=(who,h4,p,ok)=>{const d=document.createElement('div');d.className='bubble'+(ok?' ok':'');
      d.innerHTML=`<div class="who">${who}</div><h4>${h4}</h4><p>${p}</p>`;flow.appendChild(d);
      d.scrollIntoView({behavior:'smooth',block:'end'});return d;};
    put('⚔ 副本开启','影子已就位','它是什么？打完才知道。第一关很轻——');
    put('助教',`第 1 关 · ${esc(a.sh.stages[0].t)}`,esc(a.sh.stages[0].b));
    window.__dn={put,flow};
  }
  function passNext(){
    const st=Store.state,a=st.active;if(!a)return;
    const {put}=(window.__dn||{put:()=>{}});
    const idx=a.stage;
    const pips=$('pip-'+idx);if(pips)pips.classList.remove('cur'),pips.classList.add('done');
    put('',`第 ${idx+1} 关 · 通过`,esc(a.sh.stages[idx].t)+' —— 已收入履历 ✓',true);
    const r=Store.passStage();
    const btn=$('pass-btn');
    if(r==='next'){
      setTimeout(()=>{
        put('助教',`第 ${a.stage+1} 关 · ${esc(a.sh.stages[a.stage].t)}`,esc(a.sh.stages[a.stage].b));
        const np=$('pip-'+a.stage);if(np)np.classList.add('cur');
        btn.disabled=false;
        if(a.stage===a.sh.stages.length-1)btn.textContent='交作业 · 最终关';
      },340);
      btn.disabled=true;
    }else if(r==='cleared'){
      btn.hidden=true;$('stuck-btn').hidden=true;
      const tr=Store.clear();
      setTimeout(()=>Effects.showReveal(tr,()=>window.__go&&window.__go('wall')),820);
    }
  }
  function stuckHint(){
    const st=Store.state;if(!st.active)return;
    const hint=Store.stuck();
    const d=document.createElement('div');d.className='bubble';
    d.innerHTML=`<div class="who">助教 · 一步提示（不代写）</div><h4>回到能跑的那一步</h4><p>${esc(hint)}</p>`;
    window.__dn.flow.appendChild(d);
    d.scrollIntoView({behavior:'smooth',block:'end'});
  }

  /* ---------- ④ 收藏墙 + 连锚（沉淀视角） ---------- */
  function wall(root,st){
    let h=`<div class="grouplabel">收藏墙 —— 你亲手做出的东西</div><div class="wall-grid">`;
    if(st.trophies.length){
      st.trophies.forEach(tr=>{
        h+=`<div class="tcard" data-tro="${esc(tr.name)}">
          <div class="t-date">复现<br>${esc(tr.short)}<br>2026</div>
          <div class="t-badge">🏆 已复现</div><div class="t-name">${esc(tr.name)}</div>
          <div class="t-impact">${esc(tr.repo)}</div>
          <div class="t-meta"><span>${tr.stages} 关</span><span>${tr.nodes.length} 知识点</span></div>
        </div>`;
      });
    }else{
      h+=`<div class="silo-slot" style="min-height:150px"><div class="silo"></div><span class="q">？</span><small>还没有战利品——去开一个影子副本</small></div>`;
    }
    for(let i=0;i<st.silos;i++)h+=`<div class="silo-slot"><div class="silo"></div><span class="q">？</span><small>？？？（未遇见）</small></div>`;
    h+=`</div>`;

    h+=`<div class="grouplabel">连锚 —— 沉淀页里，这些知识点共同连向这张卡</div><div class="anchor-wrap">`;
    h+=anchorSVG(st.trophies[0]);
    h+=`</div>
    <div style="margin-top:16px"><button class="btn ghost" data-go="hall">← 回大厅</button>
      <button class="btn ghost" data-reset>重置演示旅程</button></div>`;
    root.innerHTML=h;
  }
  /* 连锚图：知识点节点 ← earned 虚线 → 卡形节点 */
  function anchorSVG(tr){
    if(!tr){
      return `<svg class="anchor-svg" viewBox="0 0 660 200" role="img" aria-label="连锚图谱空态">
        <rect class="an-tro" x="450" y="60" width="140" height="84" rx="14" transform="rotate(45 520 102)"/>
        <text class="an-sub" x="520" y="106" text-anchor="middle">还未有卡片</text>
        <text class="an-note" x="300" y="106" text-anchor="middle">通关一个影子副本后，知识点会在这里连成卡</text></svg>`;
    }
    const nodes=tr.nodes;
    const W=660,H=210,midY=100,cx=500,cy=midY;
    /* 显式阶梯布局（1-4 节点场景固定，避免重叠） */
    const LAYOUTS={
      1:[[110,100]],
      2:[[110,62],[150,140]],
      3:[[110,44],[160,105],[110,166]],
      4:[[95,44],[210,74],[95,150],[210,166]],
    };
    const layout=LAYOUTS[nodes.length]||LAYOUTS[3];
    const pos=nodes.map((n,i)=>({n,x:layout[i][0],y:layout[i][1]}));
    let edges='',nodesS='';
    pos.forEach((p,i)=>{
      const y2=cy+(i-(pos.length-1)/2)*16;
      edges+=`<line class="an-earned" data-idx="${i}" x1="${p.x+80}" y1="${p.y}" x2="${cx-92}" y2="${y2}"/>`;
    });
    /* 知识点之间淡淡的既有连线（语境边，连前两个） */
    if(pos.length>1){
      edges+=`<path class="an-ctx" d="M ${pos[0].x+62} ${pos[0].y+14} C ${pos[0].x+120} ${pos[0].y+26}, ${pos[1].x-60} ${pos[1].y-26}, ${pos[1].x+18} ${pos[1].y-20}"/>`;
    }
    pos.forEach(p=>{
      nodesS+=`<g class="an-g" data-node="${esc(p.n)}">
        <rect class="an-node" x="${p.x-8}" y="${p.y-17}" width="78" height="32" rx="9"/>
        <text class="an-label" x="${p.x+31}" y="${p.y+4}" text-anchor="middle">${esc(p.n)}</text></g>`;
    });
    return `<svg class="anchor-svg" viewBox="0 0 ${W} ${H}" role="img" aria-label="知识点连锚图谱：${esc(nodes.join('、'))}连向${esc(tr.name)}">
      ${edges}
      ${nodesS}
      <g class="an-g" data-tro>
        <rect class="an-tro" x="${cx-84}" y="${cy-46}" width="168" height="92" rx="14" transform="rotate(45 ${cx} ${cy})"/>
        <text class="an-label" x="${cx}" y="${cy-4}" text-anchor="middle" font-weight="700">${esc(tr.name)}</text>
        <text class="an-sub" x="${cx}" y="${cy+15}" text-anchor="middle">🏆 ${esc(tr.short)}</text>
      </g>
      <text class="an-note" x="${(cx+60)/2}" y="${cy-58}" text-anchor="middle">共同铸成 →</text>
    </svg>`;
  }

  /* ---------- 卡详情 ---------- */
  function tdetail(name){
    const st=Store.state;
    const tr=st.trophies.find(x=>x.name===name);if(!tr)return;
    const ov=document.createElement('div');ov.className='tdetail';ov.id='tdetail';
    ov.innerHTML=`<div class="td-card">
      <button class="td-close" data-close>✕</button>
      <div class="t-badge">🏆 复现成功 · 纪念卡</div>
      <div class="td-name">${esc(tr.name)}</div>
      <div class="td-repo">${esc(tr.repo)}</div>
      <div class="td-impact">${esc(tr.impact)}</div>
      <div class="td-sec"><div class="grouplabel">用到的知识点（连锚已写入沉淀）</div>
        ${tr.nodes.map(n=>`<span class="td-node" data-node="${esc(n)}">${esc(n)}</span>`).join('')}</div>
      <div class="td-foot"><span class="td-cleared">复现于 ${esc(tr.date)}</span>
        <button class="btn ghost" data-close>关闭</button></div>
    </div>`;
    document.body.appendChild(ov);
    ov.addEventListener('click',e=>{
      if(e.target===ov||e.target.closest('[data-close]'))ov.remove();
      const nd=e.target.closest('[data-node]');
      if(nd)toast(`在真实产品里会跳到关联图并聚焦「${nd.dataset.node}」`);
    });
  }

  window.Views={hall,dungeon,passNext,stuckHint,wall,tdetail};
})();

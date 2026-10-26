/* ============================================================
   effects.js — 仪式编排：开门雾散 / 一锤揭晓（时序 + 粉尘微粒）
   ============================================================ */
(function(){
  /* ---- 开门雾散 ---- */
  function fogPlay(then){
    let f=document.querySelector('.fog');
    if(!f){f=document.createElement('div');f.className='fog';f.innerHTML='<div class="fog-q">？？？？？</div>';document.body.appendChild(f);}
    f.classList.remove('play');void f.offsetWidth;
    f.classList.add('play');
    setTimeout(then,470); /* 雾最浓时切视图 */
    setTimeout(()=>f.classList.remove('play'),1000);
  }

  /* ---- 一锤揭晓：show → revealing → 粉尘 → 完成 ---- */
  function showReveal(tr,done){
    let ov=document.getElementById('reveal');
    if(!ov){
      ov=document.createElement('div');ov.className='reveal';ov.id='reveal';
      ov.innerHTML=`
        <div class="rv-qs"><span>？</span><span>？</span><span>？</span><span>？</span><span>？</span></div>
        <div class="rv-card" id="rv-card"></div>
        <div class="rv-text" id="rv-text"></div>
        <button class="rv-go" id="rv-go">收入收藏墙 →</button>`;
      document.body.appendChild(ov);
      /* 粉尘微粒：沿卡片上缘随机分布 */
      const card=ov.querySelector('.rv-card');
      for(let i=0;i<16;i++){
        const m=document.createElement('i');m.className='rv-mote';
        m.style.left=(12+Math.random()*76)+'%';
        m.style.top=(30+Math.random()*45)+'%';
        m.style.animationDelay=(0.7+Math.random()*1.1)+'s';
        m.style.width=m.style.height=(2+Math.random()*3)+'px';
        card.appendChild(m);
      }
    }
    const card=ov.querySelector('#rv-card');
    card.innerHTML=`
      <div class="t-date">复现<br>${tr.short}<br>2026</div>
      <div class="t-badge">🏆 复现成功 · 影子揭晓</div>
      <div class="t-name">${tr.name}</div>
      <div class="t-impact">${tr.impact}</div>
      <div class="t-meta">${tr.nodes.map(n=>`<span>${n}</span>`).join('')}</div>`;
    ov.querySelector('#rv-text').innerHTML=
      `<b>恭喜——你做出了「${tr.name}」。</b><small>回头看：它只用了你已经学会的东西。</small>`;
    /* 时序：show（暗场）→ 2 帧后 revealing（碎片+flip+shimmer） */
    ov.classList.add('show');ov.classList.remove('revealing');
    requestAnimationFrame(()=>requestAnimationFrame(()=>ov.classList.add('revealing')));
    ov.querySelector('#rv-go').onclick=()=>{
      ov.classList.remove('show','revealing');
      done&&done();
    };
  }

  window.Effects={fogPlay,showReveal};
})();

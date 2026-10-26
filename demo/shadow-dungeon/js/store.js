/* ============================================================
   store.js — 模拟数据 + 状态管理（localStorage 持久化，可重置）
   数据模型与设计文档《副本系统 v2 悬念化设计》对齐：
   Shadow { tier, stars, req[{n, miss}] } / active / trophies[]
   ============================================================ */
(function(){
  const KEY='voyage-shadow-dungeon-demo-v1';

  const TODAY=new Date();
  const fmtDate=d=>`${d.getFullYear()}·${String(d.getMonth()+1).padStart(2,'0')}·${String(d.getDate()).padStart(2,'0')}`;
  const fmtShort=d=>`${String(d.getMonth()+1).padStart(2,'0')}·${String(d.getDate()).padStart(2,'0')}`;

  /* 模拟学习者的已收尾知识点（呼应 Python+Go 交付实战） */
  const LEARNED=['变量与类型','字符串切片','条件门控','循环与遍历','函数封装','列表操作','错误处理','字典操作','文件读写'];

  /* 影子候选（孵化器产出；项目身份不在 demo 数据里——揭晓才知道） */
  const SHADOWS=[
    {id:'sh-1',tier:0,tiers:['巩固','拉伸','跃迁'],stars:2,
     req:[{n:'函数封装'},{n:'条件门控'},{n:'字符串切片'}],
     stages:[
       {t:'先量出压缩前的份量',b:'写一个函数：对一段 JSON 日志算出字符数、粗估 token 数。先能测量，后面才有优化的底气。'},
       {t:'只留头尾的瘦身刀',b:'实现 compress()：不值得压缩就原样返回（门控）；压缩时只留前 N 行和最后一行。想想哪些行绝对不能丢。'},
       {t:'压掉的原文找得回来',b:'把原文按哈希存档，压缩结果留个标记，随时能查回原文。最后跑一遍你自己的验证。'},
     ],
     reveal:{
       name:'Token 压缩层',repo:'headroomlabs-ai/headroom · 76k★',
       impact:'让长上下文 AI 应用便宜 10 倍的开源底座。你从零写出了它的核心：测量、压缩门控、不可丢行、原文可追溯。',
       nodes:['函数封装','条件门控','字符串切片'],
     }},
    {id:'sh-2',tier:1,tiers:['巩固','拉伸','跃迁'],stars:3,
     req:[{n:'字典操作'},{n:'文件读写'},{n:'归并思想',miss:true}],
     stages:[
       {t:'分而治之的第一刀',b:'把无序列表切成两半，各自排好，再归并——先写出归并两个有序列表的函数。'},
       {t:'归并的骨架',b:'递归拆分到底再逐层合并。跑三个测试：空列表、单元素、已有序。'},
     ],
     reveal:{
       name:'外部排序器',repo:' sort-merge 变体 · 教科书经典',
       impact:'数据库 ORDER BY 与 MapReduce 的心脏。每一次海量数据排序的背后，都是你今天写的这套归并骨架。',
       nodes:['字典操作','文件读写','归并思想'],
     }},
    {id:'sh-3',tier:1,tiers:['巩固','拉伸','跃迁'],stars:3,
     req:[{n:'类与对象'},{n:'哈希与校验',miss:true},{n:'文件读写'}],
     stages:[
       {t:'先立契约',b:'定义快照对象：内容、时间戳、哈希指纹。写 set 与 get。'},
       {t:'不许丢的字节',b:'存档前先算哈希，读档时校验——数据损坏要能被发现。'},
     ],
     reveal:{
       name:'内容寻址存储',repo:'git 对象库的最小心智模型',
       impact:'Git 与 IPFS 的共同地基：内容即地址。你今天实现的，是版本控制五十年演进里最核心的那一步。',
       nodes:['类与对象','哈希与校验','文件读写'],
     }},
  ];
  const SILOS=2; // 剪影槽：世界上还有多少个你能做出的东西

  function fresh(){
    return {learned:LEARNED,shadows:SHADOWS,silos:SILOS,active:null,trophies:[],view:'hall',done:{}};
  }
  function load(){
    try{
      const raw=localStorage.getItem(KEY);
      if(!raw)return fresh();
      const s=JSON.parse(raw);
      if(!s||!Array.isArray(s.shadows))return fresh();
      return s;
    }catch(e){return fresh()}
  }
  function save(){try{localStorage.setItem(KEY,JSON.stringify(state))}catch(e){}}
  function reset(){state=fresh();save();}

  let state=load();

  /* 展示用日期 */
  const dates={today:fmtDate(TODAY),short:fmtShort(TODAY)};

  window.Store={get state(){return state},save,reset,dates,
    openDoor(id){
      const sh=state.shadows.find(x=>x.id===id);
      if(!sh||state.active)return null;
      state.active={id:sh.id,stage:0,flow:[],sh};
      state.shadows=state.shadows.filter(x=>x.id!==id);
      save();return state.active;
    },
    passStage(){
      const a=state.active;if(!a)return false;
      a.flow.push({stage:a.stage,ok:true});
      a.stage++;
      if(a.stage>=a.sh.stages.length){ return 'cleared'; }
      save();return 'next';
    },
    clear(){
      const a=state.active;const sh=a.sh;
      const tr={name:sh.reveal.name,repo:sh.reveal.repo,impact:sh.reveal.impact,
        nodes:sh.reveal.nodes,stages:sh.stages.length,date:dates.today,short:dates.short};
      state.trophies.unshift(tr);
      state.active=null;state.done[sh.id]=true;
      save();return tr;
    },
    stuck(){
      const a=state.active;if(!a)return null;
      const st=a.sh.stages[a.stage];
      return st.hint||'回到你已经写过的那一步，从能跑的版本再往前走一小段。一次只改一件事。';
    },
  };
})();

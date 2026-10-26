# -*- coding: utf-8 -*-
# opportunities.json 种子（P1 机会页：手工维护，不抓取）
import json, os
seed = [
  {"type":"issue","title":"good first issue · Web 界面","link":"https://github.com/headroomlabs-ai/headroom/issues","note":"headroom 的 open issues——挑一个你能用已学知识点啃的，助教陪你做"},
  {"type":"issue","title":"scikit-learn 首页 issues（good first issue 筛选）","link":"https://github.com/scikit-learn/scikit-learn/contribute","note":"给真实项目提第一个 PR 的标准入口"},
  {"type":"intern","title":"Go 后端实习（BOSS 直聘搜索：Gin 实习）","link":"https://www.zhipin.com/web/geek/job?query=Gin&city=100010000","note":"学完「Go 写 HTTP 服务」就能看懂的岗位画像"},
  {"type":"gig","title":"Fiverr：Python automation 小单","link":"https://www.fiverr.com/search/gigs?query=python%20automation","note":"看看真实买家在为什么付钱——你的练习就是这些单子的微缩版"}
]
out = os.path.join(os.path.dirname(__file__),'..','tools','opportunities.json')
json.dump(seed, open(out,'w',encoding='utf-8'), ensure_ascii=False, indent=1)
print('seeded', out)

# Ozon 跨境卖家要点（2026-10-28 检索）

> 三块内容：① Seller API 基址与鉴权现状；② 中国卖家常见「API access disabled code 7」的原因与官方解法；③ 2026 秋冬俄电商品类需求公开数据。每条标来源与日期；弱源如实标弱。纯检索整理，未改代码。

---

## ① Seller API：基址与鉴权

**基址与双 header（未变，仍是主流方式）**
- 所有 Seller API 方法相对基址 `https://api-seller.ozon.ru`，例如 `POST /v3/finance/transaction/totals`（Host: api-seller.ozon.ru）。
- 每个请求必须带两个 header：`Client-Id`（卖家数字 ID）+ `Api-Key`（密钥）。
- 密钥生成：卖家后台 **设置 → API 集成（API интеграции）→ Seller API**；Client-Id 预生成，API Key 点「生成」后**只显示一次**。
- 来源：Ozon 官方 getting-started `https://seller-edu.ozon.ru/api-ozon/how-to-api`（可在文档控制台直接填 Client-Id/Api-Key 试调）；官方文档 `https://docs.ozon.ru/api/seller/`（Auth 标签页）；社区镜像 `https://github.com/etozhearut/ozon-seller-api`（检索日 2026-10-28）。

**OAuth 应用模式：已存在**
- Ozon 为「**私人应用**（частные приложения）」提供了 OAuth 客户端/OAuth token 通道，覆盖 **Seller API 与 Performance API**（比静态 key 更细的 scope 与授权流）——官方知识库页：`https://seller-edu.ozon.ru/api-ozon/rabota-s-chastnymi-prilojeniyami`（⚠ 该站是 SPA 且有反爬重定向，2026-10-28 直抓失败；上述要点来自检索摘录，端点细节以该页为准）。
- token 端点 `POST /oauth/token` 的存在由开发者社区帖佐证：`https://dev.ozon.ru/community/2117-Oshibka-pri-poluchenii-tokena-cherez-POST-oauth-token`（社区，检索日 2026-10-28）。
- 结论：**2026 年现状=双轨**——自用脚本/ERP 仍普遍用静态 `Client-Id + Api-Key`；对外发布的第三方应用走 OAuth（app 模式）。官方错误码表为 JS 渲染，纯静态抓取拿不到（见②）。

**配套资源**：Performance API（广告）文档 `https://docs.ozon.ru/api/performance/`；Seller API 变更公告 Telegram 频道 `https://t.me/OzonSellerAPI`（例：2025-04-28 完全移除过时参数，`https://dev.ozon.ru/news/513`）；开发者门户/社区 `https://dev.ozon.ru`（beta 方法、FAQ；应用问题可发 **dev@ozon.ru**）。

---

## ② 「API access disabled code 7」——中国卖家常见原因与官方解法

**判读（置信度中，依据如下）**：Ozon API 网关兼容 gRPC 错误模型，**code 7 = PERMISSION_DENIED**（gRPC 公开标准映射，非 Ozon 私有编号）。官方 Seller API 文档的错误码章节是 JS 渲染、静态抓取不可得（2026-10-28 实测），以下归因来自 gRPC 标准 + Ozon 开发者社区/中文服务商共识，故标注为「常见原因」而非官方定义。

**中国卖家常见原因（按命中率排序）**：
1. **密钥失效/被重置/权限不足**：老 Api-Key 被重置或换号后 Client-Id 不匹配——后台【设置 → Seller API】重新生成（老密钥显示一次，易失配）。来源：芒果店长授权教程 `https://help.mangoerp.com/article/1656480801894`（检索日 2026-10-28）。
2. **账户风控/封禁连带 API 禁用**：后台通知栏出现红色 «Аккаунт заблокирован»（账户被封）时 API 一并停用——先解封号再谈 API。来源：amz123 封店申诉帖 `https://m.amz123.com/ask/isN1Y1uC`（2025-06-02）。
3. **地域/IP 风控**：中国大陆出口 IP 触发网关拒绝或连接抖动（社区常见 443/cURL error 28 帖）——换固定干净出口（俄罗斯/-neutral 代理）重试。来源：Ozon dev 社区 `https://dev.ozon.ru/community/1683-Inogda-lovliu-443-pri-otpravke-zaprosov-no-dalshe-zaprosy-idut`（检索日 2026-10-28，经验共识）。
4. **方法被下线/版本淘汰**：调用已停用的旧版方法（如 `/v1/supply-order/*`、2025-04-28 移除的过时参数）也会以权限类错误外显——对 Telegram 公告频道与 `dev.ozon.ru/news` 核对方法版本。来源：`https://t.me/OzonSellerAPI`、`https://dev.ozon.ru/news/513`（2025-04-28）。

**官方解法路径**：①后台重生成 Client-Id/Api-Key 并同步 ERP；②核对 key 的角色/权限与所调方法匹配（dev.ozon.ru 的角色模型）；③仍被拒→邮件 **dev@ozon.ru**，附 Client-Id、完整响应体（code+message）、请求时间与出口 IP，请求确认密钥与接口权限状态。来源：Ozon 官方 FAQ `https://dev.ozon.ru/start/442-FAQ-Magazin-prilozhenii-i-Lichnyi-kabinet-razrabotchika`（检索日 2026-10-28）。

---

## ③ 2026 秋冬俄罗斯电商品类需求（家电 / 汽配 / 宠物 / 家居）

**主源（本节骨架）**：新浪转载点财网《俄罗斯秋冬季选品，Ozon 数据里 6 个品类正在爆发》`https://news.sina.com.cn/sx/2026-09-30/detail-initqkpc7127430.shtml`（**2026-09-30**；数据口径为 Ozon 季节性品类披露，经 Retail.ru、塔斯社/FinanceMail 转引）：

| 品类 | 秋冬公开数据 | 统计口径 |
|---|---|---|
| **汽配（冬季出行为主）** | 9 月轮胎需求=全年月均 **1.69×**，10 月 **2.11×**（2024–2025 月均基线）；2025 年 10 月上半月汽车化学品≈上年同期 **2×**，玻璃/车锁除冰剂 **>2.5×** | Ozon 季节性披露 |
| **家电/居家电器（家用桑拿 SPA 类）** | 2025 年 9–10 月家用红外迷你桑拿=上年同期 **4.2×**；家用蒸汽桑拿 **1.7×** | 塔斯社/FinanceMail 转述 Ozon |
| **家居（香氛+收纳储存）** | 2025 年 9 月家居香氛销量=上年同期 **2×**；2026 年 8 月末家用果蔬灭菌用品 **5.4×**、腌渍容器 **3.1×**、罐头套装 **2.6×**、真空包装机 **2.1×** | Ozon→Retail.ru |
| **冬季运动** | 跑步机 9–3 月月均=淡季 **1.7×**；冬钓雪橇 **10.7×**、冰钻 **6.4×**、水下钓鱼相机 **4.2×**、冰爪防滑鞋套 **18.8×** | Ozon 品类观察 |
| **新年布置** | 2025 年 11 月 vs 当年 Q3：新年摆件/树饰购买频次 **9×**、装饰树 **8×**、2026 日历 **6×** | Ozon 大促观察 |

**家电补充**：敏感人群细分小家电（婴儿/面部/花卉加湿器）销量涨 **67 倍**（AMZ123《Ozon 9 月市场趋势》`https://m.amz123.com/t/qboiDRgN`，2026-09 检索）；智能家居 2025 年在 Ozon 销售额 **+120%**，智能门锁/摄像头增速约 **150%**、空气净化器 **+90%**（AMZ123《2025 俄罗斯电商爆品指南》，转述类，弱源标注）；Ozon 2026 年拟推中高端平台 **Ozon Select**，覆盖时装/美妆/香水/**数码家电**/家居五大品类（chwe.cn，**2025-10**，`https://www.chwe.cn/article/YILcYDqeOya`）。

**宠物**：俄宠物保有量超 **5000 万只**，宠物定制服饰年增速 **150%**（网易号「克机技速」OZON POD 选品文，**2026-08-06**，`https://www.163.com/dy/article/L3L4AL4R05569H62.html`——⚠ 自媒体含 HICUSTOM 广告，弱源）；2026 年 Ozon 对轻量家居、宠物用品流量扶持提升（feikua 新手指南，弱源）；历史锚点：Ozon 宠物品类曾有 **+94%** 增长记录（zooinform，2022）。**结论：方向性一致（宠物=确定性增长带），具体倍数建议只作选品假设、不作承诺依据。**

**汽配背景**：俄乘用车保有量约 **4774 万辆**（截至 2026-07，俄汽车统计局，转引自上主源）；冬季胎法规强制——轮胎/除冰剂季节峰机制明确（主源表）。

**大盘（2026 展望的基数）**：2025 年俄电商规模 **+36% 至 1650 亿美元**（Ozon 全球 CEO Stepan Gusamov，新浪财经 2025-12-29《俄罗斯跨境电商迈向 2.0》）；Ozon 市场份额 **34.4%** 位列第二（2025Q1，amz123 引 Data Insight 口径）。

**数据纪律备注**：本节多数倍数是 Ozon 官方 PR 经俄媒/中文跨境媒体转述，或自媒体带货文——**方向可信、绝对倍数需以当季 Ozon 官方披露复核**；宠物类的硬数字最弱（已标弱源）。

---

## 附：检索测试记录（2026-10-28）

- 可抓：`seller-edu.ozon.ru/api-ozon/how-to-api`（搜索摘录）、`docs.ozon.ru/api/seller`（Auth tag 摘录）、`oceanservice/facts` 类静态页、新浪/163/AMZ123 文章页。
- 抓不到（SPA/反爬）：`seller-edu.ozon.ru/api-ozon/rabota-s-chastnymi-prilojeniyami`（重定向循环）、`docs.ozon.ru/api/seller` 错误码表（JS 渲染）、`dev.ozon.ru` 部分页。相关结论已用官方页标题+社区帖佐证并标注置信度。

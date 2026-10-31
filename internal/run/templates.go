package run

// Templates 四档起步模板：每档都走同一串关卡，但验收标准随档位升高。
//
// 「有用」的判据写在 Why 里：解决作者自己最近遇到的一个具体麻烦，
// 三天后还会打开它——这就是有用。

// 通用关卡线（从零做开源项目的骨架）。
func stagesFor(t Tier) []Stage {
	core := []Stage{
		{
			ID:     "s1-skeleton",
			Goal:   "第一关：让 AI 写一个能跑的空壳，你来验它",
			Check:  "贴三样：①你让它跑起来的那条命令 ②它的输出 ③你判断「这算启动成功」的依据（哪一行说明真起来了，而不是报错或卡住）",
			Hint:   "这关考的不是写代码，是验收：把需求说给 AI 让它写，你负责跑起来并判断它对不对",
			Expect: nil, // 需要模型/人判：判断输出里有没有「启动成功」类语义
		},
		{
			ID:     "s2-loop",
			Goal:   "做出最小闭环：一个输入进、一个结果出",
			Check:  "给它一个真实输入，能拿到你要的那个输出",
			Hint:   "把最核心的一步先写死（硬编码也行），先跑通链路，再替换成真实实现",
			Expect: nil,
		},
		{
			ID:     "s3-test",
			Goal:   "加第一条测试：让「能跑」变成「跑不坏」",
			Check:  "跑测试命令，输出里出现 ok / pass / 1 passed",
			Hint:   "只测一件事：核心那一步在给定输入下的输出。不要追求覆盖率",
			Expect: []string{"ok", "PASS", "pass", "passed", "OK"},
		},
		{
			ID:     "s4-readme",
			Goal:   "写 README：别人能不能用，取决于这三段",
			Check:  "README 里有「这是什么 / 怎么装 / 怎么用」三段，且能照着跑通",
			Hint:   "第一段一句话说清解决谁的什么麻烦；第二段给一条命令；第三段给一个真实例子",
			Expect: nil,
		},
		{
			ID:     "s5-release",
			Goal:   "发 v0.1：让它有一个可引用的版本",
			Check:  "打上 v0.1 标签（或写了 CHANGELOG 第一条），仓库能看到这个版本",
			Hint:   "版本号不必等完美：v0.1 的意思是「第一个能给别人看的东西」",
			Expect: []string{"v0.1"},
		},
		{
			ID:     "s6-useful",
			Goal:   "三天后再打开它一次：确认它是「有用」而不是「做完了」",
			Check:  "三天后回来打一句结论：还在用 / 不用了（并写一句为什么）",
			Hint:   "不用了也是好结果——它说明你找到了真需求，而不是完成了作业",
			Expect: nil,
		},
	}

	// 档位越高，多出来的能力各加一关（插在测试之后、发版之前）。
	extra := []Stage{}
	switch t {
	case TierStore:
		extra = append(extra, Stage{
			ID:     "s3b-store",
			Goal:   "让数据留得下来：关掉再开，东西还在",
			Check:  "写入一条数据，重启程序，再读出来是同一条",
			Hint:   "先用最笨的办法（一个 JSON 文件），不要一上来就用数据库",
			Expect: nil,
		})
	case TierAPI:
		extra = append(extra, Stage{
			ID:     "s3b-api",
			Goal:   "接一个外部接口：它要和真实世界换数据",
			Check:  "调用一次真实接口，把拿到的结果落成一条可复查的记录",
			Hint:   "先把它返回的原始结构打印出来，再决定你要哪几个字段",
			Expect: nil,
		})
	case TierUsers:
		extra = append(extra, Stage{
			ID:     "s3b-user",
			Goal:   "交给一个真人用：拿到第一条外部反馈",
			Check:  "有一个不是你的人用过，并说出了一句「哪里不好用」",
			Hint:   "找一个具体的人、一个具体场景，别发群里等人来",
			Expect: nil,
		})
	}

	out := core[:3]
	out = append(out, extra...)
	out = append(out, core[3:]...)
	return out
}

// Templates 返回四档起步模板（顺序即推荐顺序）。
func Templates() []Project {
	return []Project{
		{
			ID:     "t1-fix-my-annoyance",
			Tier:   TierTool,
			Title:  "先解决你自己的一个麻烦（单文件小工具）",
			Why:    "你最近三天有没有一件事，每次做都要手动重复？把它自动化，就是第一个项目",
			Stages: stagesFor(TierTool),
		},
		{
			ID:     "t2-remember-things",
			Tier:   TierStore,
			Title:  "做一个「记得住」的小系统",
			Why:    "把散落在聊天、截图、笔记里的东西攒起来，下次能找回来",
			Stages: stagesFor(TierStore),
		},
		{
			ID:     "t3-talk-to-world",
			Tier:   TierAPI,
			Title:  "做一个和外部世界换数据的工具",
			Why:    "天气、汇率、快递、题库——任选一个真实接口，把它变成对你有用的东西",
			Stages: stagesFor(TierAPI),
		},
		{
			ID:     "t4-someone-uses-it",
			Tier:   TierUsers,
			Title:  "把前三个里最好的那个，交给一个真人用",
			Why:    "有人用、有人抱怨，才算「有用」。这一步决定它是不是作品",
			Stages: stagesFor(TierUsers),
		},
	}
}

// TemplateByID 按 ID 取模板。
func TemplateByID(id string) (Project, bool) {
	for _, p := range Templates() {
		if p.ID == id {
			return p, true
		}
	}
	return Project{}, false
}

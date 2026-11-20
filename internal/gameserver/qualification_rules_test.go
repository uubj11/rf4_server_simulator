package gameserver

import (
	"context"
	"slices"
	"strconv"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

// 1001「钓获 3 种不同的鱼」：按鱼种去重计数，集满目标值（3）记完成时刻并封顶。
// 判定只由上鱼事件驱动，重复鱼种不增加分子；达标后不再计数（增量下发为空，
// 客户端不会重复弹完成窗），完成时刻保持稳定。
func TestQualificationRule1001DistinctSpecies(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	uid := sess.Userid
	land := func(species string) map[int64]store.QualificationState {
		t.Helper()
		return srv.evaluateQualifications(ctx, qualificationEvent{Kind: qevFishLanded, Species: species, WeightG: 500, LengthM: .3})
	}
	state := func() (int, float64) {
		t.Helper()
		states, err := db.QualificationStates(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		st := states[1001]
		return len(st.Counted), st.CompletedTS
	}

	// 同一事件会推进所有适用分支（1001/4001/4101/4301…），这里只看 1001。
	if ch := land("perch"); ch[1001].RuleID != 1001 {
		t.Fatalf("第一种鱼应产生 1001 变化，实得 %v", ch)
	}
	if n, ts := state(); n != 1 || ts != 0 {
		t.Fatalf("第一种鱼后应为 1/未完成，实得 %d/%g", n, ts)
	}
	if ch := land("perch"); ch[1001].RuleID != 0 { // 同种重复，无变化
		t.Fatalf("重复鱼种不应产生 1001 变化，实得 %v", ch[1001])
	}
	land("roach")
	if n, ts := state(); n != 2 || ts != 0 {
		t.Fatalf("两种鱼后应为 2/未完成，实得 %d/%g", n, ts)
	}
	if ch := land("bream"); ch[1001].CompletedTS == 0 { // 第三种，达标
		t.Fatalf("第三种鱼应达标并返回 1001 变化，实得 %v", ch)
	}
	n, ts := state()
	if n != 3 || ts == 0 {
		t.Fatalf("三种鱼后应为 3/已完成，实得 %d/%g", n, ts)
	}
	if ch := land("pike"); ch[1001].RuleID != 0 { // 达标后封顶，不再计数
		t.Fatalf("达标后 1001 应封顶无变化，实得 %v", ch[1001])
	}
	n2, ts2 := state()
	if n2 != 3 || ts2 != ts {
		t.Fatalf("达标后应停在 3 且完成时刻不变，实得 %d/%g（原 %g）", n2, ts2, ts)
	}
}

// 去重键映射成稳定 GUID：同键同 GUID、异键异 GUID（客户端只读集合长度）。
func TestQualificationCountedGUIDStable(t *testing.T) {
	if qualificationCountedGUID("perch") != qualificationCountedGUID("perch") {
		t.Fatal("同键应映射到同一 GUID")
	}
	if qualificationCountedGUID("perch") == qualificationCountedGUID("roach") {
		t.Fatal("异键应映射到不同 GUID")
	}
	if len(qualificationCountedGUID("perch")) != 32 {
		t.Fatal("GUID 应为 32 位 hex")
	}
}

// 计数器对象 5004 的形状：Idx0 规则编号、Idx1 鱼列表（分子=长度）、Idx2 完成时刻。
func TestQualificationCounterObjectShape(t *testing.T) {
	counted := []store.QualificationFish{
		{GUID: qualificationCountedGUID("perch"), Species: "perch"},
		{GUID: qualificationCountedGUID("roach"), Species: "roach"},
	}
	obj := qualificationCounterObject(1001, counted, 0)
	body, err := proto.EncodeValue(obj, wiregen.Table())
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := proto.DecodeValue(body, 0, wiregen.Table())
	if err != nil {
		t.Fatal(err)
	}
	if root.TypeID != 5004 {
		t.Fatalf("计数器类型应为 5004，实得 %d", root.TypeID)
	}
	if got := *root.Fields[0].Num; got != 1001 {
		t.Fatalf("Idx0 规则编号应为 1001，实得 %d", got)
	}
	list := root.Fields[1].Val
	if list == nil || len(list.Items) != 2 {
		t.Fatalf("Idx1 鱼列表长度应为 2（进度分子），实得 %v", list)
	}
	if got := *root.Fields[2].Num; got != 0 {
		t.Fatalf("未完成时 Idx2 应为 0，实得 %d", got)
	}
	// 完成时 Idx2 > 0，客户端据此直接判满。
	done := qualificationCounterObject(1001, counted, 1_700_000_000)
	dbody, _ := proto.EncodeValue(done, wiregen.Table())
	droot, _, err := proto.DecodeValue(dbody, 0, wiregen.Table())
	if err != nil {
		t.Fatal(err)
	}
	if *droot.Fields[2].Num == 0 {
		t.Fatal("完成时 Idx2 应 > 0")
	}
}

// 5003 快照：Idx2 计数器（容器 "201"）与 Idx1 鱼记录（容器 "202"）配套下发，
// 计数器 Guid[] 引用的每条 GUID 都能在鱼记录列表里找到（任务鱼表渲染所需）。
func TestBuild5003CarriesCountersAndFishRecords(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	for _, sp := range []string{"perch", "roach", "bream"} {
		srv.evaluateQualifications(ctx, qualificationEvent{Kind: qevFishLanded, Species: sp, WeightG: 500, LengthM: .3, Grade: 2, RigType: 1})
	}
	obj := srv.build5003(context.Background(), sess.Userid)
	body, err := proto.EncodeValue(obj, wiregen.Table())
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := proto.DecodeValue(body, 0, wiregen.Table())
	if err != nil {
		t.Fatal(err)
	}
	if root.TypeID != 5003 {
		t.Fatalf("根对象应为 5003，实得 %d", root.TypeID)
	}
	counters := root.Fields[2].Val
	if counters == nil || len(counters.Items) == 0 {
		t.Fatalf("应下发计数器（至少含 1001），实得 %v", counters)
	}
	var counter *proto.Value
	for _, it := range counters.Items {
		if it.TypeID == 5004 && *it.Fields[0].Num == 1001 {
			counter = it
			break
		}
	}
	if counter == nil {
		t.Fatalf("计数器列表缺少 1001，实得 %v", counters)
	}
	guids := counter.Fields[1].Val
	if guids == nil || len(guids.Items) != 3 {
		t.Fatalf("1001 进度分子应为 3，实得 %v", guids)
	}
	if *counter.Fields[2].Num == 0 {
		t.Fatal("集满 3 种后完成时刻应 > 0")
	}
	fish := root.Fields[1].Val
	if fish == nil || len(fish.Items) != 3 {
		t.Fatalf("应配套下发 3 条鱼记录，实得 %v", fish)
	}
	byGUID := make(map[string]bool)
	for _, f := range fish.Items {
		if f.TypeID != 5005 {
			t.Fatalf("鱼记录应为 5005，实得 %d", f.TypeID)
		}
		byGUID[f.Fields[0].Hex] = true
	}
	for _, g := range guids.Items {
		if !byGUID[g.Hex] {
			t.Fatalf("计数器 GUID %s 缺少配套鱼记录", g.Hex)
		}
	}
}

// 分支 1 条件型规则：重量门槛、拟饵种类、拟饵族集合、地点/档位组合。
// 判定入口只由事件驱动，测试直接构造事件验证过滤与去重键。
func TestQualificationBranch1Filters(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	uid := sess.Userid
	land := func(ev qualificationEvent) map[int64]store.QualificationState {
		t.Helper()
		ev.Kind = qevFishLanded
		return srv.evaluateQualifications(ctx, ev)
	}
	state := func(id int64) (int, float64) {
		t.Helper()
		states, err := db.QualificationStates(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		st := states[id]
		return len(st.Counted), st.CompletedTS
	}
	seq := 0
	catch := func(ev qualificationEvent) {
		t.Helper()
		seq++
		ev.CatchGUID = qualificationCountedGUID("catch-" + string(rune('a'+seq)))
		land(ev)
	}

	// 1002「3 公斤以上（含）」：2999g 不计，3000g 达标。
	catch(qualificationEvent{Species: "carp", WeightG: 2999})
	if n, ts := state(1002); n != 0 || ts != 0 {
		t.Fatalf("2999g 不应计入 1002，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "carp", WeightG: 3000})
	if n, ts := state(1002); n != 1 || ts == 0 {
		t.Fatalf("3000g 应达标 1002，实得 %d/%g", n, ts)
	}

	// 1003「7 种不同的拟饵」：天然饵与重复拟饵不计，第 7 种拟饵达标。
	catch(qualificationEvent{Species: "perch", Bait: "worm_01", BaitCat: 27})
	for i := 0; i < 6; i++ {
		catch(qualificationEvent{Species: "perch", Bait: "spoon_" + string(rune('a'+i)), BaitCat: 17})
	}
	catch(qualificationEvent{Species: "perch", Bait: "spoon_a", BaitCat: 17}) // 重复
	if n, ts := state(1003); n != 6 || ts != 0 {
		t.Fatalf("6 种拟饵后应为 6/未完成，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "perch", Bait: "crank_x", BaitCat: 17})
	if n, ts := state(1003); n != 7 || ts == 0 {
		t.Fatalf("7 种拟饵应达标，实得 %d/%g", n, ts)
	}

	// 1302「每种勺子/旋转亮片/鱼形硬饵/表水路亚钓获稀有鱼」：普通鱼不计。
	rare := func(bait string) qualificationEvent {
		return qualificationEvent{Species: "perch", Bait: bait, BaitCat: 17, Grade: 3}
	}
	catch(rare("spoon_a"))
	catch(rare("spinner_a"))
	catch(rare("crank_a"))
	catch(qualificationEvent{Species: "perch", Bait: "topwater_a", BaitCat: 17, Grade: 2}) // 非奖杯
	if n, ts := state(1302); n != 3 || ts != 0 {
		t.Fatalf("3 个拟饵族后应为 3/未完成（普通鱼不计），实得 %d/%g", n, ts)
	}
	catch(rare("topwater_a"))
	if n, ts := state(1302); n != 4 || ts == 0 {
		t.Fatalf("4 个拟饵族应达标，实得 %d/%g", n, ts)
	}

	// 1201「白河、人造拟饵、至少 5kg 的哲罗鱼」：地点、钓组、重量匹配，档位不限。
	taimen := qualificationEvent{
		Species: "taimen", WeightG: 6000, RigType: 5, Grade: 2,
		LocLevel: "level_010_belaya",
	}
	catch(qualificationEvent{
		Species: "taimen", WeightG: 6000, RigType: 5, Grade: 3,
		LocLevel: "level_011_ladoga_02",
	})
	if n, ts := state(1201); n != 0 || ts != 0 {
		t.Fatalf("地点不符不应计入 1201，实得 %d/%g", n, ts)
	}
	catch(taimen)
	if n, ts := state(1201); n != 1 || ts == 0 {
		t.Fatalf("白河 5kg 普通档哲罗应达标 1201，实得 %d/%g", n, ts)
	}

	// 1601「岸边、人造拟饵、稀有白北鲑或白鲑鱼」：船上不计。
	catch(qualificationEvent{
		Species: "nelma", RigType: 5, Grade: 3, LocLevel: "level_010_belaya",
		IsShore: false,
	})
	if n, ts := state(1601); n != 0 || ts != 0 {
		t.Fatalf("船上不应计入从岸边条件，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{
		Species: "wfish", RigType: 5, Grade: 3, LocLevel: "level_010_belaya",
		IsShore: true,
	})
	if n, ts := state(1601); n != 1 || ts == 0 {
		t.Fatalf("岸边稀有白鲑鱼应达标 1601，实得 %d/%g", n, ts)
	}
}

// 「奖杯/稀有」按个体档位而非物种稀有度：常见鱼种（物种稀有度 0）的奖杯个体
// 必须计数；稀有鱼种但未达奖杯档不计。物种稀有度只用于文案明确说「鱼种」
// 的任务（8603），否则 perch/atl_cod/halibut_w 等常见鱼种任务无法完成。
func TestQualificationTrophyUsesIndividualGrade(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	uid := sess.Userid
	seq := 0
	catch := func(ev qualificationEvent) {
		t.Helper()
		seq++
		ev.Kind = qevFishLanded
		ev.CatchGUID = qualificationCountedGUID("grade-" + string(rune('a'+seq)))
		srv.evaluateQualifications(ctx, ev)
	}
	state := func(id int64) (int, float64) {
		t.Helper()
		states, err := db.QualificationStates(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		st := states[id]
		return len(st.Counted), st.CompletedTS
	}

	// 2103「ThreeWay 钓组奖杯河鲈」：perch 物种稀有度 0；合格档不计，奖杯档计。
	catch(qualificationEvent{Species: "perch", RigType: 10, Grade: 2, Rarity: 1})
	if n, _ := state(2103); n != 0 {
		t.Fatalf("合格档不应计入 2103（物种稀有度不再参与判定），实得 %d", n)
	}
	catch(qualificationEvent{Species: "perch", RigType: 10, Grade: 3, Rarity: 0})
	if n, ts := state(2103); n != 1 || ts == 0 {
		t.Fatalf("常见鱼种的奖杯个体应达标 2103，实得 %d/%g", n, ts)
	}

	// 8501「稀有鳕鱼」：atl_cod 物种稀有度 0；蓝冠个体（>=3）达标。
	catch(qualificationEvent{Species: "atl_cod", Grade: 4, Rarity: 0, RigType: 50, LocLevel: evLocNorthSea})
	if n, ts := state(8501); n != 1 || ts == 0 {
		t.Fatalf("鳕鱼蓝冠个体应达标 8501，实得 %d/%g", n, ts)
	}

	// 7602「rare trophy 野生鲤」：奖杯档(3)不计，蓝冠档(4)才计。
	catch(qualificationEvent{Species: "common_carp", RigType: 24, Grade: 3})
	if n, _ := state(7602); n != 0 {
		t.Fatalf("奖杯档不应计入 7602（需蓝冠），实得 %d", n)
	}
	catch(qualificationEvent{Species: "common_carp", RigType: 24, Grade: 4})
	if n, ts := state(7602); n != 1 || ts == 0 {
		t.Fatalf("野生鲤蓝冠应达标 7602，实得 %d/%g", n, ts)
	}
}

// 全部分支 1-8 的 168 条规则必须都在静态目标表里（分母 > 0），且规则编号不重复。
func TestQualificationBranchRulesCovered(t *testing.T) {
	seen := make(map[int64]bool)
	for _, r := range qualificationRules {
		if seen[r.ID] {
			t.Fatalf("规则 %d 重复注册", r.ID)
		}
		seen[r.ID] = true
		if r.Event != qevFishLanded {
			t.Fatalf("规则 %d 事件类型异常", r.ID)
		}
		if qualificationTarget(uint64(r.ID)) == 0 {
			t.Fatalf("规则 %d 缺少静态目标值", r.ID)
		}
	}
	for branch := int64(1); branch <= 8; branch++ {
		for stage := int64(0); stage < 7; stage++ {
			for task := int64(1); task <= 3; task++ {
				ruleID := branch*1000 + stage*100 + task
				if !seen[ruleID] {
					t.Fatalf("分支 %d 规则 %d 未注册", branch, ruleID)
				}
			}
		}
	}
}

// 分支 2 汲钩钓族：SimpleJig/Texas/DropShot/Carolina/ThreeWay/Wacky/HairyJig
// 计入「汲钩钓钓组」，普通路亚钓组不计；2601 排除维基。
func TestQualificationBranch2Filters(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	uid := sess.Userid
	seq := 0
	catch := func(ev qualificationEvent) map[int64]store.QualificationState {
		t.Helper()
		seq++
		ev.Kind = qevFishLanded
		ev.CatchGUID = qualificationCountedGUID("b2-" + string(rune('a'+seq)))
		return srv.evaluateQualifications(ctx, ev)
	}
	state := func(id int64) (int, float64) {
		t.Helper()
		states, err := db.QualificationStates(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		st := states[id]
		return len(st.Counted), st.CompletedTS
	}

	// 2001「汲钩钓钓组 3 种鱼」：普通路亚钓组(5) 不计。
	catch(qualificationEvent{Species: "perch", RigType: 5})
	catch(qualificationEvent{Species: "perch", RigType: 6})
	catch(qualificationEvent{Species: "roach", RigType: 7})
	if n, ts := state(2001); n != 2 || ts != 0 {
		t.Fatalf("两种汲钩族鱼后应为 2/未完成，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "bream", RigType: 11})
	if n, ts := state(2001); n != 3 || ts == 0 {
		t.Fatalf("三种鱼应达标 2001，实得 %d/%g", n, ts)
	}

	// 2103「分支子线(ThreeWay=10) 稀有河鲈」：链斗式(13) 不计。
	catch(qualificationEvent{Species: "perch", RigType: 13, Grade: 3})
	if n, ts := state(2103); n != 0 || ts != 0 {
		t.Fatalf("非分支子线钓组不应计入 2103，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "perch", RigType: 10, Grade: 3})
	if n, ts := state(2103); n != 1 || ts == 0 {
		t.Fatalf("分支子线稀有河鲈应达标 2103，实得 %d/%g", n, ts)
	}

	// 2301「旧奥斯特罗格湖 汲钩 10kg 欧鲇」：水域不符不计。
	catch(qualificationEvent{Species: "w_catfish", RigType: 6, WeightG: 12000, LocLevel: "level_010_belaya"})
	if n, ts := state(2301); n != 0 || ts != 0 {
		t.Fatalf("水域不符不应计入 2301，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "w_catfish", RigType: 6, WeightG: 12000, LocLevel: "level_004_torfyanoe_ozero"})
	if n, ts := state(2301); n != 1 || ts == 0 {
		t.Fatalf("旧奥斯特罗格湖 10kg 欧鲇应达标 2301，实得 %d/%g", n, ts)
	}

	// 2501「维基钓组 10 种 5kg 以上」：重复鱼种与不足 5kg 不计。
	catch(qualificationEvent{Species: "perch", RigType: 11, WeightG: 6000})
	catch(qualificationEvent{Species: "perch", RigType: 11, WeightG: 7000})
	catch(qualificationEvent{Species: "roach", RigType: 11, WeightG: 4999})
	if n, ts := state(2501); n != 1 || ts != 0 {
		t.Fatalf("维基钓组应只累计 1 种足重鱼，实得 %d/%g", n, ts)
	}

	// 2601「除维基外任何汲钩族 10 种稀有鱼」：维基不计，其他汲钩族计入。
	// 前面的 2103 已用分支子线钓到稀有河鲈，因此这里比较增量。
	before, _ := state(2601)
	catch(qualificationEvent{Species: "chartus_kuoriius", RigType: 11, Grade: 3})
	if n, ts := state(2601); n != before || ts != 0 {
		t.Fatalf("维基不应增加 2601，实得 %d/%g（原 %d）", n, ts, before)
	}
	catch(qualificationEvent{Species: "chartus_kuoriius", RigType: 10, Grade: 3})
	if n, ts := state(2601); n != before+1 || ts != 0 {
		t.Fatalf("分支子线稀有鱼应增加 2601，实得 %d/%g（原 %d）", n, ts, before)
	}

	// 2603「维基钓组 任何稀有鲟鱼」：sterlet 属鲟鱼，达标。
	catch(qualificationEvent{Species: "sterlet", RigType: 11, Grade: 3})
	if n, ts := state(2603); n != 1 || ts == 0 {
		t.Fatalf("维基稀有鲟鱼应达标 2603，实得 %d/%g", n, ts)
	}
}

// 分支 3 轻型/超轻：竿硬度档尾缀判定，纺车竿限定竿型 1，小拟饵按 4cm 上限。
func TestQualificationBranch3Filters(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	uid := sess.Userid
	seq := 0
	catch := func(ev qualificationEvent) map[int64]store.QualificationState {
		t.Helper()
		seq++
		ev.Kind = qevFishLanded
		ev.CatchGUID = qualificationCountedGUID("b3-" + string(rune('a'+seq)))
		return srv.evaluateQualifications(ctx, ev)
	}
	state := func(id int64) (int, float64) {
		t.Helper()
		states, err := db.QualificationStates(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		st := states[id]
		return len(st.Counted), st.CompletedTS
	}

	// 3001「轻型渔具 5 种鱼」：中型竿不计，超轻计入轻型。
	catch(qualificationEvent{Species: "perch", RodPower: "M"})
	catch(qualificationEvent{Species: "perch", RodPower: "L"})
	catch(qualificationEvent{Species: "roach", RodPower: "UL"})
	if n, ts := state(3001); n != 2 || ts != 0 {
		t.Fatalf("轻型两种鱼后应为 2/未完成，实得 %d/%g", n, ts)
	}

	// 3301「岸边 超轻 ≤4cm 鱼形硬饵 稀有鱼」：船长、超尺寸、非超轻都不计。
	base := qualificationEvent{Species: "perch", Grade: 3, IsShore: true, RodPower: "UL", Bait: "crank_a", BaitCat: 17, LureLenCM: 4}
	ev := base
	ev.IsShore = false
	catch(ev)
	ev = base
	ev.LureLenCM = 5
	catch(ev)
	ev = base
	ev.RodPower = "L"
	catch(ev)
	if n, ts := state(3301); n != 0 || ts != 0 {
		t.Fatalf("船/超尺寸/非超轻不应计入 3301，实得 %d/%g", n, ts)
	}
	catch(base)
	if n, ts := state(3301); n != 1 || ts == 0 {
		t.Fatalf("岸边超轻 4cm 鱼形硬饵稀有鱼应达标 3301，实得 %d/%g", n, ts)
	}

	// 3601「超轻型纺车竿 50 种鱼」：超轻枪柄竿(竿型2)不计，超轻纺车竿(1)计入。
	catch(qualificationEvent{Species: "perch", RodPower: "UL", RodType: 2})
	if n, ts := state(3601); n != 0 || ts != 0 {
		t.Fatalf("超轻枪柄竿不应计入超轻纺车竿，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "perch", RodPower: "UL", RodType: 1})
	if n, ts := state(3601); n != 1 || ts != 0 {
		t.Fatalf("超轻纺车竿应计入 3601，实得 %d/%g", n, ts)
	}

	// 3102「轻型/超轻 小拟饵 拟鲤/梅花鲈/银鲷/白眼欧鳊」：鱼种不符不计。
	catch(qualificationEvent{Species: "bream", RodPower: "L", Bait: "spoon_a", BaitCat: 17, LureLenCM: 3})
	if n, ts := state(3102); n != 0 || ts != 0 {
		t.Fatalf("鱼种不符不应计入 3102，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "ruffe", RodPower: "UL", Bait: "spoon_a", BaitCat: 17, LureLenCM: 3})
	if n, ts := state(3102); n != 1 || ts == 0 {
		t.Fatalf("超轻小拟饵梅花鲈应达标 3102，实得 %d/%g", n, ts)
	}
}

// 竿硬度档尾缀解析：取 item_id 末尾连续大写字母。
func TestRodPowerClass(t *testing.T) {
	cases := map[string]string{
		"spin_10001_S60L":     "L",
		"spin_10001_S60MH":    "MH",
		"cast_10055_CSM68UL":  "UL",
		"cast_10058_CSXF60UL": "UL",
		"tele_10001":          "",
		"float_abc":           "",
	}
	for id, want := range cases {
		if got := rodPowerClass(id); got != want {
			t.Fatalf("%s 硬度档应为 %q，实得 %q", id, want, got)
		}
	}
}

// 分支 4 手竿捕钓：天然饵种类、夜间、小体稀有鱼、单鱼经验、昆虫饵、线材质、水深。
func TestQualificationBranch4Filters(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	uid := sess.Userid
	seq := 0
	catch := func(ev qualificationEvent) map[int64]store.QualificationState {
		t.Helper()
		seq++
		ev.Kind = qevFishLanded
		ev.CatchGUID = qualificationCountedGUID("b4-" + string(rune('a'+seq)))
		ev.RodType = rodTypeTelescope
		ev.RigType = 1
		return srv.evaluateQualifications(ctx, ev)
	}
	state := func(id int64) (int, float64) {
		t.Helper()
		states, err := db.QualificationStates(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		st := states[id]
		return len(st.Counted), st.CompletedTS
	}

	// 4003「5 种不同的鱼饵」：拟饵与重复饵不计。
	catch(qualificationEvent{Species: "perch", Bait: "worm_01", BaitCat: 27})
	catch(qualificationEvent{Species: "perch", Bait: "worm_01", BaitCat: 27})
	catch(qualificationEvent{Species: "perch", Bait: "spoon_a", BaitCat: 17})
	if n, ts := state(4003); n != 1 || ts != 0 {
		t.Fatalf("重复饵/拟饵不应计入 4003，实得 %d/%g", n, ts)
	}

	// 4302「夜里 ≥6kg」：白天不计。
	catch(qualificationEvent{Species: "carp", WeightG: 7000, IsNight: false})
	if n, ts := state(4302); n != 0 || ts != 0 {
		t.Fatalf("白天不应计入 4302，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "carp", WeightG: 7000, IsNight: true})
	if n, ts := state(4302); n != 1 || ts == 0 {
		t.Fatalf("夜间 7kg 应达标 4302，实得 %d/%g", n, ts)
	}

	// 4401「≤150g 稀有鱼」：151g 不计。
	catch(qualificationEvent{Species: "perch", WeightG: 151, Grade: 3})
	if n, ts := state(4401); n != 0 || ts != 0 {
		t.Fatalf("151g 不应计入 4401，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "perch", WeightG: 150, Grade: 3})
	if n, ts := state(4401); n != 1 || ts == 0 {
		t.Fatalf("150g 稀有鱼应达标 4401，实得 %d/%g", n, ts)
	}

	// 4402「单尾 ≥7000 经验」。
	catch(qualificationEvent{Species: "perch", WeightG: 500, BaseXP: 6999, XP: 13998})
	if n, ts := state(4402); n != 0 || ts != 0 {
		t.Fatalf("6999 XP 不应计入 4402，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "perch", WeightG: 500, BaseXP: 7000, XP: 14000})
	if n, ts := state(4402); n != 1 || ts == 0 {
		t.Fatalf("7000 XP 应达标 4402，实得 %d/%g", n, ts)
	}

	// 4501 真实条件仅允许昆虫类 7，幼虫类 2 不计。
	catch(qualificationEvent{Species: "roach", Bait: "worm_01", BaitCat: 27, BaitClass: 6})
	if n, ts := state(4501); n != 0 || ts != 0 {
		t.Fatalf("蚯蚓不应计入昆虫饵，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "roach", Bait: "maggot", BaitCat: 27, BaitClass: 2})
	if n, _ := state(4501); n != 0 {
		t.Fatalf("幼虫类不应计入 4501，实得 %d", n)
	}
	catch(qualificationEvent{Species: "roach", Bait: "grasshopper", BaitCat: 27, BaitClass: 7})
	if n, ts := state(4501); n != 1 || ts != 0 {
		t.Fatalf("昆虫类应计入 4501，实得 %d/%g", n, ts)
	}

	// 4502「超轻 + 尼龙/碳线 + ≥500g 稀有鱼」：编织主线且无前导不计。
	catch(qualificationEvent{Species: "perch", WeightG: 600, Grade: 3, RodTestMax: 5, MainLine: 3, LineStrengthKG: 1.5})
	if n, ts := state(4502); n != 0 || ts != 0 {
		t.Fatalf("编织线不应计入 4502，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "perch", WeightG: 600, Grade: 3, RodTestMax: 5, MainLine: 3, LeaderMat: 2, LineStrengthKG: 1.5})
	if n, ts := state(4502); n != 1 || ts == 0 {
		t.Fatalf("碳线前导应达标 4502，实得 %d/%g", n, ts)
	}

	// 4603 的十种白鲑名单不包含 wfish，深度不足也不计。
	catch(qualificationEvent{Species: "wfish", WeightG: 800, Grade: 3, DepthCM: 400})
	catch(qualificationEvent{Species: "sig", WeightG: 800, Grade: 3, DepthCM: 399})
	if n, ts := state(4603); n != 0 || ts != 0 {
		t.Fatalf("3.99m 不应计入 4603，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "sig", WeightG: 800, Grade: 3, DepthCM: 400})
	if n, ts := state(4603); n != 1 || ts == 0 {
		t.Fatalf("4m 稀有白鲑应达标 4603，实得 %d/%g", n, ts)
	}
}

// 分支 5/6：博洛尼亚/竞赛竿与飞德/Picker 的竿型、钓组、饵类过滤。
func TestQualificationBranches56Filters(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	uid := sess.Userid
	seq := 0
	catch := func(ev qualificationEvent) {
		t.Helper()
		seq++
		ev.Kind = qevFishLanded
		ev.CatchGUID = qualificationCountedGUID("b56-" + string(rune('a'+seq%26)) + string(rune('a'+seq/26%26)))
		srv.evaluateQualifications(ctx, ev)
	}
	state := func(id int64) (int, float64) {
		t.Helper()
		states, err := db.QualificationStates(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		st := states[id]
		return len(st.Counted), st.CompletedTS
	}

	// 5001 博洛尼亚钓组(2) 5 种鱼；其它钓组不计。
	catch(qualificationEvent{Species: "perch", RigType: 2})
	catch(qualificationEvent{Species: "roach", RigType: 2})
	catch(qualificationEvent{Species: "bream", RigType: 5})
	if n, ts := state(5001); n != 2 || ts != 0 {
		t.Fatalf("5001 应为 2/未完成，实得 %d/%g", n, ts)
	}

	// 5003 博洛尼亚钓组 + 5 种幼虫饵：蚯蚓(6) 不算幼虫。
	catch(qualificationEvent{Species: "perch", RigType: 2, Bait: "maggot", BaitCat: 27, BaitClass: 2})
	catch(qualificationEvent{Species: "perch", RigType: 2, Bait: "bloodworm", BaitCat: 27, BaitClass: 2})
	catch(qualificationEvent{Species: "perch", RigType: 2, Bait: "worm_01", BaitCat: 27, BaitClass: 6})
	if n, ts := state(5003); n != 2 || ts != 0 {
		t.Fatalf("5003 应为 2/未完成，实得 %d/%g", n, ts)
	}

	// 5202 博洛尼亚竿(4) + 昆虫饵 + 3kg。
	catch(qualificationEvent{Species: "carp", WeightG: 3000, RodType: 4, Bait: "maggot", BaitCat: 27, BaitClass: 2})
	if n, ts := state(5202); n != 1 || ts == 0 {
		t.Fatalf("5202 应达标，实得 %d/%g", n, ts)
	}

	// 5402 竞赛竿(5) + ≥30m + ≥10000 XP。
	catch(qualificationEvent{Species: "bream", RodType: 5, CastM: 29.9, XP: 12000})
	if n, ts := state(5402); n != 0 || ts != 0 {
		t.Fatalf("29.9m 不应计入 5402，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "bream", RodType: 5, CastM: 30, XP: 10000})
	if n, ts := state(5402); n != 1 || ts == 0 {
		t.Fatalf("5402 应达标，实得 %d/%g", n, ts)
	}

	// 5502 竞赛钓组(21) + 黑鲩 ≥20kg。
	catch(qualificationEvent{Species: "b_carp", WeightG: 20000, RigType: 21})
	if n, ts := state(5502); n != 1 || ts == 0 {
		t.Fatalf("5502 应达标，实得 %d/%g", n, ts)
	}

	// 6001 飞德竿(10) 5 种鱼；Picker(9) 不计飞德竿任务。
	catch(qualificationEvent{Species: "perch", RodType: 9})
	catch(qualificationEvent{Species: "perch", RodType: 10})
	if n, ts := state(6001); n != 1 || ts != 0 {
		t.Fatalf("6001 应为 1/未完成，实得 %d/%g", n, ts)
	}

	// 6201 飞德/Picker + 面团(13) 10 种鱼。
	catch(qualificationEvent{Species: "roach", RodType: 10, Bait: "dough_cottage", BaitCat: 27, BaitClass: 13})
	if n, ts := state(6201); n != 1 || ts != 0 {
		t.Fatalf("6201 面团应计入，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "bream", RodType: 10, Bait: "dough_cottage", BaitCat: 27, BaitClass: 13})
	if n, _ := state(6201); n != 2 {
		t.Fatalf("6201 应为 2，实得 %d", n)
	}

	// 6202 Picker + 红虫 + 2kg。
	catch(qualificationEvent{Species: "roach", RodType: 9, WeightG: 2000, Bait: "bloodworm", BaitCat: 27})
	if n, ts := state(6202); n != 1 || ts == 0 {
		t.Fatalf("6202 应达标，实得 %d/%g", n, ts)
	}

	// 6301 飞德/Picker + 鳗鱼 ≥2kg；6303 Picker + 稀有长吻梅花鲈。
	catch(qualificationEvent{Species: "eel", RodType: 10, WeightG: 2000})
	if n, ts := state(6301); n != 1 || ts == 0 {
		t.Fatalf("6301 应达标，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "ruffe_n", RodType: 9, Grade: 3})
	if n, ts := state(6303); n != 1 || ts == 0 {
		t.Fatalf("6303 应达标，实得 %d/%g", n, ts)
	}

	// 6501 Picker ≥30kg；6502 飞德钓组 + 稀有鱼。
	catch(qualificationEvent{Species: "carp", RodType: 9, WeightG: 30000})
	if n, ts := state(6501); n != 1 || ts == 0 {
		t.Fatalf("6501 应达标，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "perch", RigType: 2, Grade: 3})
	if n, ts := state(6502); n != 0 || ts != 0 {
		t.Fatalf("非飞德钓组不应计入 6502，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "perch", RigType: 13, Grade: 3})
	if n, ts := state(6502); n != 1 || ts != 0 {
		t.Fatalf("6502 应为 1/5，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "perch", RigType: 16, Grade: 3})
	if n, _ := state(6502); n != 1 {
		t.Fatalf("发丝钓组 16 不应计入 6502，实得 %d", n)
	}
	for _, rig := range []int64{12, 14, 15, 17} {
		catch(qualificationEvent{Species: "perch", RigType: rig, Grade: 3})
	}
	if n, ts := state(6502); n != 5 || ts == 0 {
		t.Fatalf("6502 五种飞德钓组应达标，实得 %d/%g", n, ts)
	}
}

// 分支 7/8：发丝钓组、颗粒尺寸/味型、鲤鱼变种与海洋钓组/深度/稀有度。
func TestQualificationBranches78Filters(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	uid := sess.Userid
	seq := 0
	catch := func(ev qualificationEvent) {
		t.Helper()
		seq++
		ev.Kind = qevFishLanded
		ev.CatchGUID = qualificationCountedGUID("b78-" + string(rune('a'+seq%26)) + string(rune('a'+seq/26%26)))
		srv.evaluateQualifications(ctx, ev)
	}
	state := func(id int64) (int, float64) {
		t.Helper()
		states, err := db.QualificationStates(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		st := states[id]
		return len(st.Counted), st.CompletedTS
	}

	// 7001 发丝钓组(24) 5 种鱼；非发丝钓组不计。
	catch(qualificationEvent{Species: "carp", RigType: 2})
	catch(qualificationEvent{Species: "carp", RigType: 24})
	if n, ts := state(7001); n != 1 || ts != 0 {
		t.Fatalf("7001 应为 1/未完成，实得 %d/%g", n, ts)
	}

	// 7002 十种味道（颗粒类 9）：味道按饵 ID 去掉尾部尺寸区分。
	// 使用真实颗粒 item_id：同一味道不同尺寸（pch14/18/24）只算 1 种。
	for _, item := range []string{
		"plt_cb_cvr14", "plt_cb_cwf14", "plt_cb_pch14", "plt_cb_prm14", "plt_cb_sam14",
		"plt_cb_sfp15", "plt_cb_sgp18", "plt_cb_spa18", "plt_cb_sqh16", "plt_hvz_ap14",
	} {
		catch(qualificationEvent{Species: "carp", Bait: item, BaitCat: 27, BaitClass: 9})
	}
	if n, ts := state(7002); n != 10 || ts == 0 {
		t.Fatalf("7002 十种味道应达标，实得 %d/%g", n, ts)
	}

	// 7102 颗粒 ≥25mm + ≥20kg；7201 ≤10mm + 稀有。
	catch(qualificationEvent{Species: "carp", WeightG: 20000, Bait: "plt_x", BaitCat: 27, BaitClass: 9, BaitSizeMM: 24})
	if n, ts := state(7102); n != 0 || ts != 0 {
		t.Fatalf("24mm 不应计入 7102，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "carp", WeightG: 20000, Bait: "plt_x", BaitCat: 27, BaitClass: 9, BaitSizeMM: 25})
	if n, ts := state(7102); n != 1 || ts == 0 {
		t.Fatalf("7102 应达标，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "carp", Grade: 3, Bait: "zbb_x", BaitCat: 27, BaitClass: 17, BaitSizeMM: 11})
	if n, ts := state(7201); n != 0 || ts != 0 {
		t.Fatalf("11mm 不应计入 7201，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "carp", Grade: 3, Bait: "zbb_x", BaitCat: 27, BaitClass: 17, BaitSizeMM: 10})
	if n, ts := state(7201); n != 1 || ts == 0 {
		t.Fatalf("7201 应达标，实得 %d/%g", n, ts)
	}

	// 7301 Maggot 钓组(29) 稀有鱼。
	catch(qualificationEvent{Species: "carp", Grade: 3, RigType: 29})
	if n, ts := state(7301); n != 1 || ts == 0 {
		t.Fatalf("7301 应达标，实得 %d/%g", n, ts)
	}

	// 7401 五种人面鲤；7402 白化鲤 ≥15kg；7603 五种指定鲤。
	catch(qualificationEvent{Species: "carp_ghost"})
	if n, ts := state(7401); n != 1 || ts != 0 {
		t.Fatalf("7401 应为 1/5，实得 %d/%g", n, ts)
	}
	for _, species := range []string{"n_carp_ghost", "m_carp_ghost", "l_carp_ghost", "r_carp_ghost"} {
		catch(qualificationEvent{Species: species})
	}
	if n, ts := state(7401); n != 5 || ts == 0 {
		t.Fatalf("7401 五种人面鲤应达标，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "m_carp_albino", WeightG: 15000, RigType: 24})
	if n, ts := state(7402); n != 1 || ts == 0 {
		t.Fatalf("7402 应达标，实得 %d/%g", n, ts)
	}
	for _, sp := range []string{"n_carp", "r_carp", "l_carp", "m_carp_red"} {
		catch(qualificationEvent{Species: sp})
	}
	if n, ts := state(7603); n != 4 || ts != 0 {
		t.Fatalf("7603 四种后应未完成，实得 %d/%g", n, ts)
	}
	catch(qualificationEvent{Species: "carp_red"})
	if n, ts := state(7603); n != 5 || ts == 0 {
		t.Fatalf("7603 五种应达标，实得 %d/%g", n, ts)
	}

	// 8003 七种不同饵（拟饵或天然饵）。
	marineCatch := func(ev qualificationEvent) {
		t.Helper()
		ev.LocLevel = evLocNorthSea
		if ev.RigType == 0 {
			ev.RigType = 50
		}
		catch(ev)
	}
	for i := 0; i < 7; i++ {
		marineCatch(qualificationEvent{Species: "perch", Bait: "spoon_" + string(rune('a'+i)), BaitCat: 17})
	}
	if n, ts := state(8003); n != 7 || ts == 0 {
		t.Fatalf("8003 七种饵应达标，实得 %d/%g", n, ts)
	}

	// 8101 柳叶亮片(50) ≥15kg；8202 海洋汲钩(51) 15 种鱼。
	marineCatch(qualificationEvent{Species: "atl_cod", WeightG: 15000, RigType: 50})
	if n, ts := state(8101); n != 1 || ts == 0 {
		t.Fatalf("8101 应达标，实得 %d/%g", n, ts)
	}
	for i := 0; i < 15; i++ {
		marineCatch(qualificationEvent{Species: "m" + string(rune('a'+i)), RigType: 51})
	}
	if n, ts := state(8202); n != 15 || ts == 0 {
		t.Fatalf("8202 应达标，实得 %d/%g", n, ts)
	}

	// 8303 海洋底部/带响珠 20 种鱼。
	for i := 0; i < 20; i++ {
		marineCatch(qualificationEvent{Species: "n" + string(rune('a'+i)), RigType: 55})
	}
	if n, ts := state(8303); n != 20 || ts == 0 {
		t.Fatalf("8303 应达标，实得 %d/%g", n, ts)
	}

	// 8401 ≥120m 五种鱼；8503 ≥120m 稀有鱼。
	marineCatch(qualificationEvent{Species: "o1", DepthCM: 11999})
	if n, ts := state(8401); n != 0 || ts != 0 {
		t.Fatalf("119.99m 不应计入 8401，实得 %d/%g", n, ts)
	}
	for i := 0; i < 5; i++ {
		marineCatch(qualificationEvent{Species: "o" + string(rune('a'+i)), DepthCM: 12000})
	}
	if n, ts := state(8401); n != 5 || ts == 0 {
		t.Fatalf("8401 应达标，实得 %d/%g", n, ts)
	}
	marineCatch(qualificationEvent{Species: "red_fish", DepthCM: 12000, Grade: 3})
	if n, ts := state(8503); n != 1 || ts == 0 {
		t.Fatalf("8503 应达标，实得 %d/%g", n, ts)
	}

	// 8502 死鱼底部(59) 3 种奖杯鱼；8603 罕见/极其罕见鱼种（物种稀有度）。
	for i := 0; i < 3; i++ {
		marineCatch(qualificationEvent{Species: "p" + string(rune('a'+i)), RigType: 59, Grade: 3})
	}
	if n, ts := state(8502); n != 3 || ts == 0 {
		t.Fatalf("8502 应达标，实得 %d/%g", n, ts)
	}
	marineCatch(qualificationEvent{Species: "q0", Rarity: 0})
	if n, _ := state(8603); n != 0 {
		t.Fatalf("普通鱼种不应计入 8603，实得 %d", n)
	}
	marineCatch(qualificationEvent{Species: "q1", Rarity: 1, Grade: 2})
	if n, _ := state(8603); n != 0 {
		t.Fatalf("罕见鱼种未达星不应计入 8603，实得 %d", n)
	}
	marineCatch(qualificationEvent{Species: "q1", Rarity: 1, Grade: 3})
	if n, ts := state(8603); n != 1 || ts == 0 {
		t.Fatalf("8603 稀有鱼种应达标，实得 %d/%g", n, ts)
	}
}

// 浮钓分支按钓组类别过滤，竿型限制跟随任务文案（qlfctn_t_<id>）。
// 回归：1402「使用抽停硬饵钓组」只能配抽停竿(12)、1501 水表路亚常用枪柄竿(2)，
// 旧实现按分支把 1xxx 全限成纺车竿(1)，这两条永远无法完成，客户端「鱼竿」行
// 还会显示文案里没有的「纺车竿」。
func TestQualificationRodTypesFollowTaskText(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	uid := sess.Userid
	seq := 0
	land := func(ev qualificationEvent) map[int64]store.QualificationState {
		t.Helper()
		seq++
		ev.Kind = qevFishLanded
		if ev.CatchGUID == "" {
			ev.CatchGUID = qualificationCountedGUID("rod-" + strconv.Itoa(seq))
		}
		return srv.evaluateQualifications(ctx, ev)
	}
	state := func(id int64) (int, float64) {
		t.Helper()
		states, err := db.QualificationStates(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		st := states[id]
		return len(st.Counted), st.CompletedTS
	}

	// 4602 按真实条件限定伸缩竿，其他浮钓竿也不计。
	for _, rod := range []int64{1, 2, 3, 4, 5, 12, 15, 16, 0} {
		before, _ := state(4602)
		rigs := map[int64]int64{1: 6, 2: 5, 3: 1, 4: 2, 5: 4, 12: 22, 15: 50, 16: 55}
		land(qualificationEvent{Species: "sp" + strconv.FormatInt(rod, 10), RodType: rod, RigType: rigs[rod]})
		want := before
		if rod == 3 {
			want++
		}
		if n, _ := state(4602); n != want {
			t.Fatalf("竿型 %d 的 4602 进度应为 %d，实得 %d", rod, want, n)
		}
	}

	// 1402：客户端 pgjliohpiie 只允许抽停竿(12)装抽停钓组(22)；
	// 纺车杆 + 路亚钓组 + 抽停饵不算（文案点名的是抽停硬饵钓组）。
	land(qualificationEvent{Species: "n.pike", Grade: 3, RodType: 1, RigType: 5, Bait: "jerkbaits_1148", BaitCat: 17})
	if n, _ := state(1402); n != 0 {
		t.Fatalf("纺车竿+路亚钓组不应计入 1402，实得 %d", n)
	}
	land(qualificationEvent{Species: "n.pike", Grade: 3, RodType: 12, RigType: 22, Bait: "jerkbaits_1148", BaitCat: 17})
	if n, ts := state(1402); n != 1 || ts == 0 {
		t.Fatalf("抽停竿+抽停钓组钓获奖杯狗鱼应完成 1402，实得 %d/%g", n, ts)
	}

	// 1501：水表路亚 + 稀有细鳞鲑，枪柄竿(2)必须能完成。
	// 用 X-Walker（item_id 是 plug_w_*，拟饵类型 5=水表）验证：只按前缀会误判
	// 成鱼形硬饵，必须按 item_def 的拟饵类型归族。
	land(qualificationEvent{Species: "lenok_o", Grade: 3, RodType: 2, Bait: "plug_w_2368", BaitCat: 17})
	if n, _ := state(1501); n != 0 {
		t.Fatalf("拟饵类型缺失时不应把 X-Walker 当水表饵，实得 %d", n)
	}
	land(qualificationEvent{Species: "lenok_o", Grade: 3, RodType: 2, Bait: "plug_w_2368", BaitCat: 17, LureType: 5})
	if n, ts := state(1501); n != 1 || ts == 0 {
		t.Fatalf("枪柄竿水表路亚钓获奖杯细鳞鲑应完成 1501，实得 %d/%g", n, ts)
	}

	// 文案明确写「博洛尼亚钓竿」的 5101 仍只认竿型 4：竞赛竿(5)不计。
	land(qualificationEvent{Species: "burbot", RodType: 5, RigType: 21, WeightG: 3200, Bait: "worm_01", BaitCat: 27, Drift: true})
	if n, _ := state(5101); n != 0 {
		t.Fatalf("竞赛竿不应计入 5101（文案写博洛尼亚钓竿），实得 %d", n)
	}
	land(qualificationEvent{Species: "burbot", RodType: 4, RigType: 2, WeightG: 3200, Bait: "worm_01", BaitCat: 27, Drift: true})
	if n, _ := state(5101); n != 1 {
		t.Fatalf("博洛尼亚竿应计入 5101，实得 %d", n)
	}

	// 文案明确写「飞德竿」的 6001：Picker(9) 不计，飞德竿(10) 才计。
	land(qualificationEvent{Species: "zander", RodType: 9})
	if n, _ := state(6001); n != 0 {
		t.Fatalf("Picker 不应计入 6001（文案写飞德竿），实得 %d", n)
	}
	land(qualificationEvent{Species: "zander", RodType: 10})
	if n, _ := state(6001); n != 1 {
		t.Fatalf("飞德竿应计入 6001，实得 %d", n)
	}
}

// 手竿分支的 21 项均按真实 5007.Idx5 限定伸缩竿。
func TestQualificationRodTypesCoverage(t *testing.T) {
	want := map[int64][]int64{
		1402: {12}, // 抽停硬饵钓组 → 抽停竿

		3302: {1}, 3501: {1}, 3601: {1}, // 轻型/超轻纺车竿

		4303: {3}, 4502: {3}, // 明写超轻伸缩竿

		5101: {4}, 5103: {4}, 5201: {4}, 5202: {4}, 5203: {4}, 5401: {4}, // 博洛尼亚钓竿
		5301: {4, 5}, 5302: {4, 5}, 5303: {4, 5}, 5601: {4, 5}, 5602: {4, 5},
		5402: {5}, 5403: {5}, 5501: {5}, 5503: {5}, // 竞赛竿

		6001: {10}, 6002: {10}, 6003: {10}, 6101: {10}, 6103: {10}, // 飞德竿
		6202: {9}, 6303: {9}, 6402: {9}, 6501: {9}, // Picker 竿
		6102: {9, 10}, 6201: {9, 10}, 6203: {9, 10}, 6301: {9, 10}, 6302: {9, 10},
		6401: {9, 10}, 6403: {9, 10}, 6503: {9, 10}, 6601: {9, 10}, 6602: {9, 10}, 6603: {9, 10},
	}
	seen := 0
	for _, r := range qualificationRules {
		if r.ID >= 9000 {
			continue // 活动分支另有逐任务表 eventRodTypes
		}
		seen++
		got := qualificationRodTypesFor(r.ID)
		if r.ID/1000 == 4 {
			want[r.ID] = []int64{3}
		}
		if !slices.Equal(got, want[r.ID]) {
			t.Fatalf("规则 %d 竿型应为 %v，实得 %v", r.ID, want[r.ID], got)
		}
	}
	if seen != 168 {
		t.Fatalf("永久资格任务应为 168 条，实得 %d", seen)
	}
	for id := range want {
		found := false
		for _, r := range qualificationRules {
			if r.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("清单里的规则 %d 不在规则表中", id)
		}
	}
}

// 旧存档的咬钩快照没有 qual_bait_lure_type：恢复后结算时按 item_id 反查
// item_def，避免 X-Walker（plug_w_*，类型 5）被前缀兜底误判成鱼形硬饵。
func TestQualificationOldSnapshotBackfillsLureType(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`INSERT INTO item_def(numeric_id,item_id,category,attrs_json)
		VALUES(990001,'plug_w_2368',17,'{"mbemakijdeh":5}')`); err != nil {
		t.Fatal(err)
	}
	f := &RolledFish{
		Species: "lenok_o", Grade: 3, WeightG: 6000,
		QualBait: qualificationBait{ID: "plug_w_2368", Cat: 17}, // 旧快照：LureType=0
	}
	ev := srv.qualificationEventForCatch(context.Background(), f, 5)
	if ev.LureType != 5 {
		t.Fatalf("旧快照应按 item_def 回填拟饵类型 5，实得 %d", ev.LureType)
	}
	if fam := lureFamilyOf(ev); fam != "topwater" {
		t.Fatalf("回填后应归为 topwater，实得 %q", fam)
	}
}

// 咬钩快照解析必须把 item_def.mbemakijdeh 读进 qualificationBait.LureType，
// 否则真实挂载流程不会走类型判定。
func TestQualificationMountedBaitReadsLureType(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	guid := "0123456789abcdef0123456789abcdef"
	if _, err := db.SQL.Exec(`INSERT INTO item_def(numeric_id,item_id,category,attrs_json)
		VALUES(990002,'plug_w_2369',17,'{"mbemakijdeh":5,"bbhhojphpfa":1101004800}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`INSERT INTO item (guid, uid, item_id, numeric_id, wire_class, type_id, container, slot, qty, durability, workmanship, created_ts)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		guid, sess.Userid, "plug_w_2369", 990002, "enikeammfen", 0, "rig", 0, 1, 1, 1, 0); err != nil {
		t.Fatal(err)
	}
	bait := srv.qualificationMountedBait(context.Background(), guid)
	if bait.Cat != 17 || bait.LureType != 5 {
		t.Fatalf("挂载快照应解析出拟饵类型 5，实得 %+v", bait)
	}
}

// 7002「10 种不同味道的颗粒」：味道只能按饵本身区分——颗粒的
// item_def.acclagajcdn 全是同一档位（25），按它数永远到不了 10。
// 同一味道的不同尺寸（plt_cb_pch14/18/24）算 1 种。
func TestQualificationRule7002PelletFlavors(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	uid := sess.Userid
	seq := 0
	catch := func(item string) {
		t.Helper()
		seq++
		srv.evaluateQualifications(ctx, qualificationEvent{
			Kind: qevFishLanded, Species: "carp", CatchGUID: qualificationCountedGUID("pellet-" + strconv.Itoa(seq)),
			Bait: item, BaitCat: 27, BaitClass: 9, BaitFlavor: 25,
		})
	}
	count := func() int {
		t.Helper()
		states, err := db.QualificationStates(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		return len(states[7002].Counted)
	}
	catch("plt_cb_pch14")
	catch("plt_cb_pch18")
	if n := count(); n != 1 {
		t.Fatalf("同一味道的不同尺寸应算 1 种，实得 %d", n)
	}
	catch("plt_cb_cvr14")
	if n := count(); n != 2 {
		t.Fatalf("不同味道应算 2 种，实得 %d", n)
	}
}

// 5007 条件字段使用官服静态规则；4502 覆盖竿型、硬度、鱼档位、重量、线型和线强。
func TestQualificationRule5007Conditions(t *testing.T) {
	decode := func(ruleID uint64) *proto.Value {
		t.Helper()
		body, err := proto.EncodeValue(qualificationRule(ruleID), wiregen.Table())
		if err != nil {
			t.Fatal(err)
		}
		root, _, err := proto.DecodeValue(body, 0, wiregen.Table())
		if err != nil {
			t.Fatal(err)
		}
		return root
	}
	u32s := func(v *proto.Value) []uint32 {
		t.Helper()
		if v == nil || v.Kind != proto.KindU32Arr {
			t.Fatalf("应为 u32 数组，实得 %v", v)
		}
		out := make([]uint32, 0, len(v.Items))
		for _, it := range v.Items {
			out = append(out, uint32(it.Num))
		}
		return out
	}

	r4502 := decode(4502)
	if got := u32s(r4502.Fields[5].Val); len(got) != 1 || got[0] != 3 {
		t.Fatalf("4502 竿型应为 [3]，实得 %v", got)
	}
	if got := u32s(r4502.Fields[6].Val); len(got) != 1 || got[0] != 1 {
		t.Fatalf("4502 竿硬度应为 [1]，实得 %v", got)
	}
	if got := u32s(r4502.Fields[12].Val); len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("4502 鱼档位应为 [3,4]，实得 %v", got)
	}
	if got := uint32(*r4502.Fields[14].Num); got != 0x3f000000 {
		t.Fatalf("4502 最低重量位模式应为 0x3f000000，实得 %#x", got)
	}
	if got := uint32(*r4502.Fields[15].Num); got != 0x7f7fffff {
		t.Fatalf("4502 最高重量位模式应为 float32 max，实得 %#x", got)
	}
	if got := u32s(r4502.Fields[22].Val); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("4502 线型应为 [1,2]，实得 %v", got)
	}
	if got := uint32(*r4502.Fields[23].Num); got != 0x3fc00000 {
		t.Fatalf("4502 线强上限位模式应为 1.5，实得 %#x", got)
	}

	if got := uint32(*decode(5002).Fields[27].Num); got != 64 {
		t.Fatalf("5002 应带顺流位 64，实得 %d", got)
	}
}

// 顺水漂流：咬钩时 0x40 位才计数；4403 只认欧飘鱼/红眼鱼。
func TestQualificationDriftRules(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	uid := sess.Userid
	seq := 0
	catch := func(ev qualificationEvent) {
		t.Helper()
		seq++
		ev.Kind = qevFishLanded
		ev.CatchGUID = qualificationCountedGUID("drift-" + string(rune('a'+seq%26)))
		srv.evaluateQualifications(ctx, ev)
	}
	state := func(id int64) (int, float64) {
		t.Helper()
		states, err := db.QualificationStates(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		st := states[id]
		return len(st.Counted), st.CompletedTS
	}

	catch(qualificationEvent{Species: "chub", RigType: 2, WeightG: 1000, RodType: 4})
	if n, _ := state(5002); n != 0 {
		t.Fatalf("未顺流不应计入 5002，实得 %d", n)
	}
	catch(qualificationEvent{Species: "chub", RigType: 2, WeightG: 1000, RodType: 4, Drift: true})
	if n, ts := state(5002); n != 1 || ts == 0 {
		t.Fatalf("顺流 1kg 应达标 5002，实得 %d/%g", n, ts)
	}

	catch(qualificationEvent{Species: "perch", RigType: 2, RodType: 3, Drift: true, Grade: 3})
	if n, _ := state(4403); n != 0 {
		t.Fatalf("非欧飘/红眼鱼不应计入 4403，实得 %d", n)
	}
	catch(qualificationEvent{Species: "sabrefish", RigType: 2, RodType: 3, Drift: true, Grade: 3})
	if n, ts := state(4403); n != 1 || ts == 0 {
		t.Fatalf("顺流稀有欧飘鱼应达标 4403，实得 %d/%g", n, ts)
	}

	// 手竿没有硬度尾缀时，测试上限 <=5g 视为超轻。
	ul := qUltralight()
	if !ul(qualificationEvent{RodTestMax: 5}) {
		t.Fatal("测试上限 5g 的手竿应视为超轻")
	}
	if ul(qualificationEvent{RodTestMax: 6}) {
		t.Fatal("测试上限 6g 不应视为超轻")
	}
}

// 拟饵族前缀分类：兼容单复数与鱼形硬饵多种前缀。
func TestLureFamilyPrefixes(t *testing.T) {
	cases := map[string]string{
		"spinner_x": "spinner", "spinnerbaits_x": "spinner",
		"spoon_a":    "spoon",
		"topwater_a": "topwater", "popper_a": "topwater", "poppers_a": "topwater",
		"jerkbait_a": "jerkbait", "jerkbaits_a": "jerkbait",
		"crank_a": "wobbler", "crankbaits_a": "wobbler", "plug_a": "wobbler", "shad_a": "wobbler",
		// 自制拟饵（hm_*）按子类型前缀归族；缺失会让所有「使用某类拟饵」的任务
		// 在玩家用手工饵时永远不计数。
		"hm_spinner_n1_bl_hq":  "spinner",
		"hm_spoon_mos_hq":      "spoon",
		"hm_walker_65f_001_hq": "topwater", "hm_popper_65f_001_hq": "topwater",
		"hm_jerk_f_100f_001_hq": "jerkbait",
		"hm_crank_130f_001_hq":  "wobbler", "hm_shad_140f_001_hq": "wobbler",
		"hm_pl_join_140s_001_hq": "wobbler", "hm_minnow_100s_hq": "wobbler",
		"hm_j_minnow_90s_hq": "wobbler", "hm_foamr_80black_hq": "",
		"softplastic_a": "", "worm_a": "",
	}
	for bait, want := range cases {
		if got := lureFamily(bait); got != want {
			t.Fatalf("%s 应归为 %q，实得 %q", bait, want, got)
		}
	}
}

// 拟饵族优先按 item_def 的拟饵类型判：X-Walker 的 item_id 是 plug_w_*，
// 前缀看着像鱼形硬饵，类型字段却是 5（水表），只按前缀会漏算水表任务。
func TestLureFamilyUsesLureType(t *testing.T) {
	cases := []struct {
		ev   qualificationEvent
		want string
	}{
		{qualificationEvent{Bait: "plug_w_2368", LureType: 5}, "topwater"},
		{qualificationEvent{Bait: "plug_w_2381", LureType: 4}, "wobbler"},
		{qualificationEvent{Bait: "jerkbaits_1148", LureType: 7}, "jerkbait"},
		{qualificationEvent{Bait: "spinnerbaits_1558", LureType: 1}, "spinner"},
		{qualificationEvent{Bait: "spoon_lures_1797", LureType: 2}, "spoon"},
		// 软饵（hm_foamr，类型 3）不属于这五族，显式返回空串。
		{qualificationEvent{Bait: "hm_foamr_80black_hq", LureType: 3}, ""},
		// 类型缺失时回落到前缀。
		{qualificationEvent{Bait: "topwater_1928"}, "topwater"},
		{qualificationEvent{Bait: "plug_w_2381"}, "wobbler"},
	}
	for _, tc := range cases {
		if got := lureFamilyOf(tc.ev); got != tc.want {
			t.Fatalf("%+v 应归为 %q，实得 %q", tc.ev, tc.want, got)
		}
	}
}

// 14/4 增量下发：达标当尾的增量包含该规则计数器（客户端据此弹一次完成）；
// 达标后封顶，后续鱼不再重发该规则，客户端无新边沿，故不重复弹窗。
// 其它分支的同类任务仍可同时推进，因此只断言 1001 本身。
func TestQualificationIncrementalDelivery(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	var last map[int64]store.QualificationState
	for _, sp := range []string{"perch", "roach", "bream"} {
		last = srv.evaluateQualifications(ctx, qualificationEvent{Kind: qevFishLanded, Species: sp})
	}
	counterIDs := func(states map[int64]store.QualificationState) map[uint32]bool {
		t.Helper()
		body, _ := proto.EncodeValue((&Server{}).build5003FromStates(states, build5012([]uint32{0}, seasonEndFiletime(0))), wiregen.Table())
		root, _, err := proto.DecodeValue(body, 0, wiregen.Table())
		if err != nil {
			t.Fatal(err)
		}
		ids := make(map[uint32]bool)
		if c := root.Fields[2].Val; c != nil {
			for _, it := range c.Items {
				ids[uint32(*it.Fields[0].Num)] = true
			}
		}
		return ids
	}
	// 达标当尾：增量含 1001 计数器。
	if ids := counterIDs(last); !ids[1001] {
		t.Fatalf("达标当尾增量应含 1001 计数器，实得 %v", ids)
	}
	// 后续鱼：1001 已封顶，不再出现在增量里。
	after := srv.evaluateQualifications(ctx, qualificationEvent{Kind: qevFishLanded, Species: "pike"})
	if after[1001].RuleID != 0 {
		t.Fatalf("达标后 1001 不应再变化，实得 %v", after[1001])
	}
	if ids := counterIDs(after); ids[1001] {
		t.Fatalf("后续鱼增量不应重发 1001 计数器，实得 %v", ids)
	}
}

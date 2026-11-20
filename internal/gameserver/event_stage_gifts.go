package gameserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// 活动阶段礼物：新年/周年面板每个阶段的奖励物品与数量，管理员经 tunable
// `event_stage_gifts` 配置（Control Deck 热更）。预览（5008.f2）随下一次
// 28/1（重启游戏客户端后登录）生效；发放判定在结算与 28/2 巡检时按最新配置执行。
//
// 5008.f2 是 string[]：wire 编码为 0x04 数组（u16 数量 + 每元素 u32 长度 +
// 原始 UTF8 目录键），与 1/5 配置版本响应同型，不能使用 args 容器。
// 客户端按目录键显示奖励预览并去重，数组本身不表达数量；实际发放数量
// 以配置为准（协议依据见 docs/debugging-workflow.md，客户端 4.0.25034）。

const eventStageGiftsKey = "event_stage_gifts"

// eventStageGiftMaxQty 单件奖励数量上限：防御手工配置笔误刷爆背包。
const eventStageGiftMaxQty = 99

type eventStageGift struct {
	ItemID string
	Qty    int64
}

// eventStageGiftPlan 一个已通过校验的阶段奖励计划。
type eventStageGiftPlan struct {
	Branch  int64
	Stage   int64
	Gifts   []eventStageGift
	Preview []string // 去重后的目录键，保持首次出现顺序
}

// eventStageGiftPlans 解析并校验 tunable 配置，按分支、阶段升序返回。
// JSON：{"<branch>": {"<stage>": [{"item":"fcb_coffee_can2","qty":1}]}}。
// stage=0 表示整条任务线完成后的额外奖励，奖杯仍由 qualification_rewards 发放。
// 非法条目跳过并 warn（单个坏条目不拖垮整份配置）。
func (s *Server) eventStageGiftPlans() []eventStageGiftPlan {
	raw := s.tunableS(eventStageGiftsKey, "")
	if raw == "" {
		return nil
	}
	var parsed map[string]map[string][]struct {
		Item string `json:"item"`
		Qty  int64  `json:"qty"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		s.log.Warn("阶段礼物配置解析失败", "err", err)
		return nil
	}
	var plans []eventStageGiftPlan
	for branchStr, stages := range parsed {
		branch, err := strconv.ParseInt(branchStr, 10, 32)
		if err != nil {
			s.log.Warn("阶段礼物配置分支号非法，跳过", "branch", branchStr)
			continue
		}
		counts, ok := eventTaskCounts[uint64(branch)]
		if !ok {
			s.log.Warn("阶段礼物配置分支不是活动分支，跳过", "branch", branch)
			continue
		}
		for stageStr, gifts := range stages {
			stage, err := strconv.ParseInt(stageStr, 10, 32)
			if err != nil || stage < 0 || stage > int64(len(counts)) {
				s.log.Warn("阶段礼物配置阶段号非法，跳过", "branch", branch, "stage", stageStr)
				continue
			}
			plan := eventStageGiftPlan{Branch: branch, Stage: stage}
			seen := map[string]bool{}
			for _, g := range gifts {
				// 本分支奖杯已有独立发放及领取标记，完成奖励不能再发一份。
				if stage == 0 && g.Item != "" && g.Item == qualificationCupByBranch[branch].ItemID {
					continue
				}
				if g.Item == "" {
					s.log.Warn("阶段礼物配置物品键为空，跳过", "branch", branch, "stage", stage)
					continue
				}
				qty := g.Qty
				if qty < 1 {
					qty = 1
				}
				if qty > eventStageGiftMaxQty {
					qty = eventStageGiftMaxQty
				}
				plan.Gifts = append(plan.Gifts, eventStageGift{ItemID: g.Item, Qty: qty})
				if !seen[g.Item] {
					seen[g.Item] = true
					plan.Preview = append(plan.Preview, g.Item)
				}
			}
			if len(plan.Gifts) == 0 {
				continue
			}
			plans = append(plans, plan)
		}
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].Branch != plans[j].Branch {
			return plans[i].Branch < plans[j].Branch
		}
		return plans[i].Stage < plans[j].Stage
	})
	return plans
}

// eventStageGiftPlanFor 取某分支某阶段的计划；未配置返回 nil。
func eventStageGiftPlanFor(plans []eventStageGiftPlan, branch, stage uint64) *eventStageGiftPlan {
	for i := range plans {
		if plans[i].Branch == int64(branch) && plans[i].Stage == int64(stage) {
			return &plans[i]
		}
	}
	return nil
}

// eventStageGiftPreview 构造 5008.f2 奖励预览键数组，未配置时发空字符串数组。
func eventStageGiftPreview(plans []eventStageGiftPlan, branch, stage uint64) *proto.Value {
	arr := &proto.Value{Kind: proto.KindArray}
	plan := eventStageGiftPlanFor(plans, branch, stage)
	if plan == nil {
		return arr
	}
	for _, key := range plan.Preview {
		arr.Items = append(arr.Items, proto.BytesOf([]byte(key)))
	}
	return arr
}

// eventCompletionGiftPreview 对应官服 5009.f4，顺序为额外奖励、既有奖杯。
func eventCompletionGiftPreview(plans []eventStageGiftPlan, branch uint64) *proto.Value {
	arr := eventStageGiftPreview(plans, branch, 0)
	if cup, ok := qualificationCupByBranch[int64(branch)]; ok {
		for _, v := range arr.Items {
			if v.Hex == proto.BytesOf([]byte(cup.ItemID)).Hex {
				return arr
			}
		}
		arr.Items = append(arr.Items, proto.BytesOf([]byte(cup.ItemID)))
	}
	return arr
}

// eventStageGiftKey 阶段礼物幂等键。
func eventStageGiftKey(uid, branch, stage int64) string {
	return fmt.Sprintf("event-stage-gift:%d:%d:%d", uid, branch, stage)
}

// ensureEventStageGifts 巡检活动分支的阶段礼物：阶段内已下发（非缺口）任务
// 全部完成且配置了奖励时发放，返回本次新发物品。调用点与分支奖杯一致：
// 14/4 结算后与 28/2 全量快照；无变化时只读。
// 与分支奖杯相同的兜底口径：首次巡检（28/2 或结算后）会对已配置且已达标、
// 但尚无 kv 标记的阶段补发，覆盖发放功能上线前的历史进度。
func (s *Server) ensureEventStageGifts(ctx context.Context, uid int64) []*store.Item {
	if s == nil || s.DB == nil {
		return nil
	}
	enabled := s.activeQualificationEventBranches(time.Now().Unix())
	if len(enabled) == 0 {
		return nil
	}
	plans := s.eventStageGiftPlans()
	if len(plans) == 0 {
		return nil
	}
	states, err := s.DB.QualificationStates(ctx, uid)
	if err != nil {
		s.log.Warn("阶段礼物巡检读进度失败", "uid", uid, "err", err)
		return nil
	}
	stageRules := map[int64][]int64{} // key = branch*100+stage，仅活动分支非缺口规则
	branchRules := map[int64][]int64{}
	for _, r := range qualificationRules {
		if eventGapRuleIDs[r.ID] {
			continue
		}
		branch := r.ID / 1000
		if _, ok := eventTaskCounts[uint64(branch)]; !ok {
			continue
		}
		key := branch*100 + (r.ID%1000)/100
		stageRules[key] = append(stageRules[key], r.ID)
		branchRules[branch] = append(branchRules[branch], r.ID)
	}
	var granted []*store.Item
	var names []string
	for i := range plans {
		plan := &plans[i]
		if !enabled[plan.Branch] {
			continue
		}
		rules := stageRules[plan.Branch*100+plan.Stage-1]
		if plan.Stage == 0 {
			rules = branchRules[plan.Branch]
		}
		if len(rules) == 0 {
			continue // 阶段没有可判定任务，不能凭空发放
		}
		complete := true
		for _, id := range rules {
			if states[id].CompletedTS <= 0 {
				complete = false
				break
			}
		}
		if !complete {
			continue
		}
		items, stageNames := s.grantEventStageGift(ctx, uid, plan)
		granted = append(granted, items...)
		if len(stageNames) > 0 {
			names = append(names, stageNames...)
		}
	}
	if len(names) > 0 {
		s.broadcastQualificationCups(uid, names)
	}
	return finalEventStageGiftItems(granted)
}

// finalEventStageGiftItems 保留每个 GUID 最后一次合并的状态，覆盖同阶段多行和跨阶段补发。
func finalEventStageGiftItems(rows []*store.Item) []*store.Item {
	indices := make(map[string]int, len(rows))
	out := make([]*store.Item, 0, len(rows))
	for _, row := range rows {
		if i, ok := indices[row.GUID]; ok {
			out[i] = row
		} else {
			indices[row.GUID] = len(out)
			out = append(out, row)
		}
	}
	return out
}

// grantEventStageGiftItemTx 沿用普通库存合并；目标堆容量不足时将当前奖励行另存为一堆。
func grantEventStageGiftItemTx(ctx context.Context, tx *sql.Tx, it *store.Item) (*store.Item, error) {
	def, err := itemDefByItemIDTx(ctx, tx, it.ItemID)
	if err != nil {
		return nil, err
	}
	merge := countableCategories[def.Category]
	if merge {
		limit := bundleQuantityLimit(it.WireClass)
		if it.Qty < 1 || it.Qty > limit {
			return nil, fmt.Errorf("阶段奖励数量超出数量槽范围: %s", it.ItemID)
		}
		// 与 grantPreparedItemTx 使用同一排序，容量判断和入账都在当前事务中。
		row, err := scanTxItem(tx.QueryRowContext(ctx, `SELECT `+txItemCols+`
			FROM item WHERE uid=? AND container=? AND item_id=?
			ORDER BY slot, created_ts, guid LIMIT 1`, it.UID, it.Container, it.ItemID))
		if err == nil {
			cur := stackCountTx(ctx, tx, row)
			merge = cur >= 0 && cur <= bundleQuantityLimit(row.WireClass)-it.Qty
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	row, _, err := grantPreparedItemTx(ctx, tx, it, merge)
	return row, err
}

// grantEventStageGift 单阶段发放：物品先按配置数量构造到正常背包，幂等键与全部入账
// 同一经济事务提交；已发过返回 nil。播报名称按配置顺序取全部物品的静态中文名，同种物品只取一次，
// 由调用方合并成一条发送。活动奖励是普通商品，不能复用资格奖杯的 house/奖赏分组。
func (s *Server) grantEventStageGift(ctx context.Context, uid int64, plan *eventStageGiftPlan) ([]*store.Item, []string) {
	// 已发阶段在构造物品前返回；事务内仍保留唯一键防止并发重复发放。
	if _, ok, _ := s.DB.KVGet(ctx, eventStageGiftKey(uid, plan.Branch, plan.Stage)); ok {
		return nil, nil
	}
	prepared := make([]*store.Item, 0, len(plan.Gifts))
	membershipDays := int64(0)
	for _, g := range plan.Gifts {
		def, err := s.DB.GetItemDefByItemID(ctx, g.ItemID)
		if err != nil || def == nil {
			s.log.Warn("阶段礼物定义不存在", "item", g.ItemID, "err", err)
			return nil, nil
		}
		// 官服预览含 premium_3d；本地直接兑现三天服务，不创建无法使用的凭证。
		if g.ItemID == "premium_3d" {
			membershipDays += 3 * g.Qty
			continue
		}
		count := g.Qty
		if countableCategories[def.Category] {
			count = 1
		}
		for i := int64(0); i < count; i++ {
			var it *store.Item
			if countableCategories[def.Category] {
				it, err = s.prepareNewItemWithCount(ctx, uid, g.ItemID, "backpack", g.Qty, "")
			} else {
				it, err = s.prepareNewItem(ctx, uid, g.ItemID, "backpack", 1, "")
			}
			if err != nil {
				s.log.Warn("阶段礼物物品构造失败", "uid", uid, "branch", plan.Branch,
					"stage", plan.Stage, "item", g.ItemID, "err", err)
				return nil, nil
			}
			prepared = append(prepared, it)
		}
	}
	tx, err := beginEconomyTx(ctx, s.DB)
	if err != nil {
		s.log.Warn("阶段礼物事务开启失败", "uid", uid, "err", err)
		return nil, nil
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO kv(k,v) VALUES(?,?)`,
		eventStageGiftKey(uid, plan.Branch, plan.Stage), "granted")
	if err != nil {
		s.log.Warn("阶段礼物幂等键写入失败", "uid", uid, "err", err)
		return nil, nil
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return nil, nil // 已发放过
	}
	if membershipDays > 0 {
		if err := grantEventMembershipTx(ctx, tx, uid, membershipDays, time.Now().Unix()); err != nil {
			s.log.Warn("活动会员发放失败", "uid", uid, "err", err)
			return nil, nil
		}
	}
	var out []*store.Item
	for _, it := range prepared {
		row, err := grantEventStageGiftItemTx(ctx, tx, it)
		if err != nil {
			s.log.Warn("阶段礼物发放失败", "uid", uid, "branch", plan.Branch,
				"stage", plan.Stage, "item", it.ItemID, "err", err)
			return nil, nil
		}
		out = append(out, row)
	}
	if err := tx.Commit(); err != nil {
		s.log.Warn("阶段礼物提交失败", "uid", uid, "err", err)
		return nil, nil
	}
	var names []string
	seen := make(map[string]bool, len(plan.Gifts))
	for _, gift := range plan.Gifts {
		if !seen[gift.ItemID] {
			seen[gift.ItemID] = true
			names = append(names, s.lootItemName(ctx, gift.ItemID))
		}
	}
	return finalEventStageGiftItems(out), names
}

// 与物品及阶段领取标记同事务；沿用会员购买的 profile 真源和在线刷新队列。
func grantEventMembershipTx(ctx context.Context, tx *sql.Tx, uid, days, now int64) error {
	var kind, expires int64
	if err := tx.QueryRowContext(ctx, `SELECT membership,membership_exp FROM profile WHERE uid=?`, uid).Scan(&kind, &expires); err != nil {
		return err
	}
	if kind == 2 {
		return nil
	} // 永久会员不能降级或覆盖到期时间。
	base := now
	if kind == 1 && expires > base {
		base = expires
	}
	if days <= 0 || days > (253402300799-base)/86400 {
		return fmt.Errorf("活动会员到期时间超出范围")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE profile SET membership=1,membership_exp=?,updated_ts=? WHERE uid=?`, base+days*86400, now, uid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO txn(uid,ts,kind,item_id,note) VALUES(?,?,'membership_reward','premium_3d',?)`, uid, now, fmt.Sprintf("event_reward_days=%d", days)); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO admin_queue(payload,created_ts) VALUES(?,?)`, fmt.Sprintf(`{"op":"refresh_player","uid":%d}`, uid), now)
	return err
}

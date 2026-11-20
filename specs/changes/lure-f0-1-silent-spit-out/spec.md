# 路亚 f0=1 叼住吐出改官方静默（咬饵≠咬钩）

Status: done

日期：2026-11-16 ｜ 分析：`docs/路亚脱钩率分析-2026-11-16.md`

## 背景与证据

路亚「脱钩率体感极高」实为两个叠加现象。客户端字节码复核出**两阶段模型**：

1. **阶段一 kldglnchdec**（14/11 时刻一次性掷骰）失败 → 鱼叼住拟饵 0.5~2.0s
   后吐出 = **f0=1**。此时鱼从未真正挂牢：客户端连「上鱼」通知都不出
   （gfgapohfogm 只在掷骰通过时调度 lbkpigfelil 通知），本地化模板表
   （4/5/6/7/10/30）无「没钩住/吐饵」类载体 → **官方对 f0=1 完全静默**。
   实证：当天 f0=1+hooked ×21 全部 gap 0.57~1.86s、负载帧全 1 条，
   含 18.9kg/1.45s——不存在秒跑 18.9kg 的物理世界。
2. **阶段二 kejihbadeej**（搏鱼期逐帧危险累计掷骰）失败 = **f0=2 真脱钩**，
   有真实搏鱼过程（21.4s/11 帧、42.0s/22 帧实例），仅占中鱼的 ~6%。

原实现把 14/12 已中鱼（st.Hooked）的 f0=1/2/8 一律按真脱钩播报 + 计
escapes + 触发大鱼脱钩补偿——给官方不存在的节点发了官方不存在的文案，
且把 ~75% 的「播报量」和统计污染源错挂在从未挂牢的鱼上。

## 改动

- `handlers_game.go` 14/12 f0=1/2/8 已中鱼分支按 f0 分流：
  - **f0=1 → 静默**：不 notifyFishEscape、不 bumpFishingCounter("escapes")、
    不 recordHuntEscape；日志记「叼住吐出」。
  - **f0=2/8 → 不变**：播报 + 计数 + Grade≥3 大鱼脱钩补偿。
  - 分支内不变的部分：渔轮磨损结算、同竿重排（scheduleBiteState）、
    多钩接管（promoteAfterActiveGone）、Pending/Hooked 状态清理。
- `progress_ops.go` 14/4 收竿未刺中补报（"收竿跑鱼"）取消——同属咬饵未咬钩，
  官方静默；保留日志。
- 不动：17/9 断件播报（官方模板 4/5/6/10/30 真实存在）、未知 f0 兜底分支的
  保守播报、`fish_escape_taunt` 开关、搏鱼磨损/经验/资格链、发窝度累积与消耗
  （只改 recordHuntEscape 的调用入口，consume 侧不动）。

## 验收

- `TestFishHookedEscapeSplitsByF0`：f0=1 奖杯级已中鱼 → 无推送、escapes=0，
  且 `BigMiss` / `CompFish` / `CompGrade` 均不变化；f0=2/8 → 24/27 蓝色模板 3 +
  跑鱼文案；`fish_escape_taunt=0` 回退官方模板 7。
- `TestFishingEndSettleUnhookedDoesNotCountEscape`：14/4 咬饵未咬钩收竿 →
  无 24/27 跑鱼推送、escapes 不增加。
- 实机：跑鱼播报量降到 ≈ 中鱼的 6%，全部伴随真实搏鱼时长（多负载帧）。

## 风险与回滚

- 风险面仅播报/计数/补偿三个副作用；状态机、磨损、重排路径零改动。
- 回滚 = 恢复 f0 分流前分支（git revert 单提交）。

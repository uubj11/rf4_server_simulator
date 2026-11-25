// 后端动作代码 → 管理员看得懂的中文。
export const AUDIT_LABELS = {
  login: '登录后台', tunable: '修改数值', player_update: '修改角色', grant_item: '发放物品', item_delete: '删除物品',
  keepnet_add: '放入护鱼网', keepnet_delete: '移出护鱼网', mail_send: '发送邮件', mail_delete: '删除邮件',
  skill_points: '修改技能点', skill_proficiency: '修改技能熟练度', kick: '踢下线', ban: '封禁',
  shop_override: '修改商品', shop_override_reset: '商品恢复默认', fish_config: '修改水域鱼种', fish_config_reset: '水域恢复默认',
  fish_zone_save: '保存鱼群区域', fish_zone_delete: '删除鱼群区域', bite_mode: '切换玩法风格', spawn_mode: '切换出鱼方式',
  hook_profile: '调整鱼钩', bait_rule_save: '修改鱼饵偏好', bait_rule_reset: '鱼饵偏好恢复默认', skill_gain_mode: '切换技能升级概率',
};
export const auditLabel = (a) => AUDIT_LABELS[a] || a || '—';

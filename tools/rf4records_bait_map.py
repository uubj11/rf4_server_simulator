# rf4records.com 英文饵名 -> item_id 手工补充表（天然饵/活饵；拟饵走 item_catalog.name 自动匹配）。
# 依据：RF4 天然饵标准英文名 + item_catalog.name_zh 对照；活饵统一 _lf（live fish）后缀。
# 未覆盖：Sweet corn（本地无独立玉米粒饵）、长尾组合饵（脚本按分号拆分逐成分映射）。
BAIT_MAP = {
    "A bunch of algae": "watergrass",
    "Bark beetle larva": "scolytinae",
    "Black-headed Worm": "worm_msor",
    "Bleak": "c_bleak_lf",
    "Bluegill": "bluegill_lf",
    "Bloodworm": "bloodworm",
    "Brandling Worm": "worm_volh",
    "Cabbage leaf": "cabbage",
    "Caddisfly": "caddisfly",
    "Caster": "maggot_caster",
    "Cockchafer": "maybug",
    "Cockchafer Larva": "maybug_larva",  # 土蚕（成虫是 maybug）
    "Cornmeal porridge": "porridge_corn",
    "Cricket": "cricket",
    "Crucian Carp": "c_carp_lf",
    "Dead herring": "fb_d_seld",
    "Diced cheese": "cheese_cube",
    "Diced potatoes": "potato",
    "Dung beetle": "dorbeetle",
    "Fly": "fly",
    "Frog": "baitfrog",  # 静态库无 frog，只有 baitfrog
    "Garlic dough": "dough_garlic",
    "Gibel Carp": "crucian_lf",
    "Gizzard Shad": "gizshad_lf",
    "Grasshopper": "grasshopper",
    "Gudgeon": "gudgeon_lf",
    "Honey dough": "dough_honey",
    "Large fillet - Saithe": "fb_bf_saida",
    "Lamprey": "lamprey_lf",
    "Lugworm": "fb_lugworm",
    "Maggot": "maggot",
    "Mayfly": "mayfly",
    "Mayfly larva": "naiad",
    "Mole cricket": "gryllotalpa",
    "Nereis": "fb_nereis",
    "Nightcrawler": "vypolzok_01",  # 地龙（不是莫斯科培育蚯蚓）
    "Pearl barley": "porridge_pearl_barley",
    "Perch": "perch_lf",
    "Pieces of fish": "fish_pieces",
    "Red compost worm": "muckworm",  # 红蚯蚓（粪蚯蚓是 worm_sur/worm_volh/worm_asurv）
    "Ruffe": "ruffe_lf",
    "Siberian Loach": "sybgolec_lf",  # 董氏须鳅（无 vjun 商品）
    "Rhinoceros beetle larva": "unibeetle_larva",
    "Horse-fly": "gadfly_little",  # 马蝇（牛虻是 gadfly）
    "River mussel meat": "unio_crassus",
    "Roach": "c_roach_lf",
    "Small fillet - Saithe": "fb_lf_saida",
    "Spices": "crn_op_scy",
    "Squid meat": "fb_squid",
    "Sweet dough": "dough_sweet",
    "Wheat seeds": "wheat_grain",
    "White Bream": "s_bream_lf",
    "Zebra mussel meat": "bait_dreissena",
    "Pea porridge": "porridge_pea",
    "Cottage cheese dough": "dough_cottage",
    "Wet bread": "bread",
    "Dead mackerel": "fb_d_makrel",
    "Diving beetle": "waterbug",
    "Leech": "black_leech",

    "Stonefly larva": "stonefly_nymph",
    "Worm": "worm_01"
}

// 代码由 tmp_analysis 流水线生成（gen_tables.py），数据=官服抓包 2026-11-10~17 实测值。

package gameserver

// 官服实测 XP 速率表。公式：XP = trunc(c(鱼种,档位) × W_g)（trunc=截断取整，与官服一致）。
// 实测优先原则：凡有实测的种（含单点）全用实测值，缺档用「本种锚 × 档位乘子」；
// 亲缘组（复用拉力 K 表 fishPullSpeciesGroup）仅兜零实测种。
// 推导与样本口径见 specs/changes/fish-xp-rate-table/spec.md。抓包原始数据不入库（CONTRIBUTING 禁令），此处仅数值。

// fishXPRateFallback 全局缺省速率（现行值，实测/组均未命中时兜底，理论上不可达）。
const fishXPRateFallback = 0.4

// fishXPRateC 官服实测 (鱼种,档位)→c。注释 = 样本数/重量范围/来源(310=结算,320=记录卡)/会话。
// [加成嫌疑] = 同组样本 XP/W 不自洽（官服疑有活动加成），c 取低值方向（去加成）。
var fishXPRateC = map[vgKey]float64{
	{"a.sleeper", 2}:         1.125,     // n=1 W=396~396g src=320
	{"a_char", 1}:            0.5,       // n=1 W=415~415g src=310
	{"a_char", 2}:            1.15,      // n=1 W=8916~8916g src=320
	{"a_grayling", 2}:        2.8565,    // n=1 W=1397~1397g src=320
	{"a_omul", 1}:            1.5989,    // n=1 W=283~283g src=320
	{"ambl_r", 2}:            11.1107,   // n=1 W=4567~4567g src=320
	{"asp", 1}:               0.8429,    // n=1 W=1614~1614g src=320
	{"atl_cod", 1}:           0.524495,  // n=2 W=2674~5677g src=310 [加成嫌疑→低值]
	{"atl_cod", 2}:           3.149971,  // n=1 W=30676~30676g src=320
	{"atl_herring", 2}:       32.499107, // n=2 W=560~637g src=310,320 [加成嫌疑→低值]
	{"b_bream", 2}:           10.0013,   // n=1 W=377~377g src=320
	{"b_carp", 2}:            2.08,      // n=1 W=7828~7828g src=310
	{"b_carp", 3}:            12.999953, // n=2 W=31932~31932g src=320
	{"b_gill", 1}:            0.1814,    // n=1 W=113~113g src=310
	{"b_gill", 3}:            1.3633,    // n=2 W=1383~1383g src=320
	{"barsch", 2}:            3.1996,    // n=1 W=714~714g src=320
	{"bh_carp", 3}:           4.3333,    // n=4 W=37894~37894g src=320
	{"bl_buffalo", 2}:        10.3995,   // n=1 W=3235~3235g src=320
	{"black_crappie", 2}:     3.9948,    // n=1 W=668~668g src=320
	{"bream", 3}:             1.4999,    // n=2 W=4414~4414g src=320
	{"buffalo", 2}:           0.936,     // n=1 W=14669~14669g src=320
	{"burbot", 2}:            1.3,       // n=1 W=4571~4571g src=320
	{"c.carp", 2}:            0.1858,    // n=1 W=1574~1574g src=310
	{"c.carp", 3}:            0.5714,    // n=3 W=2171~2439g src=320
	{"c.roach", 2}:           0.3007,    // n=1 W=680~680g src=310
	{"c.roach", 3}:           2.9988,    // n=2 W=1290~1290g src=320
	{"c_barbel", 1}:          0.5335,    // n=1 W=1478~1478g src=320
	{"c_bleak", 2}:           11.245,    // n=1 W=100~100g src=320
	{"c_nase", 2}:            4.6875,    // n=1 W=632~632g src=320
	{"c_squid", 2}:           75.0007,   // n=1 W=672~672g src=320
	{"carp", 1}:              0.1625,    // n=1 W=2434~2434g src=310
	{"carp", 2}:              0.52,      // n=5 W=4137~5710g src=310
	{"carp", 3}:              3.249971,  // n=2 W=25665~25665g src=320
	{"carp_ghost", 2}:        3.574895,  // n=2 W=4513~12632g src=310,320 [加成嫌疑→低值]
	{"ch_catfish", 2}:        0.4229,    // n=1 W=1997~1997g src=320
	{"cha_herring", 2}:       10.9065,   // n=1 W=230~230g src=320
	{"chartus_kuoriius", 2}:  2.25,      // n=1 W=6490~6490g src=320
	{"chir", 2}:              1.2998,    // n=1 W=1256~1256g src=320
	{"chlamys_isl", 2}:       83.3221,   // n=1 W=104~104g src=320
	{"coalfish", 1}:          0.22933,   // n=2 W=702~1692g src=310
	{"coalfish", 2}:          0.468285,  // n=2 W=3027~17823g src=310,320 [加成嫌疑→低值]
	{"common_carp", 3}:       6.239969,  // n=2 W=25197~25197g src=320
	{"conger", 1}:            2.5,       // n=1 W=3681~3681g src=320
	{"crawfish", 2}:          10.0027,   // n=1 W=186~186g src=320
	{"crucian", 2}:           0.137571,  // n=2 W=527~743g src=310 [加成嫌疑→低值]
	{"crucian", 3}:           0.5501,    // n=3 W=2299~2686g src=320
	{"dace", 3}:              13.8887,   // n=2 W=265~265g src=320
	{"din_l_carp", 1}:        0.5834,    // n=1 W=1715~1715g src=310
	{"din_l_carp", 2}:        2.999981,  // n=1 W=26479~26479g src=320
	{"din_m_carp", 2}:        1.3333,    // n=1 W=29005~29005g src=320
	{"din_n_carp", 2}:        6.1667,    // n=1 W=18864~18864g src=320
	{"dreissena", 2}:         5.6026,    // n=1 W=117~117g src=320
	{"e.chub", 2}:            0.6997,    // n=1 W=3482~3482g src=320
	{"e.pikeperch", 2}:       1.625,     // n=1 W=2783~2783g src=320
	{"e_bream", 2}:           1.3001,    // n=1 W=1088~1088g src=320
	{"e_smelt", 2}:           20.0066,   // n=1 W=76~76g src=320
	{"ed_crab", 3}:           99.9988,   // n=2 W=2123~2123g src=320
	{"fhcatfish", 2}:         1.199933,  // n=3 W=22275~22275g src=320
	{"freshwater_mussel", 2}: 2.0037,    // n=1 W=135~135g src=320
	{"fwdrum", 2}:            1.3998,    // n=1 W=3215~3215g src=320
	{"g_carp", 2}:            0.624,     // n=5 W=4880~21742g src=310
	{"g_carp", 3}:            3.639973,  // n=2 W=30513~30513g src=320
	{"g_shad", 2}:            0.7202,    // n=1 W=831~831g src=320
	{"g_tench", 2}:           19.067,    // n=1 W=992~992g src=320
	{"gc_smelt", 1}:          0.1561,    // n=1 W=772~772g src=310
	{"gc_smelt", 2}:          0.333308,  // n=2 W=2225~6605g src=320 [加成嫌疑→低值]
	{"gorbusha", 2}:          13.0911,   // n=1 W=1487~1487g src=320
	{"gudgeon", 3}:           29.988,    // n=3 W=102~140g src=320
	{"halibut_w", 1}:         0.5468,    // n=1 W=4110~4110g src=320
	{"hi_utsri", 2}:          20,        // n=1 W=11017~11017g src=320
	{"hippoglos_p", 2}:       29.0752,   // n=1 W=2639~2639g src=320
	{"hs_bass", 2}:           3.0765,    // n=1 W=1890~1890g src=320
	{"karasekarp", 3}:        11.9996,   // n=2 W=3944~3944g src=320
	{"katran", 2}:            20.2496,   // n=1 W=3973~3973g src=320
	{"kohaku", 2}:            3.75,      // n=1 W=14135~14135g src=320
	{"kw_tench", 3}:          53.3334,   // n=2 W=4392~4392g src=320
	{"l_carp", 2}:            1.8199,    // n=1 W=18300~18300g src=320
	{"l_carp_ghost", 2}:      18.7196,   // n=1 W=7916~7916g src=320
	{"l_minnow", 2}:          2.5,       // n=1 W=43~43g src=320
	{"lamna_n", 2}:           4.1304,    // n=1 W=33167~33167g src=320
	{"lenok_o", 2}:           6.9996,    // n=1 W=1403~1403g src=320
	{"lh_kambala", 2}:        9.9991,    // n=1 W=2904~2904g src=320
	{"lm_b_bass", 2}:         3.1997,    // n=1 W=2456~2456g src=320
	{"m_carp", 1}:            0.1561,    // n=1 W=3494~3494g src=310
	{"m_carp", 2}:            0.5201,    // n=1 W=4756~4756g src=310
	{"m_carp", 3}:            2.399956,  // n=2 W=25189~25189g src=320
	{"m_carp_ghost", 2}:      6.2399,    // n=1 W=13290~13290g src=320
	{"m_goshiki", 2}:         16.0001,   // n=1 W=8694~8694g src=320
	{"m_molva", 2}:           6.4996,    // n=1 W=5003~5003g src=320
	{"m_scorpius", 1}:        0.446341,  // n=4 W=205~585g src=310 [加成嫌疑→低值]
	{"m_scorpius", 2}:        1.1992,    // n=1 W=1142~1142g src=310
	{"m_scorpius", 3}:        7.5,       // n=2 W=3403~3403g src=320
	{"m_zubatka", 2}:         12.8571,   // n=1 W=12416~12416g src=320
	{"makrel", 2}:            3.0003,    // n=1 W=1891~1891g src=310
	{"makrel", 3}:            32.1425,   // n=2 W=2059~2059g src=320
	{"meagre", 2}:            12.272645, // n=3 W=34930~34930g src=320
	{"menek", 2}:             0.599886,  // n=2 W=6117~13652g src=310,320 [加成嫌疑→低值]
	{"merlang", 1}:           6.8988,    // n=1 W=252~252g src=310
	{"merlang", 2}:           23.0002,   // n=1 W=2130~2130g src=320
	{"merluza", 2}:           2.8,       // n=1 W=8367~8367g src=320
	{"molva", 2}:             6.1109,    // n=1 W=3763~3763g src=320
	{"mytilus_ed", 2}:        80.0075,   // n=1 W=67~67g src=320
	{"n.pike", 2}:            0.333217,  // n=2 W=4305~11164g src=310,320 [加成嫌疑→低值]
	{"n_asagi", 2}:           9.5,       // n=1 W=11599~11599g src=320
	{"n_carp", 1}:            0.2501,    // n=1 W=4405~4405g src=310
	{"n_carp", 2}:            1.666643,  // n=1 W=21513~21513g src=320
	{"o_ogon", 2}:            2.75,      // n=1 W=20322~20322g src=320
	{"os_grayling", 2}:       5.0658,    // n=1 W=691~691g src=320
	{"osetr_ship", 1}:        1.625,     // n=1 W=8419~8419g src=320
	{"ostrea_ed", 1}:         179.9118,  // n=1 W=51~51g src=320
	{"p_sturgeon", 2}:        3.119948,  // n=1 W=16553~16553g src=320
	{"p_zubatka", 2}:         6.7499,    // n=1 W=13459~13459g src=320
	{"peled", 2}:             1.9991,    // n=1 W=1637~1637g src=320
	{"perch", 1}:             0.125,     // n=4 W=100~340g src=310 [加成嫌疑→低值]
	{"perch", 3}:             1.7144,    // n=2 W=1665~1665g src=320
	{"piksha", 2}:            0.8382,    // n=1 W=2975~2975g src=310
	{"piksha", 3}:            10.0588,   // n=2 W=11714~11714g src=320
	{"pinagor", 2}:           13.1575,   // n=1 W=2441~2441g src=320
	{"pollachius", 2}:        2.916422,  // n=2 W=2387~10506g src=310,320 [加成嫌疑→低值]
	{"polyprion", 2}:         3,         // n=1 W=23470~23470g src=320
	{"poutassou", 2}:         10.5,      // n=1 W=591~591g src=320
	{"pumps_sunfish", 2}:     14.994,    // n=1 W=252~252g src=320
	{"r_carp", 2}:            1.456,     // n=1 W=21793~21793g src=320
	{"r_sturgeon", 2}:        1.307663,  // n=1 W=17043~17043g src=320
	{"red_fish", 3}:          17.5,      // n=2 W=3207~3207g src=320
	{"resfish", 2}:           1.8616,    // n=1 W=1348~1348g src=320
	{"ripus", 3}:             6.25,      // n=2 W=1242~1242g src=320
	{"riv_brown_trout", 2}:   3.214,     // n=1 W=1152~1152g src=320
	{"riv_rb_trout", 2}:      2.5,       // n=1 W=6697~6697g src=320
	{"rudd", 2}:              1.248,     // n=1 W=252~252g src=320
	{"ruffe", 3}:             11.996552, // n=4 W=145~168g src=320 [加成嫌疑→低值]
	{"ruffe_n", 2}:           44.4398,   // n=1 W=133~133g src=320
	{"s.bream", 3}:           3.214,     // n=2 W=1077~1077g src=320
	{"s.orfe", 2}:            0.8002,    // n=1 W=1739~1739g src=320
	{"s_carp", 3}:            6.76,      // n=2 W=31632~31632g src=320
	{"s_freaks", 3}:          3.999984,  // n=3 W=31717~31717g src=320
	{"s_saurus", 3}:          170.9928,  // n=2 W=627~627g src=320
	{"sabrefish", 2}:         0.8994,    // n=1 W=721~721g src=320
	{"sardina_p", 2}:         134.9637,  // n=1 W=124~124g src=320
	{"sea_kambala", 3}:       21.4282,   // n=2 W=4548~4548g src=320
	{"sebastes_m", 2}:        8.888,     // n=1 W=1344~1344g src=320
	{"sevan_trout", 2}:       1.9443,    // n=1 W=2342~2342g src=320
	{"sh_barbel", 2}:         6.8095,    // n=1 W=9752~9752g src=320
	{"shemaya_cha", 2}:       10.935897, // n=3 W=117~419g src=310,320 [加成嫌疑→低值]
	{"sig", 1}:               9.3709,    // n=1 W=368~368g src=320
	{"sig_ludoga", 2}:        3.3337,    // n=1 W=1461~1461g src=320
	{"sig_p", 2}:             1.8854,    // n=1 W=1086~1086g src=320
	{"sig_vouksa", 2}:        6.25,      // n=1 W=1194~1194g src=320
	{"sm_bass", 2}:           2.5449,    // n=1 W=2049~2049g src=320
	{"sp_bass", 2}:           3.8997,    // n=1 W=1391~1391g src=320
	{"st_sturgeon", 2}:       1.375,     // n=1 W=11855~11855g src=320
	{"sterlet", 2}:           12.9998,   // n=1 W=8346~8346g src=320
	{"syb_vendace", 2}:       3.0791,    // n=1 W=158~158g src=320
	{"taimen", 1}:            0.9068,    // n=1 W=574~574g src=320
	{"taran", 2}:             2.3748,    // n=1 W=679~679g src=320
	{"tench", 4}:             2.6667,    // n=2 W=6782~6782g src=320
	{"tiulka", 1}:            60.0417,   // n=1 W=12~12g src=320
	{"turbot", 2}:            4.8998,    // n=1 W=11003~11003g src=320
	{"valyok", 1}:            6.9975,    // n=1 W=197~197g src=320
	{"vendace", 2}:           3.9969,    // n=1 W=488~488g src=320
	{"wa_bream", 2}:          3.2702,    // n=1 W=531~531g src=320
	{"walleye", 2}:           4.1738,    // n=1 W=4053~4053g src=320
	{"warm", 2}:              1.0351,    // n=1 W=584~584g src=320
	{"wh_bass", 2}:           0.7258,    // n=1 W=1592~1592g src=320
	{"white_crappie", 2}:     1.5476,    // n=1 W=1239~1239g src=320
	{"yotsushiro", 2}:        12.5001,   // n=1 W=5352~5352g src=320
	{"zoarces_v", 3}:         146.6671,  // n=2 W=1065~1065g src=320
	{"zs_grayling", 2}:       5.0657,    // n=1 W=830~830g src=320
}

// fishXPAnchor 同种锚：该种实测样本最多的档。缺档 c = 锚 c × FACTOR[g] / FACTOR[锚档]。
var fishXPAnchor = map[string]fishXPAnchorEntry{
	"a.sleeper":         {grade: 2, c: 1.125},
	"a_char":            {grade: 1, c: 0.5},
	"a_grayling":        {grade: 2, c: 2.8565},
	"a_omul":            {grade: 1, c: 1.5989},
	"ambl_r":            {grade: 2, c: 11.1107},
	"asp":               {grade: 1, c: 0.8429},
	"atl_cod":           {grade: 1, c: 0.524495},
	"atl_herring":       {grade: 2, c: 32.499107},
	"b_bream":           {grade: 2, c: 10.0013},
	"b_carp":            {grade: 3, c: 12.999953},
	"b_gill":            {grade: 3, c: 1.3633},
	"barsch":            {grade: 2, c: 3.1996},
	"bh_carp":           {grade: 3, c: 4.3333},
	"bl_buffalo":        {grade: 2, c: 10.3995},
	"black_crappie":     {grade: 2, c: 3.9948},
	"bream":             {grade: 3, c: 1.4999},
	"buffalo":           {grade: 2, c: 0.936},
	"burbot":            {grade: 2, c: 1.3},
	"c.carp":            {grade: 3, c: 0.5714},
	"c.roach":           {grade: 3, c: 2.9988},
	"c_barbel":          {grade: 1, c: 0.5335},
	"c_bleak":           {grade: 2, c: 11.245},
	"c_nase":            {grade: 2, c: 4.6875},
	"c_squid":           {grade: 2, c: 75.0007},
	"carp":              {grade: 2, c: 0.52},
	"carp_ghost":        {grade: 2, c: 3.574895},
	"ch_catfish":        {grade: 2, c: 0.4229},
	"cha_herring":       {grade: 2, c: 10.9065},
	"chartus_kuoriius":  {grade: 2, c: 2.25},
	"chir":              {grade: 2, c: 1.2998},
	"chlamys_isl":       {grade: 2, c: 83.3221},
	"coalfish":          {grade: 1, c: 0.22933},
	"common_carp":       {grade: 3, c: 6.239969},
	"conger":            {grade: 1, c: 2.5},
	"crawfish":          {grade: 2, c: 10.0027},
	"crucian":           {grade: 3, c: 0.5501},
	"dace":              {grade: 3, c: 13.8887},
	"din_l_carp":        {grade: 1, c: 0.5834},
	"din_m_carp":        {grade: 2, c: 1.3333},
	"din_n_carp":        {grade: 2, c: 6.1667},
	"dreissena":         {grade: 2, c: 5.6026},
	"e.chub":            {grade: 2, c: 0.6997},
	"e.pikeperch":       {grade: 2, c: 1.625},
	"e_bream":           {grade: 2, c: 1.3001},
	"e_smelt":           {grade: 2, c: 20.0066},
	"ed_crab":           {grade: 3, c: 99.9988},
	"fhcatfish":         {grade: 2, c: 1.199933},
	"freshwater_mussel": {grade: 2, c: 2.0037},
	"fwdrum":            {grade: 2, c: 1.3998},
	"g_carp":            {grade: 2, c: 0.624},
	"g_shad":            {grade: 2, c: 0.7202},
	"g_tench":           {grade: 2, c: 19.067},
	"gc_smelt":          {grade: 2, c: 0.333308},
	"gorbusha":          {grade: 2, c: 13.0911},
	"gudgeon":           {grade: 3, c: 29.988},
	"halibut_w":         {grade: 1, c: 0.5468},
	"hi_utsri":          {grade: 2, c: 20},
	"hippoglos_p":       {grade: 2, c: 29.0752},
	"hs_bass":           {grade: 2, c: 3.0765},
	"karasekarp":        {grade: 3, c: 11.9996},
	"katran":            {grade: 2, c: 20.2496},
	"kohaku":            {grade: 2, c: 3.75},
	"kw_tench":          {grade: 3, c: 53.3334},
	"l_carp":            {grade: 2, c: 1.8199},
	"l_carp_ghost":      {grade: 2, c: 18.7196},
	"l_minnow":          {grade: 2, c: 2.5},
	"lamna_n":           {grade: 2, c: 4.1304},
	"lenok_o":           {grade: 2, c: 6.9996},
	"lh_kambala":        {grade: 2, c: 9.9991},
	"lm_b_bass":         {grade: 2, c: 3.1997},
	"m_carp":            {grade: 3, c: 2.399956},
	"m_carp_ghost":      {grade: 2, c: 6.2399},
	"m_goshiki":         {grade: 2, c: 16.0001},
	"m_molva":           {grade: 2, c: 6.4996},
	"m_scorpius":        {grade: 1, c: 0.446341},
	"m_zubatka":         {grade: 2, c: 12.8571},
	"makrel":            {grade: 3, c: 32.1425},
	"meagre":            {grade: 2, c: 12.272645},
	"menek":             {grade: 2, c: 0.599886},
	"merlang":           {grade: 1, c: 6.8988},
	"merluza":           {grade: 2, c: 2.8},
	"molva":             {grade: 2, c: 6.1109},
	"mytilus_ed":        {grade: 2, c: 80.0075},
	"n.pike":            {grade: 2, c: 0.333217},
	"n_asagi":           {grade: 2, c: 9.5},
	"n_carp":            {grade: 1, c: 0.2501},
	"o_ogon":            {grade: 2, c: 2.75},
	"os_grayling":       {grade: 2, c: 5.0658},
	"osetr_ship":        {grade: 1, c: 1.625},
	"ostrea_ed":         {grade: 1, c: 179.9118},
	"p_sturgeon":        {grade: 2, c: 3.119948},
	"p_zubatka":         {grade: 2, c: 6.7499},
	"peled":             {grade: 2, c: 1.9991},
	"perch":             {grade: 1, c: 0.125},
	"piksha":            {grade: 3, c: 10.0588},
	"pinagor":           {grade: 2, c: 13.1575},
	"pollachius":        {grade: 2, c: 2.916422},
	"polyprion":         {grade: 2, c: 3},
	"poutassou":         {grade: 2, c: 10.5},
	"pumps_sunfish":     {grade: 2, c: 14.994},
	"r_carp":            {grade: 2, c: 1.456},
	"r_sturgeon":        {grade: 2, c: 1.307663},
	"red_fish":          {grade: 3, c: 17.5},
	"resfish":           {grade: 2, c: 1.8616},
	"ripus":             {grade: 3, c: 6.25},
	"riv_brown_trout":   {grade: 2, c: 3.214},
	"riv_rb_trout":      {grade: 2, c: 2.5},
	"rudd":              {grade: 2, c: 1.248},
	"ruffe":             {grade: 3, c: 11.996552},
	"ruffe_n":           {grade: 2, c: 44.4398},
	"s.bream":           {grade: 3, c: 3.214},
	"s.orfe":            {grade: 2, c: 0.8002},
	"s_carp":            {grade: 3, c: 6.76},
	"s_freaks":          {grade: 3, c: 3.999984},
	"s_saurus":          {grade: 3, c: 170.9928},
	"sabrefish":         {grade: 2, c: 0.8994},
	"sardina_p":         {grade: 2, c: 134.9637},
	"sea_kambala":       {grade: 3, c: 21.4282},
	"sebastes_m":        {grade: 2, c: 8.888},
	"sevan_trout":       {grade: 2, c: 1.9443},
	"sh_barbel":         {grade: 2, c: 6.8095},
	"shemaya_cha":       {grade: 2, c: 10.935897},
	"sig":               {grade: 1, c: 9.3709},
	"sig_ludoga":        {grade: 2, c: 3.3337},
	"sig_p":             {grade: 2, c: 1.8854},
	"sig_vouksa":        {grade: 2, c: 6.25},
	"sm_bass":           {grade: 2, c: 2.5449},
	"sp_bass":           {grade: 2, c: 3.8997},
	"st_sturgeon":       {grade: 2, c: 1.375},
	"sterlet":           {grade: 2, c: 12.9998},
	"syb_vendace":       {grade: 2, c: 3.0791},
	"taimen":            {grade: 1, c: 0.9068},
	"taran":             {grade: 2, c: 2.3748},
	"tench":             {grade: 4, c: 2.6667},
	"tiulka":            {grade: 1, c: 60.0417},
	"turbot":            {grade: 2, c: 4.8998},
	"valyok":            {grade: 1, c: 6.9975},
	"vendace":           {grade: 2, c: 3.9969},
	"wa_bream":          {grade: 2, c: 3.2702},
	"walleye":           {grade: 2, c: 4.1738},
	"warm":              {grade: 2, c: 1.0351},
	"wh_bass":           {grade: 2, c: 0.7258},
	"white_crappie":     {grade: 2, c: 1.5476},
	"yotsushiro":        {grade: 2, c: 12.5001},
	"zoarces_v":         {grade: 3, c: 146.6671},
	"zs_grayling":       {grade: 2, c: 5.0657},
}

// fishXPAnchorEntry 同种锚条目。
type fishXPAnchorEntry struct {
	grade int64
	c     float64
}

// fishXPGradeFactor 档位乘子（相对 g2），多种实测比例取中位。
// g1=0.3063（观测: a_char=0.43, atl_cod=0.17, carp=0.31, coalfish=0.49, din_l_carp=0.19, gc_smelt=0.47, m_carp=0.30, m_scorpius=0.37, merlang=0.30, n_carp=0.15）；g3=6.25（观测: b_carp=6.25, c.carp=3.08, c.roach=9.97, carp=6.25, crucian=4.00, g_carp=5.83, m_carp=4.61, m_scorpius=6.25, makrel=10.71, piksha=12.00）。
// 无 g4 观测，沿用 g3 乘子（保守，spec 开放项）

var fishXPGradeFactor = [4]float64{
	0.3063, 1, 6.25, 6.25,
}

// fishXPGroupC 亲缘组锚，仅零实测种兜底：组内实测成员按档中位，缺档=锚×乘子外推。
var fishXPGroupC = map[string][4]float64{
	"apex":           {3.734, 12.19, 76.187, 76.187},          // 实测成员 2/7，g1=外推,g3=外推,g4=外推
	"carp_mid":       {0.5335, 7.8333, 48.9581, 2.6667},       // 实测成员 12/12，g3=外推
	"catfish":        {0.3676, 1.1999, 7.4995, 7.4995},        // 实测成员 3/6，g1=外推,g3=外推,g4=外推
	"flatfish":       {0.5468, 9.9991, 21.4282, 62.494},       // 实测成员 5/6，g4=外推
	"herb_carp":      {1.7609, 5.7485, 5.5466, 35.9279},       // 实测成员 6/8，g1=外推,g4=外推
	"marine_gadid":   {1.9909, 6.4996, 40.6222, 40.6222},      // 实测成员 5/6，g1=外推,g3=外推,g4=外推
	"marine_pelagic": {41.3417, 134.9637, 170.9928, 843.5178}, // 实测成员 2/9，g1=外推,g4=外推
	"marine_rock":    {2.5, 11.1107, 99.9988, 69.4414},        // 实测成员 9/22，g4=外推
	"predator":       {0.8429, 3.1997, 11.9966, 19.9977},      // 实测成员 14/20，g4=外推
	"roach_run":      {0.2451, 0.8002, 13.8887, 5.0012},       // 实测成员 4/10，g1=外推,g4=外推
	"salmonid":       {1.2529, 2.9678, 6.25, 18.5486},         // 实测成员 23/54，g4=外推
	"small_forage":   {3.4471, 11.2533, 29.988, 70.3327},      // 实测成员 3/13，g1=外推,g4=外推
	"still_carp":     {1.0017, 3.2702, 6.7497, 20.4386},       // 实测成员 5/5，g1=外推,g4=外推
	"sturgeon":       {1.625, 2.2475, 14.0466, 14.0466},       // 实测成员 5/12，g3=外推,g4=外推
	"sunfish":        {0.5221, 1.7046, 10.6537, 10.6537},      // 实测成员 4/5，g1=外推,g3=外推,g4=外推
	"weak_silver":    {179.9118, 7.8026, 4, 48.7663},          // 实测成员 8/9，g4=外推
}

// fishXPRateFor 返回 XP 速率 c（XP = trunc(c×W_g)）。
// 优先级：本种实测精确命中 → 同种锚×档位乘子 → 皮肤别名→宿主整链 → 亲缘组 → 旧 valueRates → 缺省。
// 注意：皮肤变种（common_carp 等）官服有独立经济数值，实测/锚优先于宿主继承；
// 别名仅兜零实测的皮肤种。
func fishXPRateFor(species string, grade int64) float64 {
	if grade < 1 {
		grade = 1
	} else if grade > 4 {
		grade = 4
	}
	if c, ok := fishXPRateC[vgKey{species: species, grade: grade}]; ok {
		return c
	}
	if a, ok := fishXPAnchor[species]; ok {
		return a.c * fishXPGradeFactor[grade-1] / fishXPGradeFactor[a.grade-1]
	}
	if host, ok := fishPullAlias[species]; ok {
		return fishXPRateFor(host, grade)
	}
	if grp, ok := fishPullSpeciesGroup[species]; ok {
		if gv, ok2 := fishXPGroupC[grp]; ok2 {
			return gv[grade-1]
		}
	}
	if rate, ok := valueRates[vgKey{species: species, grade: grade}]; ok && rate[0] > 0 {
		return rate[0]
	}
	return fishXPRateFallback
}

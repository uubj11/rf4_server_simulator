package gameserver

import (
	"math"
	"testing"
)

// 官服锚点（仅数值，来源见 fish_xp_rate_table.go 头注释）。
// XP = trunc(fishXPRateFor(species,grade) × W)，逐条精确断言。
var officialXPAnchors = []struct {
	species string
	grade   int64
	weightG int64
	xp      int64
}{
	{"a.sleeper", 2, 396, 445},           // 320
	{"a_char", 1, 415, 207},              // 310
	{"a_char", 2, 8916, 10253},           // 320
	{"a_grayling", 2, 1397, 3990},        // 320
	{"a_omul", 1, 283, 452},              // 320
	{"ambl_r", 2, 4567, 50742},           // 320
	{"asp", 1, 1614, 1360},               // 320
	{"atl_cod", 1, 2674, 1402},           // 310
	{"atl_cod", 2, 30676, 96628},         // 320
	{"atl_herring", 2, 560, 18199},       // 310
	{"b_bream", 2, 377, 3770},            // 320
	{"b_carp", 2, 7828, 16282},           // 310
	{"b_carp", 3, 31932, 415114},         // 320
	{"b_carp", 3, 31932, 415114},         // 320
	{"b_gill", 1, 113, 20},               // 310
	{"b_gill", 3, 1383, 1885},            // 320
	{"b_gill", 3, 1383, 1885},            // 320
	{"barsch", 2, 714, 2284},             // 320
	{"bh_carp", 3, 37894, 164206},        // 320
	{"bh_carp", 3, 37894, 164206},        // 320
	{"bh_carp", 3, 37894, 164206},        // 320
	{"bh_carp", 3, 37894, 164206},        // 320
	{"bl_buffalo", 2, 3235, 33642},       // 320
	{"black_crappie", 2, 668, 2668},      // 320
	{"bream", 3, 4414, 6620},             // 320
	{"bream", 3, 4414, 6620},             // 320
	{"buffalo", 2, 14669, 13730},         // 320
	{"burbot", 2, 4571, 5942},            // 320
	{"c.carp", 2, 1574, 292},             // 310
	{"c.carp", 3, 2171, 1240},            // 320
	{"c.carp", 3, 2439, 1393},            // 320
	{"c.carp", 3, 2439, 1393},            // 320
	{"c.roach", 2, 680, 204},             // 310
	{"c.roach", 3, 1290, 3868},           // 320
	{"c.roach", 3, 1290, 3868},           // 320
	{"c_barbel", 1, 1478, 788},           // 320
	{"c_bleak", 2, 100, 1124},            // 320
	{"c_nase", 2, 632, 2962},             // 320
	{"c_squid", 2, 672, 50400},           // 320
	{"carp", 1, 2434, 395},               // 310
	{"carp", 2, 4137, 2151},              // 310
	{"carp", 2, 4589, 2386},              // 310
	{"carp", 2, 4765, 2477},              // 310
	{"carp", 2, 4790, 2490},              // 310
	{"carp", 2, 5710, 2969},              // 310
	{"carp", 3, 25665, 83410},            // 320
	{"carp", 3, 25665, 83410},            // 320
	{"carp_ghost", 2, 4513, 16133},       // 310
	{"ch_catfish", 2, 1997, 844},         // 320
	{"cha_herring", 2, 230, 2508},        // 320
	{"chartus_kuoriius", 2, 6490, 14602}, // 320
	{"chir", 2, 1256, 1632},              // 320
	{"chlamys_isl", 2, 104, 8665},        // 320
	{"coalfish", 1, 702, 160},            // 310
	{"coalfish", 1, 1692, 388},           // 310
	{"coalfish", 2, 3027, 1417},          // 310
	{"common_carp", 3, 25197, 157228},    // 320
	{"common_carp", 3, 25197, 157228},    // 320
	{"conger", 1, 3681, 9202},            // 320
	{"crawfish", 2, 186, 1860},           // 320
	{"crucian", 2, 527, 72},              // 310
	{"crucian", 3, 2299, 1264},           // 320
	{"crucian", 3, 2686, 1477},           // 320
	{"crucian", 3, 2686, 1477},           // 320
	{"dace", 3, 265, 3680},               // 320
	{"dace", 3, 265, 3680},               // 320
	{"din_l_carp", 1, 1715, 1000},        // 310
	{"din_l_carp", 2, 26479, 79436},      // 320
	{"din_m_carp", 2, 29005, 38672},      // 320
	{"din_n_carp", 2, 18864, 116328},     // 320
	{"dreissena", 2, 117, 655},           // 320
	{"e.chub", 2, 3482, 2436},            // 320
	{"e.pikeperch", 2, 2783, 4522},       // 320
	{"e_bream", 2, 1088, 1414},           // 320
	{"e_smelt", 2, 76, 1520},             // 320
	{"ed_crab", 3, 2123, 212297},         // 320
	{"ed_crab", 3, 2123, 212297},         // 320
	{"fhcatfish", 2, 22275, 26728},       // 320
	{"fhcatfish", 2, 22275, 26728},       // 320
	{"fhcatfish", 2, 22275, 26728},       // 320
	{"freshwater_mussel", 2, 135, 270},   // 320
	{"fwdrum", 2, 3215, 4500},            // 320
	{"g_carp", 2, 4880, 3045},            // 310
	{"g_carp", 2, 5484, 3422},            // 310
	{"g_carp", 2, 5716, 3566},            // 310
	{"g_carp", 2, 10307, 6431},           // 310
	{"g_carp", 2, 21742, 13567},          // 310
	{"g_carp", 3, 30513, 111066},         // 320
	{"g_carp", 3, 30513, 111066},         // 320
	{"g_shad", 2, 831, 598},              // 320
	{"g_tench", 2, 992, 18914},           // 320
	{"gc_smelt", 1, 772, 120},            // 310
	{"gc_smelt", 2, 6605, 2201},          // 320
	{"gorbusha", 2, 1487, 19466},         // 320
	{"gudgeon", 3, 102, 3058},            // 320
	{"gudgeon", 3, 140, 4198},            // 320
	{"gudgeon", 3, 140, 4198},            // 320
	{"halibut_w", 1, 4110, 2247},         // 320
	{"hi_utsri", 2, 11017, 220340},       // 320
	{"hippoglos_p", 2, 2639, 76729},      // 320
	{"hs_bass", 2, 1890, 5814},           // 320
	{"karasekarp", 3, 3944, 47326},       // 320
	{"karasekarp", 3, 3944, 47326},       // 320
	{"katran", 2, 3973, 80451},           // 320
	{"kohaku", 2, 14135, 53006},          // 320
	{"kw_tench", 3, 4392, 234240},        // 320
	{"kw_tench", 3, 4392, 234240},        // 320
	{"l_carp", 2, 18300, 33304},          // 320
	{"l_carp_ghost", 2, 7916, 148184},    // 320
	{"l_minnow", 2, 43, 107},             // 320
	{"lamna_n", 2, 33167, 136992},        // 320
	{"lenok_o", 2, 1403, 9820},           // 320
	{"lh_kambala", 2, 2904, 29037},       // 320
	{"lm_b_bass", 2, 2456, 7858},         // 320
	{"m_carp", 1, 3494, 545},             // 310
	{"m_carp", 2, 4756, 2473},            // 310
	{"m_carp", 3, 25189, 60452},          // 320
	{"m_carp", 3, 25189, 60452},          // 320
	{"m_carp_ghost", 2, 13290, 82928},    // 320
	{"m_goshiki", 2, 8694, 139104},       // 320
	{"m_molva", 2, 5003, 32517},          // 320
	{"m_scorpius", 1, 205, 91},           // 310
	{"m_scorpius", 2, 1142, 1369},        // 310
	{"m_scorpius", 3, 3403, 25522},       // 320
	{"m_scorpius", 3, 3403, 25522},       // 320
	{"m_zubatka", 2, 12416, 159633},      // 320
	{"makrel", 2, 1891, 5673},            // 310
	{"makrel", 3, 2059, 66181},           // 320
	{"makrel", 3, 2059, 66181},           // 320
	{"meagre", 2, 34930, 428683},         // 320
	{"meagre", 2, 34930, 428683},         // 320
	{"meagre", 2, 34930, 428683},         // 320
	{"menek", 2, 6117, 3669},             // 310
	{"merlang", 1, 252, 1738},            // 310
	{"merlang", 2, 2130, 48990},          // 320
	{"merluza", 2, 8367, 23427},          // 320
	{"molva", 2, 3763, 22995},            // 320
	{"mytilus_ed", 2, 67, 5360},          // 320
	{"n.pike", 2, 4305, 1434},            // 310
	{"n_asagi", 2, 11599, 110190},        // 320
	{"n_carp", 1, 4405, 1101},            // 310
	{"n_carp", 2, 21513, 35854},          // 320
	{"o_ogon", 2, 20322, 55885},          // 320
	{"os_grayling", 2, 691, 3500},        // 320
	{"osetr_ship", 1, 8419, 13680},       // 320
	{"ostrea_ed", 1, 51, 9175},           // 320
	{"p_sturgeon", 2, 16553, 51644},      // 320
	{"p_zubatka", 2, 13459, 90846},       // 320
	{"peled", 2, 1637, 3272},             // 320
	{"perch", 1, 100, 12},                // 310
	{"perch", 3, 1665, 2854},             // 320
	{"perch", 3, 1665, 2854},             // 320
	{"piksha", 2, 2975, 2493},            // 310
	{"piksha", 3, 11714, 117828},         // 320
	{"piksha", 3, 11714, 117828},         // 320
	{"pinagor", 2, 2441, 32117},          // 320
	{"pollachius", 2, 2387, 6961},        // 310
	{"polyprion", 2, 23470, 70410},       // 320
	{"poutassou", 2, 591, 6205},          // 320
	{"pumps_sunfish", 2, 252, 3778},      // 320
	{"r_carp", 2, 21793, 31730},          // 320
	{"r_sturgeon", 2, 17043, 22286},      // 320
	{"red_fish", 3, 3207, 56122},         // 320
	{"red_fish", 3, 3207, 56122},         // 320
	{"resfish", 2, 1348, 2509},           // 320
	{"ripus", 3, 1242, 7762},             // 320
	{"ripus", 3, 1242, 7762},             // 320
	{"riv_brown_trout", 2, 1152, 3702},   // 320
	{"riv_rb_trout", 2, 6697, 16742},     // 320
	{"rudd", 2, 252, 314},                // 320
	{"ruffe", 3, 145, 1739},              // 320
	{"ruffe_n", 2, 133, 5910},            // 320
	{"s.bream", 3, 1077, 3461},           // 320
	{"s.bream", 3, 1077, 3461},           // 320
	{"s.orfe", 2, 1739, 1391},            // 320
	{"s_carp", 3, 31632, 213832},         // 320
	{"s_carp", 3, 31632, 213832},         // 320
	{"s_freaks", 3, 31717, 126867},       // 320
	{"s_freaks", 3, 31717, 126867},       // 320
	{"s_freaks", 3, 31717, 126867},       // 320
	{"s_saurus", 3, 627, 107212},         // 320
	{"s_saurus", 3, 627, 107212},         // 320
	{"sabrefish", 2, 721, 648},           // 320
	{"sardina_p", 2, 124, 16735},         // 320
	{"sea_kambala", 3, 4548, 97455},      // 320
	{"sea_kambala", 3, 4548, 97455},      // 320
	{"sebastes_m", 2, 1344, 11945},       // 320
	{"sevan_trout", 2, 2342, 4553},       // 320
	{"sh_barbel", 2, 9752, 66406},        // 320
	{"shemaya_cha", 2, 117, 1279},        // 310
	{"sig", 1, 368, 3448},                // 320
	{"sig_ludoga", 2, 1461, 4870},        // 320
	{"sig_p", 2, 1086, 2047},             // 320
	{"sig_vouksa", 2, 1194, 7462},        // 320
	{"sm_bass", 2, 2049, 5214},           // 320
	{"sp_bass", 2, 1391, 5424},           // 320
	{"st_sturgeon", 2, 11855, 16300},     // 320
	{"sterlet", 2, 8346, 108496},         // 320
	{"syb_vendace", 2, 158, 486},         // 320
	{"taimen", 1, 574, 520},              // 320
	{"taran", 2, 679, 1612},              // 320
	{"tench", 4, 6782, 18085},            // 320
	{"tench", 4, 6782, 18085},            // 320
	{"tiulka", 1, 12, 720},               // 320
	{"turbot", 2, 11003, 53912},          // 320
	{"valyok", 1, 197, 1378},             // 320
	{"vendace", 2, 488, 1950},            // 320
	{"wa_bream", 2, 531, 1736},           // 320
	{"walleye", 2, 4053, 16916},          // 320
	{"warm", 2, 584, 604},                // 320
	{"wh_bass", 2, 1592, 1155},           // 320
	{"white_crappie", 2, 1239, 1917},     // 320
	{"yotsushiro", 2, 5352, 66900},       // 320
	{"zoarces_v", 3, 1065, 156200},       // 320
	{"zoarces_v", 3, 1065, 156200},       // 320
	{"zs_grayling", 2, 830, 4204},        // 320
}

func TestOfficialXPAnchors(t *testing.T) {
	for _, a := range officialXPAnchors {
		rate := fishXPRateFor(a.species, a.grade)
		got := int64(math.Max(1, math.Trunc(rate*float64(a.weightG))))
		if got != a.xp {
			t.Errorf("%s g%d %dg: XP=%d, want %d (rate=%f)", a.species, a.grade, a.weightG, got, a.xp, rate)
		}
	}
}

func TestXPSingleAnchorNotGroup(t *testing.T) {
	// a.sleeper 仅实测 g2；缺档 g3 必须用本种锚×乘子，不得落亲缘组。
	got := fishXPRateFor("a.sleeper", 3)
	want := 1.125 * fishXPGradeFactor[2] / fishXPGradeFactor[1]
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %f, want %f", got, want)
	}
	if gv, ok := fishXPGroupC["predator"]; ok && math.Abs(got-gv[2]) < 1e-9 {
		t.Fatalf("单点种落到了组值 %f", gv)
	}
}

func TestXPGroupFallback(t *testing.T) {
	// macrourus_b 零实测 → 组 marine_gadid。
	if got, want := fishXPRateFor("macrourus_b", 2), fishXPGroupC["marine_gadid"][1]; got != want {
		t.Fatalf("got %f, want %f", got, want)
	}
}

func TestXPAliasInheritsHost(t *testing.T) {
	// carp_albino(皮肤) 继承宿主 carp 的实测 c。
	if host, ok := fishXPAnchor["carp"]; ok {
		if got := fishXPRateFor("carp_albino", host.grade); got != host.c {
			t.Fatalf("got %f, want %f", got, host.c)
		}
	}
}

func TestXPCoverage(t *testing.T) {
	// fishPullSpeciesGroup 全部键 + 别名键 × 4 档，均不得落缺省。
	check := func(sp string, t *testing.T) {
		for g := int64(1); g <= 4; g++ {
			if r := fishXPRateFor(sp, g); r == fishXPRateFallback {
				t.Errorf("%s g%d 落缺省 0.4", sp, g)
			}
		}
	}
	for sp := range fishPullSpeciesGroup {
		check(sp, t)
	}
	for sp := range fishPullAlias {
		check(sp, t)
	}
	for sp := range fishXPAnchor {
		check(sp, t)
	}
}

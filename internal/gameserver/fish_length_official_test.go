package gameserver

import (
	"math"
	"testing"
)

func lengthPred(sp string, wG float64) float64 {
	k, p := calibratedLengthKP(sp, defaultSpeciesLengthK, defaultSpeciesLengthP)
	return speciesLengthM(sp, k, p, wG)
}

// 多点拟合锚点：端点 ±tol%（tol=官服体况抖动容差，取 max(6%, 最大残差+1%)）。
var officialLengthFitAnchors = []struct {
	species string
	wG      float64
	lM      float64
	tolPct  float64
}{
	{"atl_herring", 560, 0.3259, 6},
	{"atl_herring", 637, 0.3434, 6},
	{"b_carp", 7828, 0.6957, 6},
	{"b_carp", 31932, 1.2819, 6},
	{"b_gill", 113, 0.1759, 8},
	{"b_gill", 1383, 0.4483, 8},
	{"c.carp", 631, 0.307, 8},
	{"c.carp", 2439, 0.4872, 8},
	{"c.roach", 680, 0.2938, 6},
	{"c.roach", 1290, 0.3501, 6},
	{"carp", 1063, 0.3029, 12},
	{"carp", 25665, 1.12, 12},
	{"carp_ghost", 4513, 0.5218, 9},
	{"carp_ghost", 12632, 0.7463, 9},
	{"coalfish", 702, 0.216, 21},
	{"coalfish", 17823, 1.0985, 21},
	{"crucian", 293, 0.2519, 12},
	{"crucian", 2686, 0.5289, 12},
	{"g_carp", 4880, 0.622, 8},
	{"g_carp", 30513, 1.2524, 8},
	{"gc_smelt", 772, 0.2786, 6},
	{"gc_smelt", 6605, 0.7575, 6},
	{"m_carp", 2312, 0.418, 7},
	{"m_carp", 25189, 1.1538, 7},
	{"m_scorpius", 205, 0.229, 6},
	{"m_scorpius", 3403, 0.5403, 6},
	{"menek", 6117, 0.7867, 6},
	{"menek", 13652, 1.1756, 6},
	{"n.pike", 4305, 0.6768, 6},
	{"n.pike", 11164, 1.0541, 6},
	{"n_carp", 4405, 0.5315, 7},
	{"n_carp", 21513, 1.0226, 7},
	{"perch", 93, 0.1744, 14},
	{"perch", 1665, 0.4259, 14},
	{"pollachius", 2387, 0.492, 6},
	{"pollachius", 10506, 0.9709, 6},
	{"rudd", 195, 0.22, 6},
	{"rudd", 402, 0.267, 6},
	{"shemaya_cha", 117, 0.2066, 6},
	{"shemaya_cha", 419, 0.2804, 6},
	{"tiulka", 10, 0.08, 7},
	{"tiulka", 13, 0.092, 7},
}

func TestOfficialLengthFitAnchors(t *testing.T) {
	for _, a := range officialLengthFitAnchors {
		got := lengthPred(a.species, a.wG)
		if math.Abs(got-a.lM)/a.lM*100 > a.tolPct {
			t.Errorf("%s %fg: L=%f, want %f (±%f%%)", a.species, a.wG, got, a.lM, a.tolPct)
		}
	}
}

// 单点/两点锚：calibratedLengthKP 返回本种实测对，且在该点精确复现。
var officialLengthSingleAnchors = []struct {
	species string
	k, p    float64
	wG      float64
	lM      float64
}{
	{"a.sleeper", 0.0366042, 0.33333333, 396, 0.2688},
	{"a_grayling", 0.03933305, 0.33333333, 1397, 0.4397},
	{"a_omul", 0.03250366, 0.33333333, 283, 0.2134},
	{"asp", 0.03633392, 0.33333333, 1614, 0.4262},
	{"b_bream", 0.03348539, 0.33333333, 377, 0.2419},
	{"barsch", 0.03612728, 0.33333333, 714, 0.3229},
	{"bh_carp", 0.04191618, 0.33333333, 37894, 1.4079},
	{"bl_buffalo", 0.03396292, 0.33333333, 3235, 0.5023},
	{"black_crappie", 0.03809361, 0.33333333, 668, 0.333},
	{"bream", 0.03662569, 0.33333333, 4414, 0.6008},
	{"buffalo", 0.04027882, 0.33333333, 14669, 0.986},
	{"burbot", 0.04438416, 0.33333333, 4571, 0.7366},
	{"c_barbel", 0.03479092, 0.33333333, 1478, 0.3963},
	{"c_nase", 0.03748683, 0.33333333, 632, 0.3217},
	{"c_squid", 0.04397743, 0.33333333, 672, 0.3852},
	{"ch_catfish", 0.04018135, 0.33333333, 1997, 0.506},
	{"chir", 0.03402419, 0.33333333, 1256, 0.3671},
	{"chlamys_isl", 0.02139211, 0.33333333, 104, 0.1006},
	{"crawfish", 0.03738437, 0.33333333, 186, 0.2134},
	{"dace", 0.03976237, 0.33333333, 265, 0.2554},
	{"din_m_carp", 0.03917014, 0.33333333, 29005, 1.2035},
	{"din_n_carp", 0.03589756, 0.33333333, 18864, 0.9556},
	{"dreissena", 0.02202016, 0.33333333, 117, 0.1077},
	{"e.chub", 0.03696013, 0.33333333, 3482, 0.5602},
	{"e.pikeperch", 0.03703252, 0.33333333, 2783, 0.5209},
	{"e_bream", 0.0346617, 0.33333333, 1088, 0.3565},
	{"e_smelt", 0.03597884, 0.33333333, 76, 0.1524},
	{"ed_crab", 0.02897519, 0.33333333, 2123, 0.3724},
	{"fhcatfish", 0.0492418, 0.33333333, 22275, 1.3855},
	{"freshwater_mussel", 0.02389897, 0.33333333, 135, 0.1226},
	{"fwdrum", 0.03657401, 0.33333333, 3215, 0.5398},
	{"g_shad", 0.03959977, 0.33333333, 831, 0.3723},
	{"g_tench", 0.03495346, 0.33333333, 992, 0.3486},
	{"gorbusha", 0.03486953, 0.33333333, 1487, 0.398},
	{"gudgeon", 0.04121185, 0.31995243, 140, 0.2003},
	{"hi_utsri", 0.03255547, 0.33333333, 11017, 0.7244},
	{"hippoglos_p", 0.03778828, 0.33333333, 2639, 0.5222},
	{"hs_bass", 0.03729419, 0.33333333, 1890, 0.4611},
	{"karasekarp", 0.03952636, 0.33333333, 3944, 0.6245},
	{"katran", 0.04646357, 0.33333333, 3973, 0.7359},
	{"kohaku", 0.03388528, 0.33333333, 14135, 0.8193},
	{"l_minnow", 0.03602217, 0.33333333, 43, 0.1262},
	{"lenok_o", 0.03270247, 0.33333333, 1403, 0.3661},
	{"lh_kambala", 0.03789869, 0.33333333, 2904, 0.5407},
	{"lm_b_bass", 0.03571748, 0.33333333, 2456, 0.4819},
	{"m_goshiki", 0.03179108, 0.33333333, 8694, 0.6537},
	{"merluza", 0.04922844, 0.33333333, 8367, 0.9994},
	{"mytilus_ed", 0.02343934, 0.33333333, 67, 0.0952},
	{"n_asagi", 0.03278794, 0.33333333, 11599, 0.7422},
	{"o_ogon", 0.03692687, 0.33333333, 20322, 1.0077},
	{"os_grayling", 0.03861632, 0.33333333, 691, 0.3414},
	{"ostrea_ed", 0.02176123, 0.33333333, 51, 0.0807},
	{"peled", 0.03417747, 0.33333333, 1637, 0.4028},
	{"pinagor", 0.0325969, 0.33333333, 2441, 0.4389},
	{"poutassou", 0.03978817, 0.33333333, 591, 0.3339},
	{"resfish", 0.03912504, 0.33333333, 1348, 0.4322},
	{"ripus", 0.03582611, 0.33333333, 1242, 0.3851},
	{"riv_brown_trout", 0.0381953, 0.33333333, 1152, 0.4004},
	{"riv_rb_trout", 0.0393438, 0.33333333, 6697, 0.7416},
	{"ruffe", 0.03854509, 0.33333333, 145, 0.2025},
	{"ruffe_n", 0.03626234, 0.33333333, 133, 0.1851},
	{"s.orfe", 0.03306332, 0.33333333, 1739, 0.3976},
	{"s_carp", 0.04055543, 0.33333333, 31632, 1.2826},
	{"s_freaks", 0.0389933, 0.33333333, 31717, 1.2343},
	{"s_saurus", 0.04588158, 0.33333333, 627, 0.3927},
	{"sabrefish", 0.04257855, 0.33333333, 721, 0.3818},
	{"sardina_p", 0.0412904, 0.33333333, 124, 0.2059},
	{"sea_kambala", 0.04268436, 0.33333333, 4548, 0.7072},
	{"sevan_trout", 0.03389326, 0.33333333, 2342, 0.4501},
	{"sh_barbel", 0.03913455, 0.33333333, 9752, 0.8361},
	{"sig", 0.03088153, 0.33333333, 368, 0.2213},
	{"sig_ludoga", 0.03402643, 0.33333333, 1461, 0.3861},
	{"sig_p", 0.03389494, 0.33333333, 1086, 0.3484},
	{"sig_vouksa", 0.03387739, 0.33333333, 1194, 0.3594},
	{"sm_bass", 0.03718524, 0.33333333, 2049, 0.4723},
	{"sp_bass", 0.03686329, 0.33333333, 1391, 0.4115},
	{"syb_vendace", 0.0438577, 0.33333333, 158, 0.2371},
	{"tench", 0.03973345, 0.33333333, 6782, 0.7521},
	{"turbot", 0.03464644, 0.33333333, 11003, 0.7706},
	{"valyok", 0.03729389, 0.33333333, 197, 0.217},
	{"vendace", 0.04389689, 0.33333333, 488, 0.3456},
	{"wa_bream", 0.03251517, 0.33333333, 531, 0.2633},
	{"walleye", 0.03960157, 0.33333333, 4053, 0.6314},
	{"warm", 0.0409036, 0.33333333, 584, 0.3419},
	{"wh_bass", 0.03931814, 0.33333333, 1592, 0.4591},
	{"white_crappie", 0.04023097, 0.33333333, 1239, 0.4321},
	{"yotsushiro", 0.03155734, 0.33333333, 5352, 0.552},
	{"zoarces_v", 0.04811922, 0.33333333, 1065, 0.4914},
	{"zs_grayling", 0.03643407, 0.33333333, 830, 0.3424},
}

func TestOfficialLengthSingleAnchors(t *testing.T) {
	for _, a := range officialLengthSingleAnchors {
		k, p := calibratedLengthKP(a.species, defaultSpeciesLengthK, defaultSpeciesLengthP)
		if k != a.k || p != a.p {
			t.Errorf("%s: got (k=%f,p=%f), want (k=%f,p=%f)", a.species, k, p, a.k, a.p)
			continue
		}
		got := speciesLengthM(a.species, k, p, a.wG)
		if math.Abs(got-a.lM) > 1e-4 {
			t.Errorf("%s %fg: L=%f, want %f", a.species, a.wG, got, a.lM)
		}
	}
}

// 残差修复 7 种：新 k 必须异于旧 KOverride 值（旧模型系统性偏长）。
var lengthRepairSpecies = []struct {
	species string
	oldK    float64
	wG      float64
	lM      float64
	tolPct  float64
}{
	{"carp", 0.044208, 25665, 1.12, 12},
	{"c.carp", 0.045214, 2439, 0.4872, 8},
	{"carp_ghost", 0.042832, 12632, 0.7463, 9},
	{"coalfish", 0.044971, 17823, 1.0985, 21},
	{"m_carp", 0.042678, 25189, 1.1538, 7},
	{"makrel", 0.047622, 2059, 0.4552, 8},
	{"n_carp", 0.042554, 21513, 1.0226, 7},
}

func TestLengthRepairSpecies(t *testing.T) {
	for _, a := range lengthRepairSpecies {
		k, _ := calibratedLengthKP(a.species, defaultSpeciesLengthK, defaultSpeciesLengthP)
		if k == a.oldK {
			t.Errorf("%s: k 未修复，仍为旧值 %f", a.species, k)
		}
		got := lengthPred(a.species, a.wG)
		if math.Abs(got-a.lM)/a.lM*100 > a.tolPct {
			t.Errorf("%s %fg: L=%f, want %f (±%f%%)", a.species, a.wG, got, a.lM, a.tolPct)
		}
	}
}

func TestLengthGroupFallback(t *testing.T) {
	// 零标定种 u_lamprey → 组锚。
	sp, grp := "u_lamprey", "marine_rock"
	if sp == "" {
		t.Skip("无零标定种")
	}
	k, p := calibratedLengthKP(sp, defaultSpeciesLengthK, defaultSpeciesLengthP)
	want := fishLengthGroupAnchor[grp]
	if k != want.k || p != want.p {
		t.Errorf("%s: got (k=%f,p=%f), want (k=%f,p=%f)", sp, k, p, want.k, want.p)
	}
}

func TestLengthLinearUntouched(t *testing.T) {
	// Linear 种不入新表，标定链路不变。
	for sp := range speciesLengthLinear {
		if _, ok := fishLengthOfficialKP[sp]; ok {
			t.Errorf("%s 在 Linear 表却进了拟合表，Linear 分支会被旁路", sp)
		}
		if _, ok := fishLengthSingleK[sp]; ok {
			t.Errorf("%s 在 Linear 表却进了单点表", sp)
		}
	}
}

func TestLengthCoverage(t *testing.T) {
	// fishPullSpeciesGroup 每键必须落在：拟合/单点/旧KOverride/组锚 之一。
	for sp := range fishPullSpeciesGroup {
		if _, ok := fishLengthOfficialKP[sp]; ok {
			continue
		}
		if _, ok := fishLengthSingleK[sp]; ok {
			continue
		}
		if _, ok := speciesLengthKOverride[sp]; ok {
			continue
		}
		grp, ok := fishPullSpeciesGroup[sp]
		if !ok {
			continue
		}
		if _, ok := fishLengthGroupAnchor[grp]; !ok {
			t.Errorf("%s（组 %s）体长无覆盖", sp, grp)
		}
	}
}

// TestLengthAliasInheritsHostMeasured 皮肤变种没有自己的实测时，必须继承宿主的
// 官服拟合/单点；否则旧 KOverride 会继续把宿主已修正的偏长误差带给变种。
// 变种自己有实测锚时（如 common_carp/m_carp_ghost）仍必须优先使用自己的值。
func TestLengthAliasInheritsHostMeasured(t *testing.T) {
	inherited := 0
	for alias, host := range fishPullAlias {
		if _, ownFit := fishLengthOfficialKP[alias]; ownFit {
			continue
		}
		if _, ownSingle := fishLengthSingleK[alias]; ownSingle {
			continue
		}
		if _, ok := fishLengthOfficialKP[host]; !ok {
			if _, ok := fishLengthSingleK[host]; !ok {
				continue
			}
		}
		inherited++
		gotK, gotP := calibratedLengthKP(alias, defaultSpeciesLengthK, defaultSpeciesLengthP)
		wantK, wantP := calibratedLengthKP(host, defaultSpeciesLengthK, defaultSpeciesLengthP)
		if gotK != wantK || gotP != wantP {
			t.Errorf("%s 应继承宿主 %s 的官服体长参数：got=(%f,%f) want=(%f,%f)",
				alias, host, gotK, gotP, wantK, wantP)
		}
	}
	if inherited == 0 {
		t.Fatal("测试前提失效：没有可验证的别名继承用例")
	}

	for _, alias := range []string{"common_carp", "m_carp_ghost"} {
		gotK, gotP := calibratedLengthKP(alias, defaultSpeciesLengthK, defaultSpeciesLengthP)
		want := fishLengthSingleK[alias]
		if gotK != want.k || gotP != want.p {
			t.Errorf("%s 有独立实测锚时必须保留自身参数：got=(%f,%f) want=(%f,%f)",
				alias, gotK, gotP, want.k, want.p)
		}
	}
}

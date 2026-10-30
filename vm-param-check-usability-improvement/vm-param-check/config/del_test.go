package config

import (
	"strings"
	"testing"
)

func TestIsDelOption(t *testing.T) {
	for _, n := range []string{"all_del_affinity", "all_del_lpage", "ev02_del_affinity", "ev10_del_lpage", "ev1_del_lpage"} {
		if !IsDelOption(n) {
			t.Errorf("IsDelOption(%q) = false", n)
		}
	}
	for _, n := range []string{"affinity-ev02", "cpu", "del_lpage", "tag-PURPOSE", "x_del_lpage"} {
		if IsDelOption(n) {
			t.Errorf("IsDelOption(%q) = true", n)
		}
	}
}

func TestDelGroups(t *testing.T) {
	aff, lp, err := DelGroups([]SpecOption{{"all_del_affinity", "y"}, {"ev02_del_lpage", "Y"}}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(aff) != 3 || !aff["ev01"] || !aff["ev02"] || !aff["ev03"] {
		t.Errorf("aff = %v", aff)
	}
	if len(lp) != 1 || !lp["ev02"] {
		t.Errorf("lp = %v", lp)
	}

	aff, lp, err = DelGroups([]SpecOption{{"cpu", "4"}}, 2)
	if err != nil || len(aff) != 0 || len(lp) != 0 {
		t.Errorf("옵션 없음: %v %v %v", aff, lp, err)
	}
}

func TestDelGroupsErrors(t *testing.T) {
	cases := map[string][]SpecOption{
		"그룹 수 초과": {{"ev03_del_lpage", "y"}},
		"값 n":     {{"all_del_affinity", "n"}},
		"NN 형식":    {{"ev1_del_affinity", "y"}},
		"ev00":     {{"ev00_del_lpage", "y"}},
	}
	for name, opts := range cases {
		if _, _, err := DelGroups(opts, 2); err == nil {
			t.Errorf("%s: 에러가 나야 합니다", name)
		}
	}
}

// 제외 옵션은 그룹을 만들지 않고, 모든 ev 로 펼쳐져 내보내져야 한다.
func TestExportSpecDelOptions(t *testing.T) {
	base := "cpu=4 mem=8 disk=100 shares-ev01=normal cores=4 numa=4\n" +
		"cpu-ev02=2 mem-ev02=4 disk-ev02=50 shares-ev02=1000\n"
	m := writeSpec(t, base+"all_del_lpage=y\nev02_del_affinity=y\n")
	lines, err := ExportSpec(m)
	if err != nil {
		t.Fatal(err)
	}
	got := exportMap(lines)
	for _, k := range []string{"del_lpage-ev01", "del_lpage-ev02", "del_affinity-ev02"} {
		if got[k] != "y" {
			t.Errorf("%s 없음: %v", k, got)
		}
	}
	if _, ok := got["del_affinity-ev01"]; ok || got["groups"] != "2" {
		t.Errorf("ev01 affinity 는 제외되면 안 됨 / groups=%s", got["groups"])
	}

	// 옵션이 없으면 출력이 그대로여야 한다.
	plain, err := ExportSpec(writeSpec(t, base))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range plain {
		if strings.Contains(l.Name, "del_") {
			t.Errorf("제외 줄이 나오면 안 됨: %v", l)
		}
	}

	// 그룹 수를 넘는 ev 는 에러.
	if _, err := ExportSpec(writeSpec(t, base+"ev03_del_lpage=y\n")); err == nil {
		t.Error("ev03_del_lpage 는 에러여야 합니다")
	}
}

func TestSpecGroupCount(t *testing.T) {
	opts := []SpecOption{{"cpu-ev01", "4"}, {"mem-ev03", "8"}, {"cpu", "4"}, {"all_del_lpage", "y"}, {"ev02_del_affinity", "y"}}
	if n := SpecGroupCount(opts); n != 3 {
		t.Errorf("SpecGroupCount = %d, want 3", n)
	}
	if n := SpecGroupCount(nil); n != 0 {
		t.Errorf("빈 옵션 = %d", n)
	}
}

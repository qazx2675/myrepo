package csvproc

import (
	"fmt"
	"strings"
	"testing"

	"datacenter-viewer/internal/model"
)

func TestParseCenterAndModel(t *testing.T) {
	cases := []struct{ center, model, wantCenter, wantModel string; explicit bool }{
		{"", "012_001", "SITE-A", "012_001", false},
		{"", "판교_012_001", "판교", "012_001", true},
		{"", "SITE-B:012_001", "SITE-B", "012_001", true},
		{"", "[판교]012_001", "판교", "012_001", true},
		{"센터2", "012_001", "센터2", "012_001", true},
	}
	for _, c := range cases {
		got := ParseCenterAndModel(c.center, c.model, "SITE-A")
		if got.CenterName != c.wantCenter || got.ModelCode != c.wantModel || got.HasExplicitCenter != c.explicit {
			t.Errorf("%q/%q => %+v", c.center, c.model, got)
		}
	}
}

func TestParseTsvNoHeader(t *testing.T) {
	rows := ParseFlexibleCsvOrTsv("012_001\tX029Y001\tmsvr0001\t1\n012_001\tX029Y001\t\t2\t10")
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	if rows[0].V["랙라벨"] != "X029Y001" || rows[1].V["수동지정U"] != "10" {
		t.Errorf("unexpected %+v", rows)
	}
}

// 사용자 샘플과 동일 조건: 1U 서버 145대를 한 랙(52U/서버48U)에 넣으면 48대만 성공, 97대 실패.
func TestServerRangeOverflow(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 145; i++ {
		fmt.Fprintf(&sb, "012_001\tX029Y002\tmsvr%04d\t1\n", i)
	}
	res := Validate(ParseFlexibleCsvOrTsv(sb.String()), Options{AutoCreateMissingRacks: true})
	if res.Summary.SuccessCount != 48 || res.Summary.FailCount != 97 {
		t.Fatalf("success=%d fail=%d", res.Summary.SuccessCount, res.Summary.FailCount)
	}
	if res.ValidList[0].StartU != 1 || res.ValidList[47].StartU != 48 {
		t.Errorf("아래부터 자동 할당되어야 함")
	}
	if len(res.NewlyCreatedRacks) != 1 || res.NewlyCreatedRacks[0].MaxU != 52 {
		t.Errorf("랙 자동 생성 실패")
	}
}

func TestManualStartAndCollision(t *testing.T) {
	rows := ParseFlexibleCsvOrTsv("012_001\tX001Y001\tpsvr1\t4\t5\n012_001\tX001Y001\tpsvr2\t1\t6\n012_001\tX001Y001\t\t1")
	res := Validate(rows, Options{AutoCreateMissingRacks: true})
	if res.Summary.SuccessCount != 2 || res.Summary.FailCount != 1 {
		t.Fatalf("%+v", res.Summary)
	}
	if res.ValidList[1].StartU != 1 || !res.ValidList[1].IsTemp {
		t.Errorf("빈 hostname 은 임시할당, 1U 부터: %+v", res.ValidList[1])
	}
}

func TestNewCenterAutoCreated(t *testing.T) {
	rows := ParseFlexibleCsvOrTsv("판교\t012_001\tX001Y001\tmsvr1\t1")
	res := Validate(rows, Options{
		AutoCreateMissingRacks: true,
		AllSites:               []model.Site{{SiteID: "SITE-A", Name: "SITE-A"}},
	})
	if len(res.TargetSites) != 1 || res.TargetSites[0] != "판교" || res.ValidList[0].SiteID != "판교" {
		t.Errorf("신규 센터 자동 추가 실패: %+v", res)
	}
}

func TestSwitchUsesTopArea(t *testing.T) {
	res := Validate(ParseFlexibleCsvOrTsv("switch\tX001Y001\tcsw1\t1"), Options{AutoCreateMissingRacks: true})
	if res.ValidList[0].StartU != 49 {
		t.Errorf("스위치는 49U 부터: %d", res.ValidList[0].StartU)
	}
}

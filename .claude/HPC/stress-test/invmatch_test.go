package main

import (
	"reflect"
	"testing"
)

func TestInventoryDiff(t *testing.T) {
	cases := []struct {
		inv, want, ex  []string
		extra, missing []string
	}{
		{[]string{"n1", "n2"}, []string{"N2", "n1"}, nil, nil, nil},
		{[]string{"n1", "n2", "old9"}, []string{"n1", "n2"}, nil, []string{"old9"}, nil},
		{[]string{"n1"}, []string{"n1", "n2"}, nil, nil, []string{"n2"}},
		{[]string{"n1", "mgmt01"}, []string{"n1", "MGMT02"}, []string{"mgmt01", "mgmt02"}, nil, nil},
		{[]string{}, []string{"n1"}, []string{"n1"}, nil, nil},
	}
	for i, c := range cases {
		e, m := inventoryDiff(c.inv, c.want, c.ex)
		if !reflect.DeepEqual(e, c.extra) || !reflect.DeepEqual(m, c.missing) {
			t.Errorf("%d: extra=%v missing=%v, want %v %v", i, e, m, c.extra, c.missing)
		}
	}
}

func TestConfigInventoryMatchExclude(t *testing.T) {
	c, err := parseConfig("inventory_match_exclude = mgmt01 | mgmt02|| # 주석\n", "x.conf")
	if err != nil || !reflect.DeepEqual(c.InventoryMatchExclude, []string{"mgmt01", "mgmt02"}) {
		t.Errorf("got %v, %v", c.InventoryMatchExclude, err)
	}
}

// 등록 직후 인벤토리에 대상 밖 호스트가 남아 있으면 그 묶음은 launch 하지 않고 실패한다. 제외 호스트는 비교에서 빠진다.
func TestLaunchBatchInventoryMismatch(t *testing.T) {
	a, f, _ := newTestApp(t)
	fakeClock(t) // 완전일치 재조회 대기를 건너뛴다
	if err := a.prepareAWX([]string{stageCPU}); err != nil {
		t.Fatal(err)
	}
	ts := twoTargets()
	for _, x := range ts {
		x.Res = map[string]string{}
	}
	f.hosts = []string{"n1", "n2", "old9"}
	if a.launchBatch(stageCPU, batch{name: "cpu", tc: a.cfg.CPU, list: ts}, 0) {
		t.Fatal("불일치인데 launch 했습니다")
	}
	if len(f.launches) != 0 || ts[0].Res[stageCPU] != resFail {
		t.Errorf("launches=%v res=%v", f.launches, ts[0].Res)
	}
	a.cfg.InventoryMatchExclude = []string{"OLD9"}
	if !a.launchBatch(stageCPU, batch{name: "cpu", tc: a.cfg.CPU, list: ts}, 0) {
		t.Error("제외 호스트만 다른데 launch 하지 않았습니다")
	}
}

// 등록 직후 지우는 중이던 옛 호스트가 잠깐 남아 있어도, 다시 조회해 대상과 같아지면 launch 한다(실제 장애: 900대 삭제 중 등록).
func TestLaunchBatchInventoryLateMatch(t *testing.T) {
	a, f, _ := newTestApp(t)
	fakeClock(t)
	if err := a.prepareAWX([]string{stageCPU}); err != nil {
		t.Fatal(err)
	}
	ts := twoTargets()
	for _, x := range ts {
		x.Res = map[string]string{}
	}
	f.hostSeq = [][]string{{"n1", "n2", "old9"}, {"n1", "n2", "old9"}, {"n1", "n2"}}
	if !a.launchBatch(stageCPU, batch{name: "cpu", tc: a.cfg.CPU, list: ts}, 0) {
		t.Fatalf("다시 조회해 일치했는데 launch 하지 않았습니다: res=%v", ts[0].Res)
	}
	if len(f.launches) != 1 {
		t.Errorf("launches=%v", f.launches)
	}
}

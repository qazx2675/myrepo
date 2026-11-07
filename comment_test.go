package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "골든 파일 갱신")

type spec struct {
	vendor string
	st     Status
	n      int
}

// mkSnap 은 (벤더, 상태, 개수) 목록으로 스냅샷을 만든다. 호스트 이름은 <벤더>NNN 형태로 정렬 가능하게 붙인다.
// names 로 지정하면 그 이름을 쓴다.
func mkSnap(_ []string, specs []spec, names map[string][]string) Snapshot {
	cfg := defaultConfig()
	var recs []Record
	idx := 0
	for _, sp := range specs {
		for i := 0; i < sp.n; i++ {
			name := fmt.Sprintf("h%05d", idx)
			idx++
			if l := names[sp.vendor+fmt.Sprint(sp.st)]; i < len(l) {
				name = l[i]
			}
			recs = append(recs, Record{Name: name, Vendor: sp.vendor, Status: sp.st})
		}
	}
	return *buildSnapshot(cfg, recs, 1, false, time.Time{}, "")
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden")
	if *update {
		os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s 불일치\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func TestInstallComment(t *testing.T) {
	// 사용자 예시: 300대 = 완료30 + 설치중265 + 디스크3(sds) + ping X 2(sds)
	basic := mkSnap(nil, []spec{
		{"D", StatusDone, 30}, {"D", StatusInstalling, 265},
		{"sds", StatusDisk, 3}, {"sds", StatusPingX, 2},
	}, map[string][]string{
		"sds" + StatusDisk.String():  {"svr3", "svr1", "svr2"},
		"sds" + StatusPingX.String(): {"svr7", "svr5"},
	})
	want := "설치현황\n300대 중 30대 설치완료, 265대 설치중, 3대 디스크 미인식, ping X 2대\nsds\n디스크 미인식 3대\nsvr1 svr2 svr3\n접속불가 2대\nsvr5 svr7"
	if got := InstallComment(basic); got != want {
		t.Fatalf("사용자 예시와 다름\n%q\n%q", got, want)
	}
	golden(t, "install_basic", InstallComment(basic))

	golden(t, "install_delay", InstallComment(mkSnap(nil, []spec{
		{"D", StatusInstalling, 5}, {"D", StatusDelayed, 2}, {"L", StatusDone, 3},
	}, nil)))
	golden(t, "install_off", InstallComment(mkSnap(nil, []spec{
		{"D", StatusDone, 4}, {"D", StatusOff, 2}, {"sds", StatusOff, 1},
	}, nil)))
	golden(t, "install_rebooted", InstallComment(mkSnap(nil, []spec{
		{"D", StatusDone, 2}, {"D", StatusRebooted, 3}, {"D", StatusInstalling, 1},
	}, nil)))
	// 벤더 순서(D, sds, L, 기타)와 확인전은 전체 수에만 들어가는지
	golden(t, "install_multi_vendor", InstallComment(mkSnap(nil, []spec{
		{"L", StatusPingX, 1}, {"sds", StatusDisk, 1}, {"D", StatusPingX, 2},
		{"기타", StatusDisk, 1}, {"D", StatusNone, 2},
	}, nil)))
	golden(t, "install_zero", InstallComment(mkSnap(nil, nil, nil)))
}

func TestDelayComment(t *testing.T) {
	golden(t, "delay_multi", DelayComment(mkSnap(nil, []spec{
		{"sds", StatusDelayed, 2}, {"D", StatusDelayed, 1}, {"D", StatusInstalling, 4}, {"L", StatusDone, 1},
	}, map[string][]string{"sds" + StatusDelayed.String(): {"svrB", "svrA"}})))
	golden(t, "delay_none", DelayComment(mkSnap(nil, []spec{{"D", StatusInstalling, 3}}, nil)))
}

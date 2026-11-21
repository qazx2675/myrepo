package main

import (
	"strings"
	"testing"
)

// l 패널을 연 채로 u 를 누르면 다시 조사한다. 조사 중이면 무시, l 패널 밖에서는 아무것도 하지 않는다.
func TestLPanelURefresh(t *testing.T) {
	snap := tuiSnap()
	v := newTestView(120, 24)
	if a := press(v, snap, "u"); a != actNone {
		t.Fatalf("패널 밖 u: %v", a)
	}
	press(v, snap, "l")
	v.setReport(panelLDAP, "점검 완료", false)
	if a := press(v, snap, "u"); a != actReport || v.reportReq != panelLDAP {
		t.Fatalf("u 로 재조사 요청 안 됨: %v %q", a, v.reportReq)
	}
	v.setReport(panelLDAP, "조사 중", true)
	if a := press(v, snap, "u"); a != actNone {
		t.Fatalf("조사 중 u 는 무시: %v", a)
	}
}

// t 패널은 완료 서버만 보이고, 파일 내용은 머리줄 + 같은 서버들이다.
func TestDoneList(t *testing.T) {
	snap := tuiSnap()
	rows := doneRows(&snap)
	got := DoneList(snap)
	if len(rows) == 0 {
		if !strings.Contains(got, "없습니다") {
			t.Fatalf("완료 0대 안내 없음: %q", got)
		}
	} else if !strings.Contains(got, "완료 ") || !strings.Contains(got, rows[0]) {
		t.Fatalf("완료 목록 %q", got)
	}
	f := string(doneFileContent(&snap))
	if !strings.HasPrefix(f, doneHeader+"\n") || strings.Count(f, "\n") != len(rows)+1 {
		t.Fatalf("파일 내용 %q", f)
	}
	for _, r := range rows {
		if strings.Contains(r, "확인전") || strings.Contains(r, "설치중") {
			t.Errorf("완료 아닌 서버 %q", r)
		}
	}
	v := newTestView(120, 24)
	press(v, snap, "t")
	if v.Panel != panelDone || panelTitle(v.Panel) != "완료 목록" {
		t.Fatalf("t 패널 %q", v.Panel)
	}
}

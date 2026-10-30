package main

import "testing"

func TestParseEvList(t *testing.T) {
	got, err := parseEvList("ev01, EV03", 3)
	if err != nil || len(got) != 3 || !got[0] || got[1] || !got[2] {
		t.Fatalf("정상 목록 파싱 실패: %v %v", got, err)
	}
	if got, err = parseEvList("", 2); err != nil || got[0] || got[1] {
		t.Fatalf("빈 목록은 전부 false 여야 함: %v %v", got, err)
	}
	for _, bad := range []string{"ev00", "ev04", "03", "evxx", "ev01,ev100"} {
		if _, err := parseEvList(bad, 3); err == nil {
			t.Errorf("%q 는 에러여야 함", bad)
		}
	}
}

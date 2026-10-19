// userfilter_test.go - auto_setup -user 옵션: 파싱, job 필터, x/w/m user 단계 건너뜀
package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitUser(t *testing.T) {
	cases := []struct {
		in   []string
		rest []string
		user string
		bad  bool
	}{
		{[]string{"-user", "alice"}, nil, "alice", false},
		{[]string{"--user=bob", "--admin"}, []string{"--admin"}, "bob", false},
		{[]string{"--admin", "-user", "a.b-c_1", "--plain"}, []string{"--admin", "--plain"}, "a.b-c_1", false},
		{[]string{"--start"}, []string{"--start"}, "", false},
		{[]string{"-user"}, nil, "", true},
		{[]string{"-user", "a b"}, nil, "", true},
		{[]string{"-user", "../x"}, nil, "", true},
		{[]string{"-user", ".."}, nil, "", true}, // 디렉터리 이름으로 쓰이므로 . 와 .. 불가
		{[]string{"-user", "."}, nil, "", true},
	}
	for _, c := range cases {
		rest, user, err := splitUser(c.in)
		if (err != nil) != c.bad {
			t.Fatalf("%v: err=%v", c.in, err)
		}
		if c.bad {
			continue
		}
		if user != c.user || !reflect.DeepEqual(rest, c.rest) {
			t.Fatalf("%v: rest=%v user=%q", c.in, rest, user)
		}
	}
}

func TestFilterSnapshotUser(t *testing.T) {
	s := Snapshot{Jobs: []SnapJob{
		{ID: "J1", User: "alice", Counts: StageCounts{Total: 2}},
		{ID: "J2", User: "bob", Counts: StageCounts{Total: 3}},
		{ID: "J3", User: "alice", Closed: true, Counts: StageCounts{Total: 5}},
	}}
	s.Totals.merge(StageCounts{Total: 5})
	got := filterSnapshotUser(s, "alice")
	if len(got.Jobs) != 2 || got.Jobs[0].ID != "J1" || got.Jobs[1].ID != "J3" {
		t.Fatalf("jobs: %+v", got.Jobs)
	}
	if got.Totals.Total != 2 { // 진행 중 job 만 합산
		t.Fatalf("totals: %+v", got.Totals)
	}
	if all := filterSnapshotUser(s, ""); len(all.Jobs) != 3 {
		t.Fatalf("빈 user 는 필터 없음: %+v", all.Jobs)
	}
}

func TestNewUserStepForced(t *testing.T) {
	old := forceUser
	defer func() { forceUser = old }()
	st := &tuiState{}
	forceUser = ""
	if o := st.newUserStep(false, nil); o.step != ooUser {
		t.Fatalf("-user 없으면 기존처럼 user 단계: %d", o.step)
	}
	forceUser = "alice"
	o := st.newUserStep(true, nil)
	if o.step != ooHosts || o.user != "alice" || !o.awx {
		t.Fatalf("-user alice: %+v", o)
	}
}

// -user 로 실행해도 화면 1 의 a 는 다른 user 의 job 까지 보이게 토글한다
func TestForceUserAToggleAllUsers(t *testing.T) {
	h := menuTUI(t)
	oldF, oldS := forceUser, showAllUsers
	t.Cleanup(func() { forceUser, showAllUsers = oldF, oldS })
	forceUser, showAllUsers = "nobody-xyz", false
	h.st.src = userFilterSource{h.st.src}
	h.st.refresh()
	if len(h.st.snap.Jobs) != 0 {
		t.Fatalf("다른 user 라 job 이 없어야 함: %d", len(h.st.snap.Jobs))
	}
	h.keys("a")
	if !showAllUsers || len(h.st.snap.Jobs) == 0 {
		t.Fatalf("a 로 전체 user 가 보여야 함: all=%v jobs=%d", showAllUsers, len(h.st.snap.Jobs))
	}
	if !strings.Contains(h.frame(), "전체 user 보기") {
		t.Errorf("머리에 전체 user 보기 표시 없음")
	}
	h.keys("a")
	if showAllUsers || len(h.st.snap.Jobs) != 0 {
		t.Fatalf("다시 a 로 내 job 만: all=%v jobs=%d", showAllUsers, len(h.st.snap.Jobs))
	}
}

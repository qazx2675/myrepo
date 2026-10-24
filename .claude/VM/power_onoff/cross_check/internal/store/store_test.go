package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sub", "jobs.json") // 하위 디렉터리도 Open 이 만들어야 함
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

func TestOpenMissingIsEmpty(t *testing.T) {
	s, _ := openTemp(t)
	if got := s.List(); len(got) != 0 {
		t.Fatalf("빈 저장소여야 함: %v", got)
	}
}

func TestRoundTrip(t *testing.T) {
	s, p := openTemp(t)
	appr := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	added, err := s.Add(Job{User: "kim", Checker: "lee", Targets: []string{"svr1ev01", "svr1ev02"}, WorkerUID: 1001})
	if err != nil {
		t.Fatal(err)
	}
	if added.ID == "" || added.Status != 대기 || added.CreatedAt.IsZero() {
		t.Fatalf("기본값이 채워져야 함: %+v", added)
	}
	if _, err := s.Update(added.ID, func(j *Job) {
		j.Status = 승인
		j.ApprovedAt = &appr
		j.CheckerUID = 1002
	}); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(p) // 다시 열어도 같아야 함
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Get(added.ID)
	if !ok {
		t.Fatal("다시 연 저장소에 작업이 없음")
	}
	if got.User != "kim" || got.Checker != "lee" || got.Status != 승인 ||
		len(got.Targets) != 2 || got.Targets[1] != "svr1ev02" ||
		got.WorkerUID != 1001 || got.CheckerUID != 1002 {
		t.Fatalf("필드 불일치: %+v", got)
	}
	if got.ApprovedAt == nil || !got.ApprovedAt.Equal(appr) {
		t.Fatalf("ApprovedAt 불일치: %v", got.ApprovedAt)
	}
	if got.StartedAt != nil {
		t.Fatalf("StartedAt 은 미설정이어야 함: %v", got.StartedAt)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "started_at") {
		t.Fatalf("미설정 시각은 파일에 쓰지 않아야 함:\n%s", b)
	}
	if !strings.Contains(string(b), `"대기"`) && !strings.Contains(string(b), `"승인"`) {
		t.Fatalf("상태는 한글 그대로 저장되어야 함:\n%s", b)
	}
}

func TestAddDuplicateAndNotFound(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.Add(Job{ID: "x1", User: "a", Checker: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(Job{ID: "x1"}); !errors.Is(err, ErrExists) {
		t.Fatalf("ErrExists 기대: %v", err)
	}
	if _, err := s.Update("nope", func(*Job) {}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ErrNotFound 기대: %v", err)
	}
	if err := s.Delete("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ErrNotFound 기대: %v", err)
	}
	if _, ok := s.Get("nope"); ok {
		t.Fatal("없는 작업이 조회됨")
	}
}

func TestCopiesAreIsolated(t *testing.T) {
	s, _ := openTemp(t)
	j, _ := s.Add(Job{Targets: []string{"a", "b"}})
	j.Targets[0] = "ZZZ" // 돌려받은 사본을 고쳐도 저장소엔 영향 없어야 함
	got, _ := s.Get(j.ID)
	if got.Targets[0] != "a" {
		t.Fatalf("내부 상태가 노출됨: %v", got.Targets)
	}
	// Update 가 ID 를 바꾸려 해도 무시
	up, _ := s.Update(j.ID, func(x *Job) { x.ID = "hacked" })
	if up.ID != j.ID {
		t.Fatalf("ID 는 바뀌면 안 됨: %s", up.ID)
	}
}

func TestListOrderDeleteClear(t *testing.T) {
	s, _ := openTemp(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"c", "a", "b"} {
		if _, err := s.Add(Job{ID: id, CreatedAt: base.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	var ids []string
	for _, j := range s.List() {
		ids = append(ids, j.ID)
	}
	if strings.Join(ids, ",") != "c,a,b" {
		t.Fatalf("생성 순서여야 함: %v", ids)
	}
	if err := s.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 2 {
		t.Fatal("Delete 후 2건이어야 함")
	}
	n, err := s.Clear()
	if err != nil || n != 2 || len(s.List()) != 0 {
		t.Fatalf("Clear = %d, %v", n, err)
	}
}

func TestAtomicSaveLeavesNoTempFiles(t *testing.T) {
	s, p := openTemp(t)
	for i := 0; i < 5; i++ {
		if _, err := s.Add(Job{User: "u"}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "jobs.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("임시파일이 남으면 안 됨: %v", names)
	}
}

// 저장 실패 시 메모리 변경이 되돌려지는지(파일 위치를 디렉터리로 막아 rename 실패 유도).
func TestSaveFailureRollsBack(t *testing.T) {
	s, p := openTemp(t)
	j, err := s.Add(Job{User: "u", Status: 대기})
	if err != nil {
		t.Fatal(err)
	}
	// jobs.json 을 비어있지 않은 디렉터리로 바꿔 rename 이 실패하게 만든다.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(p, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(j.ID, func(x *Job) { x.Status = 승인 }); err == nil {
		t.Fatal("저장 실패 오류가 나야 함")
	}
	got, _ := s.Get(j.ID)
	if got.Status != 대기 {
		t.Fatalf("변경이 되돌려져야 함: %s", got.Status)
	}
	if _, err := s.Add(Job{ID: "new"}); err == nil {
		t.Fatal("저장 실패 오류가 나야 함")
	}
	if _, ok := s.Get("new"); ok {
		t.Fatal("추가가 되돌려져야 함")
	}
	// 실패한 임시파일이 정리되었는지
	entries, _ := os.ReadDir(filepath.Dir(p))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("임시파일이 남음: %s", e.Name())
		}
	}
}

func TestAbortOpen(t *testing.T) {
	s, p := openTemp(t)
	mk := func(id string, st Status) {
		if _, err := s.Add(Job{ID: id, Status: st}); err != nil {
			t.Fatal(err)
		}
	}
	mk("w", 대기)
	mk("a", 승인)
	mk("d", 중단)
	mk("s", 승인)
	if _, err := s.Update("d", func(j *Job) { j.Reason = "원래 사유" }); err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-time.Minute)
	if _, err := s.Update("s", func(j *Job) { j.ApprovedAt = &started; j.StartedAt = &started }); err != nil {
		t.Fatal(err)
	}

	if n := s.AbortOpen("데몬 재시작"); n != 2 {
		t.Fatalf("AbortOpen = %d, want 2", n)
	}
	// 이미 본 작업을 시작한 승인 건은 기록 그대로
	if j, _ := s.Get("s"); j.Status != 승인 || j.Reason != "" || j.StartedAt == nil {
		t.Fatalf("시작된 승인 건은 유지되어야 함: %+v", j)
	}
	for _, id := range []string{"w", "a"} {
		j, _ := s.Get(id)
		if j.Status != 중단 || j.Reason != "데몬 재시작" {
			t.Fatalf("%s: %+v", id, j)
		}
	}
	d, _ := s.Get("d")
	if d.Reason != "원래 사유" {
		t.Fatalf("이미 중단된 건의 사유는 유지: %+v", d)
	}
	if n := s.AbortOpen("again"); n != 0 {
		t.Fatalf("두 번째 호출은 0 이어야 함: %d", n)
	}

	// 디스크에도 반영되었는지
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if j, _ := s2.Get("w"); j.Status != 중단 {
		t.Fatalf("저장되지 않음: %+v", j)
	}
}

func TestOpenCorruptFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p); err == nil {
		t.Fatal("깨진 파일은 오류여야 함")
	}
	// 빈 파일은 빈 저장소
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p); err != nil {
		t.Fatalf("빈 파일은 허용: %v", err)
	}
}

func TestConcurrentAdd(t *testing.T) {
	s, p := openTemp(t)
	const n = 50
	var wg sync.WaitGroup
	ids := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			j, err := s.Add(Job{User: fmt.Sprintf("u%d", i), Targets: []string{"svr1ev01"}})
			if err != nil {
				t.Errorf("Add: %v", err)
				return
			}
			ids <- j.ID
		}(i)
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("ID 중복: %s", id)
		}
		seen[id] = true
	}
	if len(seen) != n || len(s.List()) != n {
		t.Fatalf("%d 건이어야 함: ids=%d list=%d", n, len(seen), len(s.List()))
	}
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.List()) != n {
		t.Fatalf("파일에도 %d 건이어야 함: %d", n, len(s2.List()))
	}
}

func TestNewID(t *testing.T) {
	a, b := NewID(), NewID()
	if a == b || len(a) < 8 || len(a) > 12 {
		t.Fatalf("짧고 서로 달라야 함: %q %q", a, b)
	}
}

func TestLenCountDeleteWhere(t *testing.T) {
	s, p := openTemp(t)
	for i, st := range []Status{대기, 대기, 승인, 중단, 중단} {
		if _, err := s.Add(Job{ID: fmt.Sprintf("j%d", i), User: "kim", Checker: "lee", Status: st}); err != nil {
			t.Fatal(err)
		}
	}
	if s.Len() != 5 {
		t.Fatalf("Len = %d", s.Len())
	}
	if n := s.Count(func(j *Job) bool { return j.Status == 대기 }); n != 2 {
		t.Fatalf("Count(대기) = %d", n)
	}
	ids, err := s.DeleteWhere(func(j *Job) bool { return j.Status == 중단 })
	if err != nil || strings.Join(ids, ",") != "j3,j4" {
		t.Fatalf("DeleteWhere = %v, %v", ids, err)
	}
	if ids, err := s.DeleteWhere(func(*Job) bool { return false }); err != nil || len(ids) != 0 {
		t.Fatalf("지울 것 없음 = %v, %v", ids, err)
	}
	// 파일에도 반영
	s2, err := Open(p)
	if err != nil || s2.Len() != 3 {
		t.Fatalf("다시 열기: %v len=%d", err, s2.Len())
	}
}

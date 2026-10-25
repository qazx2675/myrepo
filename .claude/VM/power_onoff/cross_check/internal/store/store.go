// Package store 는 교차검증 작업(대기/승인/중단) 기록을 JSON 파일 하나에 보관한다.
//
// 쓰는 쪽은 데몬 하나뿐이라(INTERVIEW Q9) 프로세스 내 뮤텍스로 충분하고,
// 저장은 같은 디렉터리에 임시파일을 쓰고 fsync 후 rename 하는 원자적 방식이다.
// 재기동 시 연결이 사라진 대기/승인 건은 AbortOpen 으로 중단 처리한다.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Status 는 작업 상태. 계획서의 3가지만 둔다(INTERVIEW Q7).
type Status string

const (
	대기 Status = "대기" // --run 이 등록되어 확인자 처리를 기다리는 중
	승인 Status = "승인" // 확인자가 대상을 정확히 붙여넣어 승인됨(시작 여부는 StartedAt)
	중단 Status = "중단" // 취소·불일치·작업자 종료·승인 유효시간 초과·데몬 재시작 등(사유는 Reason)
)

// Phase 는 승인된 작업의 본 작업 진행 단계. 상태가 승인일 때만 뜻이 있다.
// 빈 값은 "승인됐지만 본 작업 시작 전"(승인 유효시간 카운트다운 중)이다.
type Phase string

const (
	작업전  Phase = ""     // 승인, 본 작업 시작 전
	작업중  Phase = "작업중"  // --start 를 받음(작업자 스크립트 실행 중)
	작업완료 Phase = "작업완료" // --finish --result done (스크립트가 취소 없이 끝까지 실행됨)
	작업중단 Phase = "작업중단" // --finish --result aborted (작업자가 멈춤·Ctrl+C), 작업 제한 시간 초과, 데몬 재시작
)

// Job 은 교차검증 작업 한 건. 시각 필드는 nil 이면 아직 없음.
type Job struct {
	ID         string     `json:"id"`
	User       string     `json:"user"`    // 작업자
	Checker    string     `json:"checker"` // 확인자
	Targets    []string   `json:"targets"` // 작업 대상 목록(작업자가 등록한 것)
	Status     Status     `json:"status"`
	Phase      Phase      `json:"phase,omitempty"`  // 승인 뒤 본 작업 단계(빈 값 = 시작 전). 예전 jobs.json 에는 없다
	Reason     string     `json:"reason,omitempty"` // 중단 사유(작업중단 사유 포함)
	Caller     string     `json:"caller,omitempty"` // --run 을 부른 스크립트 이름(정보용, 위조 가능 — 권한 판단에 쓰지 않음)
	CreatedAt  time.Time  `json:"created_at"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`  // --start 신호를 받은 시각
	FinishedAt *time.Time `json:"finished_at,omitempty"` // 작업완료/작업중단이 된 시각
	UpdatedAt  time.Time  `json:"updated_at"`
	WorkerUID  int        `json:"worker_uid"`  // SO_PEERCRED 로 얻은 작업자 OS uid(미사용 시 0)
	CheckerUID int        `json:"checker_uid"` // 승인한 확인자의 OS uid
}

// clone 은 Targets 와 시각 포인터까지 복사한 사본을 돌려준다(호출자가 내부 상태를 못 건드리게).
func (j Job) clone() Job {
	c := j
	c.Targets = append([]string(nil), j.Targets...)
	if j.ApprovedAt != nil {
		t := *j.ApprovedAt
		c.ApprovedAt = &t
	}
	if j.StartedAt != nil {
		t := *j.StartedAt
		c.StartedAt = &t
	}
	if j.FinishedAt != nil {
		t := *j.FinishedAt
		c.FinishedAt = &t
	}
	return c
}

// 호출자가 errors.Is 로 구분할 수 있는 오류.
var (
	ErrNotFound = errors.New("작업을 찾을 수 없음")
	ErrExists   = errors.New("이미 있는 작업 ID")
)

// Store 는 파일 하나에 묶인 작업 저장소. 모든 메서드는 동시 호출에 안전하다.
type Store struct {
	mu   sync.Mutex
	path string
	jobs map[string]*Job
}

// fileFormat 은 디스크에 저장되는 형태.
type fileFormat struct {
	Jobs []*Job `json:"jobs"`
}

// Open 은 path 의 JSON 파일을 읽어 저장소를 연다. 파일이 없으면 빈 저장소(디렉터리는 만든다).
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("저장 디렉터리 생성 실패: %w", err)
	}
	s := &Store{path: path, jobs: map[string]*Job{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("저장 파일 읽기 실패: %w", err)
	}
	if len(b) == 0 {
		return s, nil
	}
	var ff fileFormat
	if err := json.Unmarshal(b, &ff); err != nil {
		return nil, fmt.Errorf("저장 파일 형식 오류(%s): %w", path, err)
	}
	for _, j := range ff.Jobs {
		if j != nil && j.ID != "" {
			s.jobs[j.ID] = j
		}
	}
	return s, nil
}

// NewID 는 시각(초, 36진수)과 랜덤 2바이트(16진수) 로 만든 10자 안팎의 짧은 작업 ID.
func NewID() string {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand 실패: " + err.Error())
	}
	return strconv.FormatInt(time.Now().Unix(), 36) + hex.EncodeToString(b[:])
}

// sortedLocked 는 생성 시각 → ID 순으로 정렬한 사본 목록. 호출 전에 mu 를 잡아야 한다.
func (s *Store) sortedLocked() []Job {
	out := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, j.clone())
	}
	sort.Slice(out, func(a, b int) bool {
		if !out[a].CreatedAt.Equal(out[b].CreatedAt) {
			return out[a].CreatedAt.Before(out[b].CreatedAt)
		}
		return out[a].ID < out[b].ID
	})
	return out
}

// saveLocked 는 현재 상태를 원자적으로 저장한다(임시파일 → fsync → rename). 호출 전에 mu 를 잡아야 한다.
func (s *Store) saveLocked() error {
	jobs := make([]*Job, 0, len(s.jobs))
	for _, j := range s.sortedLocked() {
		j := j
		jobs = append(jobs, &j)
	}
	b, err := json.MarshalIndent(fileFormat{Jobs: jobs}, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("임시파일 생성 실패: %w", err)
	}
	tmpName := tmp.Name()
	fail := func(e error) error {
		tmp.Close()
		os.Remove(tmpName)
		return e
	}
	if _, err := tmp.Write(b); err != nil {
		return fail(fmt.Errorf("임시파일 쓰기 실패: %w", err))
	}
	if err := tmp.Sync(); err != nil {
		return fail(fmt.Errorf("임시파일 fsync 실패: %w", err))
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("임시파일 닫기 실패: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("저장 파일 교체 실패: %w", err)
	}
	// 디렉터리 fsync 는 rename 을 내구성 있게 하려는 것. 지원 안 하는 OS(Windows)는 건너뛴다.
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// Add 는 작업을 추가하고 저장한 뒤 저장된 사본을 돌려준다.
// ID 가 비면 NewID 로 채우고, 상태가 비면 대기, 시각이 비면 지금으로 채운다.
// 이미 있는 ID 면 ErrExists. 저장에 실패하면 추가하지 않은 것으로 되돌린다.
func (s *Store) Add(j Job) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if j.ID == "" {
		for {
			j.ID = NewID()
			if _, dup := s.jobs[j.ID]; !dup {
				break
			}
		}
	} else if _, dup := s.jobs[j.ID]; dup {
		return Job{}, fmt.Errorf("%w: %s", ErrExists, j.ID)
	}
	if j.Status == "" {
		j.Status = 대기
	}
	if j.CreatedAt.IsZero() {
		j.CreatedAt = now
	}
	if j.UpdatedAt.IsZero() {
		j.UpdatedAt = j.CreatedAt
	}
	c := j.clone()
	s.jobs[c.ID] = &c
	if err := s.saveLocked(); err != nil {
		delete(s.jobs, c.ID)
		return Job{}, err
	}
	return c.clone(), nil
}

// Get 은 작업 사본을 돌려준다. 없으면 ok=false.
func (s *Store) Get(id string) (Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, false
	}
	return j.clone(), true
}

// Update 는 fn 으로 작업을 고치고 UpdatedAt 을 지금으로 바꿔 저장한 뒤 갱신된 사본을 돌려준다.
// fn 이 ID 를 바꿔도 무시한다. 없으면 ErrNotFound, 저장에 실패하면 변경을 되돌린다.
func (s *Store) Update(id string, fn func(*Job)) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	old := j.clone()
	fn(j)
	j.ID = id
	j.UpdatedAt = time.Now()
	if err := s.saveLocked(); err != nil {
		*j = old
		return Job{}, err
	}
	return j.clone(), nil
}

// List 는 모든 작업 사본을 생성 시각 순(같으면 ID 순)으로 돌려준다.
func (s *Store) List() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sortedLocked()
}

// Len 은 저장된 작업 수.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.jobs)
}

// Count 는 pred 가 true 인 작업 수를 센다(사본을 만들지 않는다). pred 는 받은 값을 읽기만 해야 하고
// 저장소 메서드를 부르면 안 된다.
func (s *Store) Count(pred func(j *Job) bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, j := range s.jobs {
		if pred(j) {
			n++
		}
	}
	return n
}

// DeleteWhere 는 pred 가 true 인 작업을 모두 지우고 한 번에 저장한 뒤 지운 ID 목록을 돌려준다.
// 저장에 실패하면 아무것도 지우지 않은 것으로 되돌린다. pred 규칙은 Count 와 같다.
func (s *Store) DeleteWhere(pred func(j *Job) bool) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := map[string]*Job{}
	for id, j := range s.jobs {
		if pred(j) {
			removed[id] = j
		}
	}
	if len(removed) == 0 {
		return nil, nil
	}
	for id := range removed {
		delete(s.jobs, id)
	}
	if err := s.saveLocked(); err != nil {
		for id, j := range removed {
			s.jobs[id] = j
		}
		return nil, err
	}
	ids := make([]string, 0, len(removed))
	for id := range removed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// Delete 는 작업 하나를 지운다. 없으면 ErrNotFound.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	delete(s.jobs, id)
	if err := s.saveLocked(); err != nil {
		s.jobs[id] = j
		return err
	}
	return nil
}

// Clear 는 모든 작업을 지우고 지운 개수를 돌려준다(--admin 전체 정리용).
func (s *Store) Clear() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.jobs
	n := len(old)
	s.jobs = map[string]*Job{}
	if err := s.saveLocked(); err != nil {
		s.jobs = old
		return 0, err
	}
	return n, nil
}

// AbortOpen 은 데몬 재시작 직후 한 번 호출한다: 재시작 전 연결과 메모리의 token 은 모두 사라졌기 때문이다.
//   - 대기, 그리고 승인됐지만 본 작업 시작 전(Phase 빈 값)인 작업은 중단(사유 reason)으로 바꾼다.
//   - 본 작업 중(Phase 작업중)이던 작업은 상태 승인과 시작 기록을 그대로 두고 Phase 만 작업중단(사유 reason)으로 바꾼다
//     (token 이 사라져 작업자가 결과(--finish)를 보낼 수 없으므로 더 추적하지 않는다).
//   - 이미 끝난 작업(작업완료·작업중단·중단)은 건드리지 않는다.
//
// 바꾼 개수를 돌려준다. 저장 실패 시에도 메모리 상태는 바뀐 채 남고, 다음 저장 때 함께 기록된다.
func (s *Store) AbortOpen(reason string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	n := 0
	for _, j := range s.jobs {
		switch {
		case j.Status == 대기 || (j.Status == 승인 && j.Phase == 작업전 && j.StartedAt == nil):
			j.Status = 중단
			j.Reason = reason
			j.UpdatedAt = now
			n++
		case j.Status == 승인 && j.Phase == 작업중:
			// (Phase 가 없던 예전 버전의 "시작함" 기록(StartedAt 만 있음)은 끝났는지 알 수 없어 그대로 둔다)
			j.Phase = 작업중단
			j.Reason = reason
			t := now
			j.FinishedAt = &t
			j.UpdatedAt = now
			n++
		}
	}
	if n > 0 {
		_ = s.saveLocked()
	}
	return n
}

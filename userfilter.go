// userfilter.go - auto_setup -user <이름>: 해당 user 의 job 만 보이고, x/w/m 의 user 선택 단계를 건너뛴다.
package main

import (
	"fmt"
	"strings"
)

// forceUser: -user 로 지정한 user (비면 기존과 같음 — 모든 job 이 보이고 user 를 물어본다)
var forceUser string

// splitUser: 명령 앞쪽(--admin·--plain·-user 만 이어지는 구간)에서 -user/--user <이름> 또는 -user=<이름> 을 떼어 낸다.
// 값이 없거나 user 이름 규칙(영문·숫자·. _ -)에 맞지 않으면 오류.
func splitUser(args []string) (rest []string, user string, err error) {
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "-user" || a == "--user":
			if i+1 >= len(args) {
				return nil, "", fmt.Errorf("%s 다음에 user 이름이 필요합니다 (예: auto_setup -user alice)", a)
			}
			user = args[i+1]
			i += 2
		case strings.HasPrefix(a, "-user=") || strings.HasPrefix(a, "--user="):
			user = a[strings.Index(a, "=")+1:]
			i++
		case a == "--admin" || a == "--plain":
			rest = append(rest, a)
			i++
		default:
			rest = append(rest, args[i:]...)
			i = len(args)
		}
	}
	if user != "" && (!userNameRe.MatchString(user) || !validChangeUser(user)) { // . 와 .. 는 디렉터리 이름으로 쓰이는 곳(newhost·work)이 있어 불가
		return nil, "", fmt.Errorf("user 이름이 올바르지 않습니다: %q (영문·숫자·. _ - 만)", user)
	}
	return rest, user, nil
}

// userFilterSource: Snapshot 에서 forceUser 의 job 만 남긴다 (나머지 동작은 그대로)
type userFilterSource struct{ SnapshotSource }

func (u userFilterSource) Snapshot() (Snapshot, error) {
	s, err := u.SnapshotSource.Snapshot()
	if err != nil {
		return s, err
	}
	return filterSnapshotUser(s, forceUser), nil
}

// filterSnapshotUser: user 의 job 만 남기고 진행 중 합계를 다시 계산
func filterSnapshotUser(s Snapshot, user string) Snapshot {
	if user == "" {
		return s
	}
	jobs := make([]SnapJob, 0, len(s.Jobs))
	var totals StageCounts
	for _, j := range s.Jobs {
		if j.User != user {
			continue
		}
		jobs = append(jobs, j)
		if !j.Closed {
			totals.merge(j.Counts)
		}
	}
	s.Jobs, s.Totals = jobs, totals
	return s
}

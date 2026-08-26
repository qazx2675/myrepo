package main

import (
	"reflect"
	"regexp"
	"testing"
)

// -f 목록에 VM 이름을 그대로 적어도 BM 접두어로 바뀌고, 중복 제거 + 이름순 정렬돼야 한다.
func TestNormalizeTargets(t *testing.T) {
	tests := []struct {
		name         string
		in           []string
		want         []string
		wantStripped int
	}{
		{
			name:         "VM 이름으로만 적은 경우",
			in:           []string{"hostnameaev01", "hostnameaev02", "hostnamebev01", "hostnamebev02"},
			want:         []string{"hostnamea", "hostnameb"},
			wantStripped: 4,
		},
		{
			name:         "접두어로만 적은 경우 — 그대로 유지",
			in:           []string{"hostnameb", "hostnamea"},
			want:         []string{"hostnamea", "hostnameb"},
			wantStripped: 0,
		},
		{
			name:         "접두어와 VM 이름이 섞이고 서로 중복되는 경우",
			in:           []string{"hostnameb", "hostnameaev01", "hostnamea", "hostnamebev03"},
			want:         []string{"hostnamea", "hostnameb"},
			wantStripped: 2,
		},
		{
			name:         "정렬되지 않은 입력",
			in:           []string{"zzzev01", "aaaev01", "mmmev02"},
			want:         []string{"aaa", "mmm", "zzz"},
			wantStripped: 3,
		},
		{
			name:         "ev+숫자 형태가 아니면 손대지 않음",
			in:           []string{"hostnamea", "host-01", "hostev", "hostnameaev01x"},
			want:         []string{"host-01", "hostev", "hostnamea", "hostnameaev01x"},
			wantStripped: 0,
		},
		{
			name:         "접두어가 비는 ev01 같은 줄은 그대로 둔다",
			in:           []string{"ev01"},
			want:         []string{"ev01"},
			wantStripped: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, stripped := normalizeTargets(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("targets = %v, 기대 %v", got, tt.want)
			}
			if stripped != tt.wantStripped {
				t.Errorf("stripped = %d, 기대 %d", stripped, tt.wantStripped)
			}
		})
	}
}

// splitBase는 예전의 접두어별 정규식(^<접두어>ev\d+$)과 판정이 완전히 같아야 한다.
// 접두어 후보를 전부 훑어가며 옛 방식과 새 방식의 결과를 대조한다.
func TestSplitBaseMatchesOldRegex(t *testing.T) {
	names := []string{
		"hostnameaev01", "hostnameaev02", "hostnamebev1", "hostnamebev003",
		"hostnamea", "hostev", "aev01", "ev01", "aevev01", "hostev01ev02",
		"host-01ev07", "hostnameaev01x", "hostnameaEV01", "host01", "",
		"hostev1ev2ev3", "aev1ev2", "xev0",
	}
	// 위 이름들에서 나올 수 있는 접두어 후보 + 일부러 어긋나는 후보들
	prefixes := []string{
		"hostnamea", "hostnameb", "host", "hostev", "a", "aev", "", "hostev01",
		"host-01", "hostev1ev2", "aev1", "x", "hostname",
	}

	for _, p := range prefixes {
		old := regexp.MustCompile("^" + regexp.QuoteMeta(p) + `ev\d+$`)
		for _, n := range names {
			wantMatch := old.MatchString(n)

			base, ok := splitBase(n)
			gotMatch := ok && base == p

			if gotMatch != wantMatch {
				t.Errorf("접두어 %q / 이름 %q: 새 방식=%v, 기존 정규식=%v (splitBase -> %q, %v)",
					p, n, gotMatch, wantMatch, base, ok)
			}
		}
	}
}

// keyreader_test.go - keyReader: 붙여넣기의 줄바꿈 덩어리를 Enter 하나로 합침 (읽기 경계·CR+LF·시간 간격)
package main

import (
	"testing"
	"time"
)

func countEnter(evs []keyEv) int {
	n := 0
	for _, e := range evs {
		if e.k == kEnter {
			n++
		}
	}
	return n
}

func TestKeyReaderEnterRuns(t *testing.T) {
	t0 := time.Unix(1000, 0)
	cases := []struct {
		name   string
		chunks []string
		gaps   []time.Duration // chunks[i] 를 앞 읽기 gaps[i] 뒤에 읽음 (gaps[0] 은 무시)
		enters int
	}{
		{"한 덩어리 CRLF", []string{"ab\r\ncd"}, nil, 1},
		{"CR 만", []string{"ab\rcd"}, nil, 1},
		{"CR CR 한 덩어리", []string{"ab\r\rcd"}, nil, 1},
		{"CRLF CRLF 한 덩어리", []string{"ab\r\n\r\ncd"}, nil, 1},
		{"CR LF 가 읽기 경계에서 갈림 (빠름)", []string{"ab\r", "\ncd"}, []time.Duration{0, time.Millisecond}, 1},
		{"CR 로 끝나고 CR 로 시작 (빠름)", []string{"ab\r", "\rcd"}, []time.Duration{0, time.Millisecond}, 1},
		{"느린 두 번째 읽기의 CR 은 따로 Enter", []string{"ab\r", "\r"}, []time.Duration{0, 300 * time.Millisecond}, 2},
		{"느린 두 번째 읽기의 LF 도 따로 Enter", []string{"ab\r", "\n"}, []time.Duration{0, 300 * time.Millisecond}, 2},
		{"앞 읽기가 줄바꿈으로 끝나지 않으면 합치지 않음", []string{"ab", "\rcd"}, []time.Duration{0, time.Millisecond}, 1},
		{"Enter 뒤 글자가 오면 다음 CR 은 새 Enter", []string{"a\r", "b", "\r"}, []time.Duration{0, time.Millisecond, time.Millisecond}, 2},
	}
	for _, c := range cases {
		var kr keyReader
		now, total := t0, 0
		for i, ch := range c.chunks {
			if i > 0 {
				now = now.Add(c.gaps[i])
			}
			total += countEnter(kr.events([]byte(ch), now))
		}
		if total != c.enters {
			t.Errorf("%s: Enter %d 개, 기대 %d", c.name, total, c.enters)
		}
	}
}

// 줄바꿈 말고 다른 키는 parseKeys 와 같다
func TestKeyReaderOtherKeys(t *testing.T) {
	var kr keyReader
	got := kr.events([]byte("a\tb\x1b[Ac"), time.Now())
	want := parseKeys([]byte("a\tb\x1b[Ac"))
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

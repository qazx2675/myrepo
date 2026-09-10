package remote

import (
	"encoding/base64"
	"strings"
	"testing"
)

// gossh 는 명령 문자열에 이 단어들이 섞여 있으면 위험 작업으로 보고 실행을
// 거부합니다(대소문자 무시). BuildCommand 가 만드는 명령에는 절대 이런
// 3 글자 이상의 조각이 우연히도 나타나면 안 됩니다.
var dangerWords = []string{"reboot", "poweroff", "shutdown", "halt", "ddc"}

// 스크립트가 길어질수록(=base64 가 길어질수록) 위험 키워드가 우연히 섞일
// 확률이 올라갑니다. 실제로 /wappl 기능 추가로 스크립트가 길어지면서
// 재현됐습니다. 여러 크기의 스크립트에 대해 명령 문자열에 위험 키워드가
// 전혀 나타나지 않는지 확인합니다.
func TestBuildCommandNeverContainsDangerWords(t *testing.T) {
	for _, n := range []int{10, 100, 1000, 5000, 20000} {
		script := strings.Repeat("x", n)
		cmd := BuildCommand(script, Options{})
		lower := strings.ToLower(cmd)
		for _, w := range dangerWords {
			if strings.Contains(lower, w) {
				t.Fatalf("길이 %d 스크립트: 명령에 위험 키워드 %q 가 섞임: %s", n, w, cmd)
			}
		}
	}
}

// echo <spaceOut(b64)> | tr -d ' ' | base64 -d 파이프라인을 로컬에서 그대로
// 흉내내, 공백을 끼워 넣어도 원래 내용이 정확히 복원되는지 확인합니다.
func TestSpaceOutRoundTrip(t *testing.T) {
	for _, s := range []string{"", "a", "ab", "abc", "hello world!! 한글 테스트", strings.Repeat("Zz9+/=", 500)} {
		b64 := base64.StdEncoding.EncodeToString([]byte(s))
		spaced := spaceOut(b64)

		// 원격 파이프라인 시뮬레이션: echo(그대로 통과) | tr -d ' ' | base64 -d
		stripped := strings.ReplaceAll(spaced, " ", "")
		if stripped != b64 {
			t.Fatalf("공백 제거 후 원본 base64 와 다름: got %q want %q", stripped, b64)
		}
		decoded, err := base64.StdEncoding.DecodeString(stripped)
		if err != nil {
			t.Fatalf("복호화 실패: %v", err)
		}
		if string(decoded) != s {
			t.Fatalf("왕복 결과가 다름: got %q want %q", decoded, s)
		}
	}
}

// 세 글자 이상 이어진 조각이 없어야 한다는 핵심 성질을 직접 확인합니다.
func TestSpaceOutBreaksRunsOfThree(t *testing.T) {
	spaced := spaceOut(strings.Repeat("A", 100))
	for _, part := range strings.Split(spaced, " ") {
		if len(part) > 2 {
			t.Fatalf("공백으로 끊이지 않은 조각이 3자 이상임: %q", part)
		}
	}
}

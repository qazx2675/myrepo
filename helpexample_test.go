package main

import (
	"os"
	"strings"
	"testing"
)

// 18개 옵션 모두 설명·사용법·출력결과가 비어 있지 않고, 색상 없이 만들면 이스케이프 코드가 없어야 한다.
func TestHelpExampleAllOptions(t *testing.T) {
	cwd, _ := os.Getwd()
	before, _ := os.ReadDir(".")
	ex := helpExamples()
	for _, d := range optionDocs {
		if len(ex[d.flag]) == 0 {
			t.Errorf("-%s: 예시가 정의되지 않음", d.flag)
			continue
		}
		text := helpExampleText(d, false)
		if !strings.Contains(text, "사용법:") || !strings.Contains(text, "출력결과:") {
			t.Errorf("-%s: 사용법/출력결과 없음:\n%s", d.flag, text)
		}
		if strings.Contains(text, "\033") {
			t.Errorf("-%s: 색상 끔인데 이스케이프 코드가 있음", d.flag)
		}
		out := text[strings.LastIndex(text, "출력결과:")+len("출력결과:"):]
		if strings.TrimSpace(out) == "" {
			t.Errorf("-%s: 출력결과가 비어 있음", d.flag)
		}
	}
	// -rpm 예시는 임시 폴더에서 만들고 지운다: 현재 폴더와 그 안의 파일이 그대로여야 한다.
	if now, _ := os.Getwd(); now != cwd {
		t.Errorf("작업 폴더가 바뀜: %s -> %s", cwd, now)
	}
	if after, _ := os.ReadDir("."); len(after) != len(before) {
		t.Errorf("예시 실행 후 현재 폴더 파일 수가 바뀜: %d -> %d", len(before), len(after))
	}
}

// -b 예시는 실제 printBunched 형식(구분선 + 묶은 호스트)과 접속불가 그룹, 작업 요약을 포함해야 한다.
func TestHelpExampleBunch(t *testing.T) {
	d, _ := findOptionDoc("b")
	text := helpExampleText(d, false)
	for _, want := range []string{"svr[1-3]\n", kernel8, "svr[4-10]\n", kernel7, "접속불가 (Timeout/Refused)", "작업 요약", exampleHostFile + sufResOff} {
		if !strings.Contains(text, want) {
			t.Errorf("-b 예시에 %q 없음:\n%s", want, text)
		}
	}
	// -script 예시에는 작업 요약이 없어야 한다.
	d, _ = findOptionDoc("-script")
	if text := helpExampleText(d, false); strings.Contains(text[strings.Index(text, "출력결과:"):], "작업 요약") {
		t.Errorf("-script 예시에 작업 요약이 있음")
	}
}

func TestHelpExampleArgs(t *testing.T) {
	for _, a := range []string{"-b", "b", "--b", "-dnlgjawkrdjqghkrdls", "tl"} {
		if _, ok := findOptionDoc(a); !ok {
			t.Errorf("%q 를 옵션으로 인식하지 못함", a)
		}
	}
	if code := runHelpExamples([]string{"-xyz"}); code != 1 {
		t.Errorf("모르는 옵션 종료코드 = %d, 1 이어야 함", code)
	}
	if code := runHelpExamples([]string{"-b", "-xyz"}); code != 1 {
		t.Errorf("모르는 옵션이 섞인 경우 종료코드 = %d, 1 이어야 함", code)
	}
}

// 사용법 줄은 살펴보는 옵션(과 값)만 강조색이다.
func TestUsageLineHighlight(t *testing.T) {
	d, _ := findOptionDoc("u")
	got := usageLine("gossh", []string{"-w", "hostnamefile", "-u", "admin", `"uname -r"`}, d, true)
	want := "gossh -w hostnamefile " + colorHelpOpt + "-u admin" + colorReset + ` "uname -r"`
	if got != want {
		t.Errorf("usageLine = %q, want %q", got, want)
	}
	if got := usageLine("gossh", []string{"-w", "f", "-u", "admin"}, d, false); got != "gossh -w f -u admin" {
		t.Errorf("색상 끔 usageLine = %q", got)
	}
}

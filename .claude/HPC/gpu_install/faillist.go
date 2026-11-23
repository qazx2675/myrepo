// faillist.go - fail_list / fail_list.retry 작성
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	failListFile  = "fail_list"
	retryListFile = "fail_list.retry"
)

// Failed 는 Fail 이 채워진 대상만 고른다.
func Failed(ts []*Target) []*Target {
	var f []*Target
	for _, t := range ts {
		if t.Fail != "" {
			f = append(f, t)
		}
	}
	return f
}

// formatFailList 는 "hostname 요청버전 사유" 줄들이다.
func formatFailList(fs []*Target) string {
	var b strings.Builder
	for _, t := range fs {
		fmt.Fprintf(&b, "%s %s %s\n", t.Host, t.Version, t.Fail)
	}
	return b.String()
}

// formatRetryList 는 "hostname 버전" 줄들이라 그대로 list 로 다시 쓸 수 있다.
func formatRetryList(fs []*Target) string {
	var b strings.Builder
	for _, t := range fs {
		fmt.Fprintf(&b, "%s %s\n", t.Host, t.Version)
	}
	return b.String()
}

// WriteFailLists 는 실패한 호스트가 있으면 fail_list·fail_list.retry 를 쓰고 화면에도 출력한다.
// 실패가 없으면 이전 실행이 남긴 두 파일을 지운다.
func WriteFailLists(out io.Writer, fs []*Target) error {
	if len(fs) == 0 {
		os.Remove(failListFile)
		os.Remove(retryListFile)
		return nil
	}
	if err := os.WriteFile(failListFile, []byte(formatFailList(fs)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(retryListFile, []byte(formatRetryList(fs)), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s실패 %d대 (%s, %s):%s\n", red, len(fs), failListFile, retryListFile, reset)
	fmt.Fprint(out, formatFailList(fs))
	return nil
}

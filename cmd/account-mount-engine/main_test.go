package main

import "testing"

func TestFormatDiffLine(t *testing.T) {
	cases := map[string]string{
		"--- /etc/group\t2026-11-06 01:49:25 -0500": "[변경 전 파일] /etc/group",
		"+++ /tmp/tmp.abc\t2026-11-06":              "[변경 후 내용] (적용될 새 내용)",
		"@@ -66,10 +66,12 @@":                       "[위치] 변경 전 66번째 줄부터 10줄 → 변경 후 66번째 줄부터 12줄",
		"@@ -0,0 +1 @@":                             "[위치] 변경 전 0번째 줄부터 0줄 → 변경 후 1번째 줄부터 1줄",
		"-asdf:x:1000:":                             "[삭제] asdf:x:1000:",
		"+asdf:x:1234:":                             "[추가] asdf:x:1234:",
		" tcpdump:x:72:":                            "[유지] tcpdump:x:72:",
	}
	for in, want := range cases {
		if got := formatDiffLine(in); got != want {
			t.Errorf("formatDiffLine(%q) = %q, want %q", in, got, want)
		}
	}
}

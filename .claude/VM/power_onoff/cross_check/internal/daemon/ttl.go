package daemon

// 승인 유효시간(approval_ttl_sec) 도우미: 허용 범위, 사람이 읽는 표기, 중단 사유 문구.
//
// 기본은 120초(2분, 필수 정책의 기준값)이고 cross_check.conf 의 approval_ttl_sec 로 바꾼다.
// 화면·메시지에는 "2분" 을 하드코딩하지 않고 항상 이 파일의 FormatTTL 로 만든 값을 쓴다.

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// approval_ttl_sec 허용 범위(초).
const (
	MinApprovalTTLSec = 10   // 너무 짧으면 확인자가 승인한 뒤 작업자가 --start 하기 전에 만료됨
	MaxApprovalTTLSec = 3600 // 1시간. 이보다 길면 "승인 직후 바로 작업" 이라는 정책의 의미가 없음
)

// EnvMinTTLSec 는 시험 전용: 설정 파일 검증의 최소값(초)을 낮춘다(격리 E2E 가 3초 TTL 을 쓰기 위함).
// 운영에서는 설정하지 않는다. 문서화된 설정이 아니다.
const EnvMinTTLSec = "CROSS_CHECK_TEST_MIN_TTL_SEC"

// minTTLSec 는 LoadConf 가 받아들이는 approval_ttl_sec 의 최소값(기본 MinApprovalTTLSec).
func minTTLSec() int {
	if v := os.Getenv(EnvMinTTLSec); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n < MinApprovalTTLSec {
			return n
		}
	}
	return MinApprovalTTLSec
}

// work_timeout_sec(본 작업 제한 시간) 허용 범위(초). 작업중으로 이보다 오래 --finish 가 없으면 작업중단("작업자 응답 없음").
const (
	MinWorkTimeoutSec = 600    // 10분
	MaxWorkTimeoutSec = 604800 // 7일
)

// EnvMinWorkTimeoutSec 는 시험 전용: work_timeout_sec 검증의 최소값(초)을 낮춘다(격리 E2E 가 몇 초짜리 제한을 쓰기 위함).
// 운영에서는 설정하지 않는다. 문서화된 설정이 아니다.
const EnvMinWorkTimeoutSec = "CROSS_CHECK_TEST_MIN_WORK_TIMEOUT_SEC"

// minWorkTimeoutSec 는 LoadConf 가 받아들이는 work_timeout_sec 의 최소값(기본 MinWorkTimeoutSec).
func minWorkTimeoutSec() int {
	if v := os.Getenv(EnvMinWorkTimeoutSec); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n < MinWorkTimeoutSec {
			return n
		}
	}
	return MinWorkTimeoutSec
}

// FormatTTL 은 초 단위 시간을 사람이 읽는 한국어로 바꾼다.
//
//	120 → "2분"   45 → "45초"   90 → "1분 30초"   3600 → "1시간"   3720 → "1시간 2분"
//
// 0 이하는 "0초".
func FormatTTL(sec int) string {
	if sec <= 0 {
		return "0초"
	}
	h, m, s := sec/3600, sec%3600/60, sec%60
	var parts []string
	if h > 0 {
		parts = append(parts, strconv.Itoa(h)+"시간")
	}
	if m > 0 {
		parts = append(parts, strconv.Itoa(m)+"분")
	}
	if s > 0 {
		parts = append(parts, strconv.Itoa(s)+"초")
	}
	return strings.Join(parts, " ")
}

// FormatTTLDuration 은 FormatTTL 의 time.Duration 판(초 미만은 올림해서 최소 1초로 표시).
func FormatTTLDuration(d time.Duration) string {
	if d <= 0 {
		return FormatTTL(0)
	}
	return FormatTTL(int((d + time.Second - 1) / time.Second))
}

// 승인 만료 사유는 "승인 후 {TTL} 내 미진행" (TTL 은 FormatTTLDuration 표기).
const (
	reasonExpiredPrefix = "승인 후 "
	reasonExpiredSuffix = " 내 미진행"
)

// ReasonExpired 는 승인 후 유효시간(ttl) 안에 --start 가 없어 중단될 때의 사유 "승인 후 {TTL} 내 미진행".
func ReasonExpired(ttl time.Duration) string {
	return reasonExpiredPrefix + FormatTTLDuration(ttl) + reasonExpiredSuffix
}

// IsExpiredReason 은 msg 가 어떤 TTL 값이든 승인 만료 사유 문구인지 알려준다
// (클라이언트가 데몬의 TTL 설정을 몰라도 만료를 알아보게 함).
func IsExpiredReason(msg string) bool {
	return strings.Contains(msg, reasonExpiredPrefix) && strings.Contains(msg, reasonExpiredSuffix)
}

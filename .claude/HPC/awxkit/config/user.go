package config

import "os"

// CurrentUser는 ${user}_setting.conf 파일명에 쓸 사용자 식별자를 반환한다.
// 현장 환경에 맞는 실제 판별 로직(사번, LDAP 계정 등)을 이 함수 안에 채워 넣는다.
// 빈 문자열을 반환하면 ResolveUser가 -user 플래그와 AWXKIT_USER 환경변수로 폴백한다.
func CurrentUser() string {
	// TODO: 여기에 사용자 판별 로직을 넣으세요.
	return ""
}

// ResolveUser는 CurrentUser() -> flagUser -> AWXKIT_USER 환경변수 -> $USER/$USERNAME 순으로
// 사용자 식별자를 결정한다.
func ResolveUser(flagUser string) string {
	if u := CurrentUser(); u != "" {
		return u
	}
	if flagUser != "" {
		return flagUser
	}
	if u := os.Getenv("AWXKIT_USER"); u != "" {
		return u
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return os.Getenv("USERNAME")
}

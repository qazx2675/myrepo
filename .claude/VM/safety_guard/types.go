package safetyguard

// 프로세스 종료 코드 규약: 안전장치 통과 0, 취소·오류 1.
const (
	ExitOK   = 0
	ExitFail = 1
)

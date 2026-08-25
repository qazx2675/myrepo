package cli

// ANSI 색상 코드. 폐쇄망 SSH 터미널(bash)에서의 가독성을 위해 사용한다.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
)

// 결과 마커: 실패/불가=빨강, 성공/완료=초록, 경고/확인필요=노랑
var (
	MarkFail = colorRed + "[X]" + colorReset
	MarkOK   = colorGreen + "[✔]" + colorReset
	MarkWarn = colorYellow + "[!]" + colorReset
	MarkAsk  = colorYellow + "[?]" + colorReset
)

// ColorStatus는 Job/InventoryUpdate 상태 문자열에 색을 입힌다.
func ColorStatus(status string) string {
	switch status {
	case "successful":
		return colorGreen + status + colorReset
	case "failed", "error", "canceled":
		return colorRed + status + colorReset
	default:
		return colorYellow + status + colorReset
	}
}

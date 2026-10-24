// guarddemo: 안전장치 5단계 체험·시험용. 샘플 대상 3대, 실제 작업 없음.
package main

import (
	"context"
	"os"
	"os/signal"

	sg "safetyguard"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(sg.RunDemo(ctx, sg.NewLineReader(os.Stdin), os.Stdout))
}

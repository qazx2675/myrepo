// Package connect는 VC_USER/VC_PASS 환경변수로 실 vCenter(또는 vcsim)에
// govmomi 클라이언트를 연결한다. 기존 vm-param-check와 동일한 변수명을 쓴다.
package connect

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/vim25/soap"
)

// Client는 target(예: "192.168.0.50" 또는 "127.0.0.1:8989")에 접속한다.
func Client(ctx context.Context, target string) (*govmomi.Client, error) {
	user := os.Getenv("VC_USER")
	pass := os.Getenv("VC_PASS")
	if user == "" || pass == "" {
		return nil, fmt.Errorf("VC_USER / VC_PASS 환경변수를 설정하세요")
	}

	u, err := soap.ParseURL(target)
	if err != nil {
		return nil, fmt.Errorf("vCenter 주소 파싱 실패(%s): %w", target, err)
	}
	u.User = url.UserPassword(user, pass)

	// insecure=true: 실 vCenter의 자체서명 인증서 및 vcsim 모두 허용.
	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		return nil, fmt.Errorf("%s 접속 실패: %w", target, err)
	}
	return client, nil
}

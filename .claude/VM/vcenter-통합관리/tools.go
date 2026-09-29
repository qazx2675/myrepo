//go:build tools

// 이후 단계에서 사용할 의존성을 vendor 에 유지하기 위한 파일.
package tools

import (
	_ "github.com/chromedp/cdproto/target"
	_ "github.com/chromedp/chromedp"
	_ "github.com/vmware/govmomi"
	_ "github.com/vmware/govmomi/property"
	_ "github.com/vmware/govmomi/view"
	_ "github.com/vmware/govmomi/vim25/mo"
	_ "golang.org/x/sys/windows"
	_ "golang.org/x/text/encoding/korean"
)

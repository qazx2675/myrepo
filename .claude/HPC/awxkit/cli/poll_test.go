package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"awxkit/awx"
)

// Job이 끝난 뒤 event_processing_finished가 true가 될 때까지 다시 조회하고, 끝내 false면 오류를 낸다.
func TestWaitJobEvents(t *testing.T) {
	for _, tc := range []struct {
		name    string
		doneAt  int // 몇 번째 조회부터 true인지 (0 = 계속 false)
		wantErr bool
	}{{"저장 완료", 2, false}, {"시간 초과", 0, true}} {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			fmt.Fprintf(w, `{"id":7,"status":"successful","event_processing_finished":%t}`, tc.doneAt > 0 && calls >= tc.doneAt)
		}))
		client := awx.NewClient(srv.URL, "u", "p", false, time.Second)
		err := WaitJobEvents(client, &awx.Job{ID: 7}, 1, 2500*time.Millisecond)
		srv.Close()
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err=%v", tc.name, err)
		}
		if tc.doneAt > 0 && calls != tc.doneAt {
			t.Errorf("%s: 조회 %d회", tc.name, calls)
		}
	}
}

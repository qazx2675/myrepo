package tui

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

// ErrTimeout 은 ReadLineTimeout 의 시간 초과.
var ErrTimeout = errors.New("timeout")

type lineMsg struct {
	line string
	err  error
}

// LineReader 는 줄 단위 리더(vm_power_onoff ui.go 의 LineReader 와 같은 방식).
// 읽기 고루틴이 버퍼 없는 채널로 넘기므로, 시간 초과로 포기한 뒤에도 그 줄은 다음 ReadLine 이 받는다.
// 비밀번호도 같은 리더로 읽어야 한다(다른 리더로 stdin 을 읽으면 줄을 서로 가로챈다).
type LineReader struct {
	ch  chan lineMsg
	err error // 채널에서 오류를 받은 뒤 계속 돌려줄 오류(보통 io.EOF)
}

// NewLineReader 는 r 을 읽는 고루틴을 하나 띄운다.
func NewLineReader(r io.Reader) *LineReader {
	lr := &LineReader{ch: make(chan lineMsg)}
	go func() {
		br := bufio.NewReader(r)
		for {
			s, err := br.ReadString('\n')
			if s != "" {
				// 마지막 줄에 개행이 없어도 한 줄로 넘긴다
				lr.ch <- lineMsg{line: strings.TrimRight(s, "\r\n")}
			}
			if err != nil {
				lr.ch <- lineMsg{err: err}
				return
			}
		}
	}()
	return lr
}

// ReadLine 은 한 줄을 읽는다(시간 제한 없음). EOF 는 io.EOF, ctx 취소는 ctx.Err().
func (r *LineReader) ReadLine(ctx context.Context) (string, error) {
	return r.read(ctx, nil)
}

// ReadLineTimeout 은 d 안에 줄이 오지 않으면 ErrTimeout.
func (r *LineReader) ReadLineTimeout(ctx context.Context, d time.Duration) (string, error) {
	if d <= 0 {
		d = time.Millisecond
	}
	t := time.NewTimer(d)
	defer t.Stop()
	return r.read(ctx, t.C)
}

func (r *LineReader) read(ctx context.Context, timeout <-chan time.Time) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	select {
	case m := <-r.ch:
		if m.err != nil {
			r.err = m.err
			return "", m.err
		}
		return m.line, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timeout:
		return "", ErrTimeout
	}
}

package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// ---- 색상 ----

// ColorEnabled 는 색상 출력 여부. PWONOFF_COLOR=never|always 가 우선, 그다음 NO_COLOR, 마지막으로 stdout 이 터미널인지.
func ColorEnabled() bool {
	switch strings.ToLower(os.Getenv("PWONOFF_COLOR")) {
	case "never":
		return false
	case "always":
		return true
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// Paint 는 색상이 켜져 있을 때만 ANSI 코드로 감싼다.
func Paint(code, s string) string {
	if !ColorEnabled() {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func Red(s string) string { return Paint("31;1", s) }

func Green(s string) string { return Paint("32", s) }

func Yellow(s string) string { return Paint("33", s) }

func Cyan(s string) string { return Paint("36", s) }

func Bold(s string) string { return Paint("1", s) }

func RedBg(s string) string { return Paint("41;97;1", s) }

// ---- 공용 stdin 리더 ----

// ErrTimeout 은 ReadLineTimeout 의 시간 초과.
var ErrTimeout = errors.New("timeout")

type lineMsg struct {
	line string
	err  error
}

// LineReader 는 프로세스당 하나만 쓰는 줄 단위 리더.
// 읽기 고루틴이 버퍼 없는 채널로 넘기므로, 시간 초과로 포기한 뒤에도 그 줄은 다음 ReadLine 이 받는다.
type LineReader struct {
	ch  chan lineMsg
	err error // 채널이 닫힌 뒤 돌려줄 오류(보통 io.EOF)
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

// ReadLine 은 한 줄을 읽는다. EOF 는 io.EOF, ctx 취소는 ctx.Err().
func (r *LineReader) ReadLine(ctx context.Context) (string, error) {
	return r.read(ctx, nil)
}

// ReadLineTimeout 은 d 안에 줄이 오지 않으면 ErrTimeout.
func (r *LineReader) ReadLineTimeout(ctx context.Context, d time.Duration) (string, error) {
	if d <= 0 {
		// 남은 시간이 없어도 이미 와 있는 줄은 받지 않는다(시간 초과 우선)
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", ErrTimeout
	}
	t := time.NewTimer(d)
	defer t.Stop()
	return r.read(ctx, t.C)
}

// ReadLineLimit 은 질문 한 번의 제한 시간 d 로 읽는다(d <= 0 이면 제한 없음 = ReadLine). 시간 초과는 ErrTimeout.
func (r *LineReader) ReadLineLimit(ctx context.Context, d time.Duration) (string, error) {
	if d <= 0 {
		return r.ReadLine(ctx)
	}
	return r.ReadLineTimeout(ctx, d)
}

// durText 는 제한 시간을 "10분" / "90초" 형태로(분 단위로 떨어지면 분).
func durText(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return strconv.Itoa(int(d/time.Minute)) + "분"
	}
	return secs(d)
}

// inputTimeoutReason 은 입력 제한 시간 초과의 취소 사유("입력 시간 10분 초과")다.
func inputTimeoutReason(d time.Duration) string { return "입력 시간 " + durText(d) + " 초과" }

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

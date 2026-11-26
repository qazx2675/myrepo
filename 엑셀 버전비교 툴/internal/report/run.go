package report

import (
	"cmp"
	"errors"
	"path/filepath"
	"time"

	"excel-diff/internal/describe"
	"excel-diff/internal/diff"
	"excel-diff/internal/reader"
)

// Options 는 Run 의 입력이다.
type Options struct {
	OldPath, NewPath string
	OldName, NewName string // 결과에 적는 파일명. "" 이면 경로의 파일명
	// Old, New 는 미리 읽어 둔 내용이다. 둘 다 있으면 파일을 다시 읽지 않는다(경로는 쓰지 않는다).
	Old, New    *reader.Workbook
	Pairs       []diff.SheetPair // nil 이면 diff.DefaultPairs
	OutDir      string
	FallbackDir string // OutDir 에 쓸 수 없을 때 쓰는 폴더. "" 이면 대체하지 않는다
	// Progress 는 nil 이어도 된다. percent 는 0 부터 100 까지 줄지 않으며 stage 는 단계 이름이다.
	Progress func(percent int, stage string)
}

// Output 은 Run 의 결과이다.
type Output struct {
	Path     string // 만든 결과 파일
	Summary  diff.Summary
	Items    int // 변경내역 항목 수
	Warnings []reader.Warning
}

// Run 은 두 파일을 읽어 비교하고 결과 파일을 만든다. 입력 파일은 읽기만 한다.
func Run(o Options) (*Output, error) {
	last := 0
	// stage 는 전체의 [from,to] 구간을 차지하는 단계의 진행 함수를 돌려준다.
	stage := func(name string, from, to int) func(done, total int) {
		report := func(done, total int) {
			if o.Progress == nil {
				return
			}
			if total > 0 {
				last = max(last, from+(to-from)*done/total)
			}
			o.Progress(last, name)
		}
		report(0, 1)
		return report
	}

	var err error
	old, new := o.Old, o.New
	// 비교 시작, 설명 시작, 결과 파일 시작 지점(%)
	cmpAt, descAt, writeAt := 0, 30, 40
	if old == nil || new == nil {
		cmpAt, descAt, writeAt = 50, 65, 70
		if old, err = reader.Read(o.OldPath, stage("이전 파일 읽는 중", 0, 25)); err != nil {
			return nil, err
		}
		if new, err = reader.Read(o.NewPath, stage("최신 파일 읽는 중", 25, 50)); err != nil {
			return nil, err
		}
	}
	pairs := o.Pairs
	if pairs == nil {
		pairs = diff.DefaultPairs(old, new)
	}
	res, err := diff.Compare(old, new, pairs, stage("비교하는 중", cmpAt, descAt))
	if err != nil {
		return nil, err
	}
	stage("변경내역 설명 만드는 중", descAt, writeAt)
	src := Source{
		OldName: cmp.Or(o.OldName, filepath.Base(o.OldPath)),
		NewName: cmp.Or(o.NewName, filepath.Base(o.NewPath)),
		Created: time.Now(),
		Result:  res,
		Items:   describe.Describe(res, pairs),
	}
	write := func(dir string) (string, error) {
		report := stage("결과 파일 만드는 중", writeAt, 99)
		saving := false
		return Write(src, dir, func(done, total int) {
			report(done, total)
			if !saving && done*10 >= total*9 {
				saving = true
				report = stage("결과 파일 저장 중", 96, 99)
			}
		})
	}
	path, err := write(o.OutDir)
	if errors.Is(err, ErrOutputDir) && o.FallbackDir != "" {
		path, err = write(o.FallbackDir)
	}
	if err != nil {
		return nil, err
	}
	stage("완료", 100, 100)
	return &Output{Path: path, Summary: res.Summary, Items: len(src.Items), Warnings: res.Warnings}, nil
}

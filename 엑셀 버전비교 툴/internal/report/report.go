package report

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/xuri/excelize/v2"

	"excel-diff/internal/describe"
	"excel-diff/internal/diff"
)

// ErrOutputDir 는 출력 디렉터리에 파일을 만들 수 없다는 오류이다.
// errors.Is 로 확인해 다른 디렉터리(문서 폴더 등)로 다시 시도할 수 있다.
var ErrOutputDir = errors.New("결과 폴더에 파일을 만들 수 없습니다")

// Source 는 결과 파일에 담을 내용이다.
type Source struct {
	OldName, NewName string    // 요약에 적는 파일명
	Created          time.Time // 생성 시각(파일 이름에도 쓴다)
	Result           *diff.Result
	Items            []describe.Item // describe.Describe(Result, pairs)
}

// Write 는 dir 에 "비교결과_YYYYMMDD_HHMMSS.xlsx" 를 만들고 경로를 돌려준다.
// 기존 파일은 덮어쓰지 않으며 이름이 겹치면 "_2" 부터 번호를 붙인다.
// progress 는 nil 이어도 되며, done 은 0 부터 total 까지 증가한다.
// done*10 >= total*9 부터는 압축·저장 구간이다.
func Write(src Source, dir string, progress func(done, total int)) (path string, err error) {
	// 오래 걸리는 작업 전에 쓰기 가능 여부부터 확인한다.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("%w: %w", ErrOutputDir, err)
	}
	tmp, err := os.CreateTemp(dir, "비교결과-*.tmp")
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrOutputDir, err)
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name()) // 이름을 바꾼 뒤에는 없는 파일이다
	}()

	f := excelize.NewFile()
	defer func() { err = errors.Join(err, f.Close()) }()
	w := &writer{f: f, src: src, styles: map[styleKey]int{}, progress: progress}
	if err := w.build(); err != nil {
		return "", fmt.Errorf("결과 파일을 만들 수 없습니다: %w", err)
	}
	w.report(progressSave)
	if err := f.Write(tmp); err != nil {
		return "", fmt.Errorf("결과 파일을 저장할 수 없습니다: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("결과 파일을 저장할 수 없습니다: %w", err)
	}
	path, err = publish(tmp.Name(), dir, "비교결과_"+src.Created.Format("20060102_150405"))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrOutputDir, err)
	}
	w.report(progressTotal)
	return path, nil
}

// publish 는 임시 파일의 이름을 겹치지 않는 결과 파일 이름으로 바꾼다.
// 빈 파일을 배타적으로 만들어 이름을 먼저 차지하므로 남의 파일을 덮어쓰지 않는다.
func publish(tmp, dir, base string) (string, error) {
	for n := 1; ; n++ {
		name := base + ".xlsx"
		if n > 1 {
			name = fmt.Sprintf("%s_%d.xlsx", base, n)
		}
		path := filepath.Join(dir, name)
		hold, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if _, statErr := os.Lstat(path); errors.Is(err, fs.ErrExist) || statErr == nil {
				continue // 같은 이름의 파일·폴더가 있다
			}
			return "", err
		}
		hold.Close()
		if err := os.Rename(tmp, path); err != nil {
			os.Remove(path)
			return "", err
		}
		return path, nil
	}
}

type writer struct {
	f      *excelize.File
	src    Source
	styles map[styleKey]int
	err    error // 서식 생성 오류

	sheets []*sheetPlan
	logs   []string // 변경내역 시트 이름

	progress     func(done, total int)
	done, total  int
	cut          int      // 셀 한도로 줄인 셀 수
	cutAt, notes []string // 요약에 적는 분할·축약 내용
}

const (
	progressTotal = 1000
	progressSave  = 900 // 여기부터는 압축·저장 구간
)

func (w *writer) report(v int) {
	if w.progress != nil {
		w.progress(v, progressTotal)
	}
}

// step 은 행 하나를 썼음을 알린다.
func (w *writer) step() {
	if w.done++; w.done%1000 == 0 {
		w.report(min(w.done, w.total) * progressSave / w.total)
	}
}

func (w *writer) build() error {
	rows := len(w.src.Items)
	for i := range w.src.Result.Sheets {
		sd := &w.src.Result.Sheets[i]
		rows += len(sd.Rows)
		if sd.Old != nil {
			rows += len(sd.Old.Rows)
		}
		if sd.New != nil {
			rows += len(sd.New.Rows)
		}
	}
	w.total = max(rows, 1)
	w.report(0)

	// 시트를 보이는 순서대로 먼저 만든다. 요약은 분할·축약 내용을 모두 안 뒤에 쓴다.
	if err := w.f.SetSheetName("Sheet1", summaryName); err != nil {
		return err
	}
	names := newNamer(summaryName, logName)
	perLog := maxRows - 1
	logRows := w.logRows()
	for n := range max(1, (len(logRows)+perLog-1)/perLog) {
		name := logName
		if n > 0 {
			name = names.name(logName, fmt.Sprint(n+1))
		}
		w.logs = append(w.logs, name)
	}
	if len(w.logs) > 1 {
		w.notes = append(w.notes, fmt.Sprintf("변경내역이 시트당 행 한도를 넘어 %d개 시트로 나눴습니다", len(w.logs)))
	}
	for i := range w.src.Result.Sheets {
		w.sheets = append(w.sheets, w.plan(&w.src.Result.Sheets[i], names))
	}
	all := w.logs
	for _, p := range w.sheets {
		all = append(all[:len(all):len(all)], p.names()...)
	}
	for _, name := range all {
		if _, err := w.f.NewSheet(name); err != nil {
			return fmt.Errorf("시트 '%s': %w", name, err)
		}
	}

	if err := w.writeLog(logRows); err != nil {
		return err
	}
	for _, p := range w.sheets {
		if err := w.writeSheets(p); err != nil {
			return fmt.Errorf("시트 '%s': %w", p.sd.Name(), err)
		}
	}
	if err := w.writeSummary(names); err != nil {
		return err
	}
	return w.err
}

// stream 은 틀 고정과 열 너비를 설정한 스트림을 연다. 둘 다 첫 행보다 먼저 써야 한다.
// freezeRows, freezeCols 는 고정할 행·열 수, widths 는 1열부터의 너비(0 은 기본값)이다.
func (w *writer) stream(name string, freezeRows, freezeCols int, widths []float64) (*excelize.StreamWriter, error) {
	sw, err := w.f.NewStreamWriter(name)
	if err != nil {
		return nil, err
	}
	if freezeRows > 0 {
		pane := "bottomLeft"
		if freezeCols > 0 {
			pane = "bottomRight"
		}
		top, err := excelize.CoordinatesToCellName(freezeCols+1, freezeRows+1)
		if err != nil {
			return nil, err
		}
		if err := sw.SetPanes(&excelize.Panes{Freeze: true, XSplit: freezeCols, YSplit: freezeRows, TopLeftCell: top, ActivePane: pane,
			Selection: []excelize.Selection{{SQRef: top, ActiveCell: top, Pane: pane}}}); err != nil {
			return nil, err
		}
	}
	for i := 0; i < len(widths); {
		j := i + 1
		for j < len(widths) && widths[j] == widths[i] {
			j++
		}
		if widths[i] > 0 {
			if err := sw.SetColWidth(i+1, j, widths[i]); err != nil {
				return nil, err
			}
		}
		i = j
	}
	return sw, nil
}

func setRow(sw *excelize.StreamWriter, row int, values []any) error {
	cell, err := excelize.CoordinatesToCellName(1, row)
	if err != nil {
		return err
	}
	return sw.SetRow(cell, values)
}

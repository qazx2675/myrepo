// init.go: 신규 CAE 스펙 디렉터리를 만드는 폴더 셋업 스크립트(-initFolder).
//
// 현업에서 새 CAE 스펙이 생길 때마다 -specRoot 아래에 디렉터리와 <이름>_spec.txt를
// 손으로 만드는 대신, 이름만 주면 규칙에 맞는 틀을 만들어준다. 기존 스펙을 -template으로
// 주면 그 값을 그대로 복사해서 시작점으로 삼을 수 있다(완전히 새 스펙을 적는 것보다,
// 비슷한 기존 스펙을 베껴서 다른 부분만 고치는 경우가 현업에서 더 흔할 것으로 보고 넣었다).
package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// blankSpecTemplate은 -template 없이 -initFolder만 줬을 때 만드는 빈 틀이다.
// 필수 옵션은 값 없이, 옵션(ev02/ev03 등)은 주석으로 처리해서 어떤 이름을 쓰는지 보여준다.
const blankSpecTemplate = `# %s 스펙 정의 파일
# vm-param-check가 이 폴더에 속한 VM을 체크할 때 자동으로 읽어들이는 기대값이다.
# 형식은 CLI 플래그와 동일하다: 이름=값 (하이픈은 있어도 없어도 됨), '#' 뒤는 주석.

# --- 필수 (ev01 및 미분류 VM 기준) ---
ht=
cores=
numa=
cpu=
mem=
disk=
shares-ev01=          # ratio 숫자(예: 4000) 또는 normal

# --- 선택: ev02 그룹 (없으면 ev02 관련 체크는 스킵됨) ---
# cores-ev02=
# numa-ev02=
# cpu-ev02=
# mem-ev02=
# disk-ev02=
# shares-ev02=

# --- 선택: ev03 그룹 ---
# cores-ev03=
# numa-ev03=
# cpu-ev03=
# mem-ev03=
# disk-ev03=
# shares-ev03=

# --- 선택: affinity 파일 (이 스펙 디렉터리 기준 상대경로 권장) ---
# affinity-ev01=
# affinity-ev02=
# affinity-ev03=
`

// InitFolder는 specRoot 아래에 folderName 스펙 디렉터리와 "<folderName>_spec.txt"를 만든다.
//
// templateFolder가 비어 있지 않으면 그 이름으로 매칭되는 기존 스펙의 옵션을 그대로 복사해서
// 채운다(값이 다른 부분만 나중에 손으로 고치면 되게). affinity 파일처럼 스펙 디렉터리
// 기준 상대경로로 적힌 옵션은, 참조된 파일 자체도 새 디렉터리로 함께 복사한다 — 안 그러면
// 복사된 spec.txt가 옛 디렉터리의 파일을 가리키게 되어 새 스펙만 옮겨서는 못 쓰게 된다.
//
// folderName은 CAE 폴더 규칙을 따라야 하고(안 그러면 vm-param-check가 이 스펙을 못 찾음),
// 같은 스펙으로 이미 존재하는 디렉터리가 있으면 실수로 덮어쓰지 않도록 에러로 막는다.
func InitFolder(specRoot, folderName, templateFolder string) (specFile string, err error) {
	if _, ok := NormalizeFolderName(folderName); !ok {
		return "", fmt.Errorf("폴더명 %q은(는) CAE 폴더 규칙(하이픈 %d개 레코드, 2번째가 CAE<숫자> 또는 LSI<숫자>)과 맞지 않습니다 — 이 규칙에 안 맞으면 만들어도 자동매칭되지 않습니다", folderName, folderRecordCount)
	}

	existing, err := FindSpec(specRoot, folderName)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return "", fmt.Errorf("이미 같은 스펙으로 매칭되는 디렉터리가 있습니다: %s (실수로 덮어쓰지 않도록 중단합니다)", existing.SpecDir)
	}

	specDir := filepath.Join(specRoot, folderName)
	specFile = filepath.Join(specDir, folderName+"_spec.txt")

	var template *SpecMatch
	if templateFolder != "" {
		template, err = FindSpec(specRoot, templateFolder)
		if err != nil {
			return "", fmt.Errorf("-template=%s 조회 실패: %w", templateFolder, err)
		}
		if template == nil {
			return "", fmt.Errorf("-template=%s 에 해당하는 스펙을 %s 아래에서 찾지 못했습니다", templateFolder, specRoot)
		}
	}

	if err := os.MkdirAll(specDir, 0o755); err != nil {
		return "", fmt.Errorf("스펙 디렉터리 생성 실패: %w", err)
	}

	if template == nil {
		content := fmt.Sprintf(blankSpecTemplate, folderName)
		if err := os.WriteFile(specFile, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("스펙 파일 생성 실패: %w", err)
		}
		return specFile, nil
	}

	if err := copySpecFrom(template, specDir, specFile, folderName); err != nil {
		return "", err
	}
	return specFile, nil
}

// copySpecFrom은 template의 옵션 값을 그대로 새 spec.txt에 적고, 상대경로로 참조된
// affinity 파일이 있으면 그 내용도 새 디렉터리로 복사한다.
func copySpecFrom(template *SpecMatch, newSpecDir, newSpecFile, folderName string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s 스펙 정의 파일\n# -template=%s 로부터 값을 복사해서 생성됨 — 필요한 부분만 고쳐서 쓴다.\n\n", folderName, template.FolderName)

	for _, opt := range template.Options {
		value := opt.Value
		if strings.HasPrefix(opt.Name, "affinity-") && !filepath.IsAbs(value) {
			if err := copyFile(filepath.Join(template.SpecDir, value), filepath.Join(newSpecDir, value)); err != nil {
				return fmt.Errorf("affinity 파일 복사 실패(%s): %w", value, err)
			}
		}
		fmt.Fprintf(&b, "%s=%s\n", opt.Name, value)
	}

	if err := os.WriteFile(newSpecFile, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("스펙 파일 생성 실패: %w", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

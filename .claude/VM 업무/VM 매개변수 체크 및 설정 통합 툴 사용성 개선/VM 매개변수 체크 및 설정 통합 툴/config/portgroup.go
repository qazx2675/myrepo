// portgroup.go: 포트그룹 이름에서 CAE 폴더명을 복원한다.
//
// 대상 VM이 CAE 폴더 규칙을 따르지 않는 임시 "Task" 폴더 등에 들어 있을 때, vm.Folder만으로는
// 어떤 스펙을 써야 할지 알 수 없다. 이런 경우 포트그룹 이름에 원래 폴더명이 "<폴더명>-cae-옥텟-
// 옥텟-옥텟-옥텟" 형태로 함께 박혀 있는 관례가 있어서, 그걸 역으로 파싱해 폴더명을 복원한다.
package config

import "regexp"

// portgroupFolderSuffix는 포트그룹 이름 끝의 "-cae-10-111-111-0" 같은 네트워크 식별자
// 부분을 떼어내기 위한 패턴이다. 옥텟은 자릿수가 다를 수 있어 1~3자리로 허용한다.
var portgroupFolderSuffix = regexp.MustCompile(`(?i)^(.+)-cae-\d{1,3}-\d{1,3}-\d{1,3}-\d{1,3}$`)

// ExtractFolderFromPortgroup은 포트그룹 이름에서 CAE 폴더명 부분을 뽑아낸다.
// 예: "RND-CAE001-LICE48c-TCAD-cae-10-111-111-0" -> "RND-CAE001-LICE48c-TCAD"
// 패턴과 안 맞으면 ok=false.
func ExtractFolderFromPortgroup(portgroupName string) (string, bool) {
	m := portgroupFolderSuffix.FindStringSubmatch(portgroupName)
	if m == nil {
		return "", false
	}
	return m[1], true
}

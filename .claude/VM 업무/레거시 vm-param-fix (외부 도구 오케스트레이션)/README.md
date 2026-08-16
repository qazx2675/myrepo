# 레거시 vm-param-fix (외부 도구 오케스트레이션 방식) — 보관용 아카이브

**이 폴더는 더 이상 권장되는 방식이 아닙니다.** 새로 시작하는 경우 대신
`../VM 매개변수 체크 및 설정 통합 툴/`(자체 내장 `-fix`, 외부 도구 불필요)을 쓰세요.
자세한 대체 배경은 그 폴더의 README.md "기존 개별 도구와의 관계" 절 참고.

이 폴더는 Rocky Linux 로컬(`/root/vm-param-fix`, `/root/affinity-test`, `/root/lpage-test`)에만
남아있던 작업 산출물을 로컬 디스크 정리 전에 git으로 옮겨 보관한 것입니다(2026-08-16).

## 구성

- `vm-param-fix/` — vm-param-check가 낸 CSV를 태그(affinity/lpage/power)별로 분류해서
  아래 3개 외부 바이너리를 호출하는 오케스트레이터. 소스+vendor/ 포함, `setup.sh`로 빌드 가능.
- `affinity_setting-source/` — affinity 태그 담당 외부 도구의 실제 소스(원래 `/root/affinity-test/main.go`).
  소스+vendor/ 포함, `setup.sh`로 빌드 가능.
- `lpage_setting-source/` — lpage(HugePage/CPU 토폴로지) 태그 담당 외부 도구의 실제 소스
  (원래 `/root/lpage-test/main.go`). 소스+vendor/ 포함, `setup.sh`로 빌드 가능.
- `vm-param-fix/power_setting` — **호스트 고성능 전원정책 자동교정 도구, 컴파일된 바이너리만 존재**.
  아래 "power_setting에 대한 중요 안내" 참고.

## power_setting에 대한 중요 안내

`power_setting`의 **Go 소스 코드를 Rocky Linux 어디에서도 확실하게 찾지 못했습니다.**
로컬 파일 여러 개(`/root/pro/main.go` 등)를 대조해봤지만, 실행 바이너리의 플래그 구성
(`-vcTargetIP`, `-worklistFile`, `-worklistBmFile`(default) 등)과 정확히 일치하는 소스를
확정하지 못했습니다. 그래서 **컴파일된 바이너리(`vm-param-fix/power_setting`)만** 이 폴더에
보관했습니다 — 소스 없이 바이너리만 있으므로 재빌드는 불가능하고, 이 바이너리 파일 자체가
유일한 사본입니다. 삭제하지 마세요.

같은 이유로, 새 통합 도구(`VM 매개변수 체크 및 설정 통합 툴`)는 **호스트 전원정책 자동교정
기능이 없습니다**(체크만 함, README의 "알려진 한계" 참고) — 이 기능이 다시 필요해지면
`power_setting` 바이너리를 그대로 재사용하거나(vm-param-fix 오케스트레이터를 통해),
같은 로직을 처음부터 새로 작성해야 합니다.

## 사용법 (참고용 — 새 프로젝트에는 비권장)

```bash
cd "affinity_setting-source" && ./setup.sh   # -> affinity_setting 바이너리 생성
cd "../lpage_setting-source" && ./setup.sh   # -> lpage_setting 바이너리 생성
cd "../vm-param-fix" && ./setup.sh           # -> vm-param-fix 바이너리 생성 (power_setting은 이미 포함되어 있음)

# 세 도구를 vm-param-fix와 같은 디렉토리에 모아두고 실행
VC_PASSWORD='<비밀번호>' ./vm-param-fix -checkResult=<체크CSV> -vcTargetIP=<vCenter> -id=<계정> \
  -affinityTool=./affinity_setting -lpageTool=./lpage_setting -powerTool=./power_setting \
  -recheckTool=<vm-param-check 경로>
```

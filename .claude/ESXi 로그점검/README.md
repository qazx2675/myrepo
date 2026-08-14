# esxi-log-check

gossh로 수집한(또는 직접 수집하는) 다중 ESXi 호스트 로그를 `esxi_critical_patterns.yaml`
패턴 레지스트리로 매칭해서, 호스트별 CRITICAL/HIGH 하드웨어 장애 이벤트를 뽑아내는 도구.

## 가장 빠른 시작 (원샷 모드)

```bash
esxi-log-check -w hosts.txt
```

`hosts.txt`는 gossh `-w`에 쓰는 것과 완전히 같은 형식(호스트 한 줄에 하나, `#` 주석 가능)이다.
이 한 줄이면 내부적으로 gossh를 호출해서 `vobd.log`/`vmkernel.log`/`ipmi_sel`을 수집하고,
패턴 매칭 + aggregate 격상 + correlation_chains + 무증상 리부팅 탐지까지 전부 돌려서
리포트를 바로 stdout에 출력한다.

gossh 바이너리가 PATH에 없으면 `-gosshPath`로 경로를 지정한다:

```bash
esxi-log-check -w hosts.txt -gosshPath=/root/go-ssh-pack/gossh
```

## 빌드

```bash
go mod tidy   # gopkg.in/yaml.v3 다운로드 (인터넷 필요 — air-gapped면 vendor/ 미리 준비)
go build -o esxi-log-check .
```

## 옵션 전체 목록

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-w <hostfile>` | (없음) | **원샷 모드.** gossh `-w`와 동일한 형식의 호스트 목록 파일 하나만 지정하면 내부적으로 gossh를 호출해 `vobd.log`/`vmkernel.log`/`ipmi_sel`을 자동 수집하고 점검+리포트까지 실행. `-input`과 병행 가능(같은 source명은 `-input`이 우선). `-hostlist`를 따로 안 주면 이 파일을 hostlist로도 사용함. |
| `-gosshPath <path>` | `gossh` | `-w` 모드에서 내부적으로 호출할 gossh 바이너리 경로. PATH에 있으면 이름만으로 충분, 아니면 전체 경로(예: `/root/go-ssh-pack/gossh`) 지정. |
| `-tailLines <N>` | `300` | `-w` 모드에서 `vobd.log`/`vmkernel.log`를 각각 몇 줄 tail 할지. |
| `-input <source>=<file>` | (반복 지정) | **수동 모드.** 이미 수집해 둔 gossh 출력 파일을 소스별로 직접 지정. source는 `esxi_critical_patterns.yaml`의 각 패턴 `source:` 필드와 정확히 일치해야 함(예: `vobd.log`, `vmkernel.log`, `ipmi_sel`). 최소 `-w` 또는 `-input` 하나는 필요. |
| `-patterns <path>` | `esxi_critical_patterns.yaml` | 패턴 레지스트리 YAML 경로. |
| `-hostlist <path>` | (없음, `-w` 지정 시 그 값 사용) | gossh `-w`에 쓴 것과 같은 호스트 목록 파일. "무응답/미수집 호스트"(`[3-1]`) 계산에 씀. `-input`만 쓰는 수동 모드에서 무응답 탐지를 켜고 싶으면 명시적으로 지정. |
| `-format <text\|json>` | `text` | 출력 형식. `json`은 Splunk 등 후속 파이프라인 연동용(전체 Finding 필드 포함, 확정/의심/aggregate/체인 근거 다 들어있음). |
| `-out <path>` | (없음, stdout) | 출력 파일 경로. |
| `-onlyProblems` | `false` | text 리포트 `[1] 호스트별 요약`에서 완전히 정상(CRITICAL/HIGH/MEDIUM/LOW/SUSPECTED 전부 0)인 호스트는 개별 나열 대신 "N대 생략됨"으로 축약. 수십~수백 대 규모에서 유용. |
| `-silentRebootGap <duration>` | `30m` | `vmkernel.log` 타임스탬프 공백이 이 이상이면 무증상 리부팅(`CHAIN_SILENT_REBOOT`) 후보로 판단. Go duration 형식(`5m`, `10m`, `1h` 등). 환경별 로그 밀도에 따라 조정 필요 — 기본값 30분은 실측 중 스로틀링 로그(5분 간격) 오탐을 피하기 위해 올린 값. |
| `-noCorrelate` | `false` | `correlation_chains`(연쇄 이벤트 상관분석)와 무증상 리부팅 탐지를 끔. `aggregate`(빈도 기반 격상)는 이 플래그와 무관하게 항상 적용됨. |

## 사용 예시

**1. 원샷 모드 — 가장 간단, 대부분의 경우 이걸 쓰면 됨:**
```bash
esxi-log-check -w hosts.txt -gosshPath=/root/go-ssh-pack/gossh
```

**2. 원샷 모드 + 문제 있는 호스트만 보기 (수백 대 규모):**
```bash
esxi-log-check -w hosts.txt -onlyProblems -out report.txt
```

**3. 원샷 모드 + JSON 출력 (Splunk 연동):**
```bash
esxi-log-check -w hosts.txt -format json -out report.json
```

**4. 수동 모드 — 이미 수집해 둔 파일이 있을 때:**
```bash
mkdir -p collected
gossh -w hosts.txt -script "tail -300 /var/run/log/vobd.log"     > collected/vobd.txt
gossh -w hosts.txt -script "tail -300 /var/run/log/vmkernel.log" > collected/vmkernel.txt
gossh -w hosts.txt -script "localcli hardware ipmi sel list"     > collected/ipmi_sel.txt

esxi-log-check \
  -hostlist hosts.txt \
  -input vobd.log=collected/vobd.txt \
  -input vmkernel.log=collected/vmkernel.txt \
  -input ipmi_sel=collected/ipmi_sel.txt
```
(주의: gossh 자체 플래그는 원격 명령 문자열보다 **앞**에 와야 한다 — `-script "cmd"` 순서, `"cmd" -script`가 아님)

**5. 원샷 + 수동 혼합 — 기본 3종은 자동 수집하고, hostd.log만 수동으로 추가:**
```bash
gossh -w hosts.txt -script "tail -300 /var/run/log/hostd.log" > collected/hostd.txt
esxi-log-check -w hosts.txt -input hostd.log=collected/hostd.txt
```

**6. 무증상 리부팅 탐지를 더 민감하게(10분 공백부터), correlation은 끄고 aggregate만:**
```bash
esxi-log-check -w hosts.txt -silentRebootGap=10m -noCorrelate
```
(`-noCorrelate`를 켜도 aggregate는 계속 적용됨을 참고)

## 리포트 구조 (text 포맷)

1. `[0]` 전체 집계 — 대상 호스트 수(findings/수집실패/무응답 분해), 문제/정상호스트 수, 심각도별 합계
2. `[1]` 호스트별 요약 — 심각도 내림차순 정렬(`-onlyProblems`로 정상 호스트 축약 가능)
3. `[2]` CRITICAL/HIGH 하이라이트
4. `[2-1]` 의심(SUSPECTED) 항목 — `requires_prev_line_suffix` 조건 불충족(확정 아님)
5. `[3]` 수집 실패 호스트 (SSH 실패 등 — "크리티컬 0건"과 명확히 구분)
6. `[3-1]` 무응답/미수집 호스트
7. `[4]` 전체 상세 (NOISE 포함, 감사/튜닝용)

## 마일스톤 진행 현황

- **M0** — 패턴 리서치: `esxi_critical_patterns.yaml`(40+ 패턴) + `M0_pattern_research.md`
- **M1** — SSH Executor(파싱 전용) + Pattern Registry 골격. 실제 gossh 출력으로 검증 완료.
- **M2** — 다중 호스트 처리는 gossh 자체 워커풀에 위임(설계상 이미 충족)로 판단, Severity 리포트 강화(`[0]` 전체집계, 심각도 정렬, `-onlyProblems`).
- **M3** — `aggregate`(빈도 기반 격상), `correlation_chains`(연쇄 이벤트 상관분석 4종), 무증상 리부팅 탐지(`CHAIN_SILENT_REBOOT`), IPMI SEL 전용 타임스탬프 파서.
- **M4(이번)** — `-w` 원샷 모드: 내부적으로 gossh 서브프로세스를 호출해 수집부터 리포트까지 한 번에.

## 알려진 한계 / 확인 필요 (누적)

- gossh 실제 출력 포맷은 실제 ESXi 8.0.3 호스트로 검증했으나(`prefix_regex` 그대로 일치), SSH 실패 시 정확한 에러 문구는 환경마다 다를 수 있음.
- `aggregate`는 "조건을 만족시킨 순간의 특정 발생"이 아니라 "그 호스트의 해당 패턴 findings 전체"를 격상하는 단순화된 방식.
- `correlation_chains` 중 `condition`/`then` 타입(자유 텍스트, `CHAIN_FABRIC_WIDE_APD`/`CHAIN_SILENT_REBOOT`)은 YAML 스키마 일반화 대신 ID별로 Go 코드에서 특별 처리.
- 무증상 리부팅의 부팅 로그 판별 정규식은 VMware 공개 자료 기준 추정치 — 실제 재부팅 로그로 미검증(테스트 환경이 캡처 구간 중 재부팅 이력 없음).
- IPMI SEL 파서(`MM/DD/YYYY HH:MM:SS`)는 이 환경에 실 BMC 하드웨어가 없어 실측 원본 포맷 미검증 — 컬럼 위치 비의존 방식으로 리스크 완화했으나 재검증 권장.
- `-w` 모드는 개별 소스 gossh 실행 실패 시 그 소스만 건너뛰고 계속 진행하도록 설계됨(전체 gossh 부재 케이스로 확인됨). 특정 소스 하나만 실패하는 세부 케이스(예: vobd.log는 되고 ipmi_sel만 실패)는 코드 구조상 독립적으로 처리되지만 실측 재현 테스트는 아직 안 함.

패턴은 전부 YAML 외부화되어 있으므로, 새 케이스가 나오면 **재빌드 없이 YAML만 수정**하면 된다.

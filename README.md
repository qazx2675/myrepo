# bios_compare — 실제조사 vs 웹조사(bios_json) BIOS 설정이름 비교

폐쇄망 장비에서 실제 수집한 BIOS JSON(`out/<일시>/<hostname>.json`, [collect](../) 도구 결과)의 **설정이름**과 웹에서 조사한 `bios_json/` 의 설정이름이 같은지 **양방향**으로 비교합니다. 이름이 있는지만 보며 값·허용값은 비교하지 않습니다. 흐름은 [WORKFLOW.md](WORKFLOW.md) 참고.

## 1. 빌드 및 설치

표준 라이브러리만 쓰는 Go 프로그램이라 받을 의존성이 없고 폐쇄망에서 그대로 빌드됩니다(`vendor/` 에 복사할 패키지 없음). `setup.sh` 는 `GOPROXY=off` + `-mod=vendor` 로 인터넷 접속을 하지 않습니다.

```bash
git clone -b bios-compare <저장소 URL>     # 또는 폴더째 다운로드 후 반입
cd <저장소>
bash setup.sh        # 오프라인 빌드 -> ./bios_compare (Go 1.20 이상)
```

Go 가 없으면 저장소에 포함된 빌드 완료 바이너리 `bios_compare`(RHEL8/Rocky, 정적)를 그대로 쓰면 됩니다.

## 2. 사용 방법

```bash
# 1) 실제조사 JSON 을 out/ 아래에 넣는다  (예: out/20261008_155548/node001.json)
# 2) list.txt 작성 (hostname [model], 한 줄에 하나)
cp list.txt.example list.txt && vi list.txt
# 3) 실행
bash 조사.sh         # -> results/<일시>/
```

`list.txt` 형식: `hostname model` — model 은 **bios_json 폴더 이름 기준**입니다. 요약 폴더의 일부도 됩니다(예: `DL360_G9` 는 `DL360_G9_XL170R_G9_XL250_G9_XL270D_G9` 폴더를 참조). 모델을 생략하면 JSON 의 `Model` 값(`ProLiant DL360 Gen10`→`DL360_G10`, `PowerEdge R640`→`R640`)으로 자동 판단하고 `mapping.tsv` 에 출처(`JSON자동`)를 표시합니다.

### 판정 규칙
- **기준은 list.txt** 입니다. list.txt 에 없는 호스트·모델은 건너뜁니다.
- list.txt 모델이 bios_json 에 없으면 `없음`, 폴더는 있는데 속성 파일(`bios_attributes.json`/`bios_tokens.json`)이 없으면 `조사파일없음` 으로 기록합니다.
- 같은 모델의 여러 호스트는 설정이름을 합집합으로 모으고 `보유호스트`(예: `3/5`)를 적습니다.
- 이름 상태: `일치`(양쪽에 있음) / `실제만`(실제조사에만) / `조사만`(bios_json 에만). 대소문자·공백·`_`·`-` 만 다른 이름은 `유사이름` 열에 힌트로 표시합니다(판정은 정확히 같은 이름만 일치).
- 시리얼·MAC·UUID 등 고정적이지 않은 항목은 `ignore.txt` 정규식으로 제외하며 개수만 기록합니다.

## 3. 옵션별 상세 설명

| 옵션 | 기본값 | 설명 |
|---|---|---|
| `-list` | `list.txt` | hostname [model] 목록 |
| `-in` | `./out` 아래 최신 폴더 | 실제조사 JSON 폴더 |
| `-bios-json` | `bios_json` | 웹 조사 폴더 |
| `-ignore` | `ignore.txt` | 제외 정규식 목록 (없으면 내장 기본값) |
| `-out` | `results` | 결과 폴더 |

결과 (`results/<일시>/`): `summary.txt`(모델별 일치/실제만/조사만/제외 개수, 건너뜀 목록), `mapping.tsv`(호스트→모델→폴더), `<모델>.tsv`(이름·상태·보유호스트·유사이름).

### 문서별 설명
| 문서 | 내용 |
|---|---|
| [사용법.txt](사용법.txt) | 수동 실행 명령 모음 |
| [ARCHITECTURE.md](ARCHITECTURE.md) | 파일별 역할 |
| [WORKFLOW.md](WORKFLOW.md) | 작업 흐름도 |
| [CHANGELOG.md](CHANGELOG.md) | 변경 이력 |
| [PR_CHECKLIST.md](PR_CHECKLIST.md) | 수정 전 체크리스트 |

## 주의사항 (Disclaimer)

> 본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.

- 이 도구는 읽기 전용입니다. 이 결과를 근거로 설정을 변경하는 작업을 한다면, 변경 후 **랜덤한 서버 몇 대를 직접 확인**해 실제로 변경되었는지 확인하십시오.
- `조사만` 이 많은 것은 오류가 아닐 수 있습니다. HPE Gen10 조사는 iLO 5 문서 샘플(401개), Dell 은 14G 전체 합집합 등 **조사 자료의 범위가 모델과 정확히 일치하지 않습니다**. Cisco `bios_tokens.json` 은 UCSM 표기라 Redfish 이름과 다를 수 있습니다.
- 모델 자동 판단은 HPE/Dell/Lenovo 표기 규칙 기준입니다. `모델판단불가`·`없음` 이면 list.txt 에 폴더 이름 기준으로 모델을 적으십시오.
- `out/`, `results/` 는 실제 장비 데이터라 저장소에 커밋하지 않습니다(`.gitignore`).

## 5. 전역 명령어로 사용하기 (선택 사항)

빌드된 실행 파일을 PATH 환경 변수에 포함된 디렉터리로 이동하거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 사용할 수 있습니다.

```bash
sudo cp bios_compare /usr/local/bin/bios_compare
bios_compare -list list.txt -in out/20261008_155548 -bios-json /opt/bios_compare/bios_json
```

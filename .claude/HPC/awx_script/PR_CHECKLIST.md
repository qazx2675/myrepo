# PR_CHECKLIST

AWX 노드정보 V2 스크립트 풀리퀘스트 검증 항목. 모든 항목을 확인하고 체크한 후 커밋/푸시하세요.

## 빌드 및 문법 검사

- [ ] `bash -n 01.AWX_nodeinfo_V2.sh` 에러 없음
- [ ] `bash -n 02.source_dhcp_pxe.sh` 에러 없음
- [ ] `bash -n test/run_tests.sh` 에러 없음
- [ ] `shellcheck -S warning 01.AWX_nodeinfo_V2.sh 02.source_dhcp_pxe.sh` 경고 0건
  - 원문 보존 블록(git 업로드 부분)은 `# shellcheck disable=SC2006,…` 로 마크됨

## 테스트 통과

- [ ] `bash test/run_tests.sh` 모든 케이스 통과 (PASS 집계)
  - 케이스 목록은 `bash test/run_tests.sh` 출력의 케이스별 결과표 참고. 모두 PASS, FAIL 0
  - 환경: 랩 Rocky 8.10, bash 4.4 (또는 RHEL 7+)

## 실제 시나리오 검증

- [ ] 최소 1가지 실제 또는 목업 시나리오:
  - **대상**: 호스트 10대 이상, 2개 이상 infra, 섞인 os/boot
  - **검증**:
    - [ ] `${user}.txt` 가공(msg 파싱, MAC 보정, sed 3줄 치환) 정상
    - [ ] 다단 출력(20개씩 세로 → 가로) 정상
    - [ ] 분할 그룹 수 = (infra,nic,disk,용량,os,boot,splunk) 고유 조합 수
    - [ ] yml 수집 = 그룹 수 + 1(all)
    - [ ] 02 호출 시 그룹 yml 만 인자로 전달 (all 제외)
    - [ ] 02 옵션 확인표에서 모든 infra/os/boot/splunk 값 표시
    - [ ] 02 N 선택 후 수동 수정 반영 정상
    - [ ] dhcp_pool 줄 형식: `tmp tmp SEC $4 $5 $6 eth0 sda sda5 960 7.9`
    - [ ] LDAP/LACP 점검:
      - [ ] LDAP 동일 → "모든 호스트의 LDAP이 …으로 동일함" (1줄)
      - [ ] LDAP 상이 → 값별 호스트 나열 (N줄)
      - [ ] LACP 혼재 → 호스트명 나열 + lacp_comment
      - [ ] 응답 없음 → "응답 없음 : 호스트명 …"
    - [ ] LOG 파일 생성: `LOG/${user}.log` 에 단계별 타임스탬프
    - [ ] 로컬 분할 파일 잔여 없음 (${user}_*.yaml)
    - [ ] mktemp 파일/디렉터리 정리됨

## 문서 갱신

- [ ] **CHANGELOG.md**: 2026-10-02 항목에 변경 사항 반영
  - 신규 기능, 보정사항, 주의사항 기재
- [ ] **README.md**: 
  - 최상단 변수 7개 설명 및 샘플값 포함
  - 사용 방법 단계별 예 포함
  - 옵션 상세 설명 갱신
  - user() 함수 내용 변경 시 수정
  - 주의사항(Disclaimer) 갱신
- [ ] **ARCHITECTURE.md**: 변경 영향 있는 함수/단계 갱신
- [ ] **WORKFLOW.md**: 흐름 변경 시 mermaid 갱신
- [ ] **workflow.svg**: 흐름 변경 시 이미지 갱신

## 코드 규칙

- [ ] **01 최상단 변수** 7개 검증:
  ```bash
  repohost=""
  svr_dir=""
  ai_server_list=""
  ldap_check_script=""
  lacp_comment=""
  inventory_delete_host=""
  infra_alias=""
  ```
  - 모두 `=""` 상태 (테스트값 포함 금지)
  
- [ ] **user() 함수** 확인:
  ```bash
  user(){
  user_route=""
  bash $user_route/info_mn.sh
  read -p "Input Number: " user_choice
  user=`bash $user_route/info.sh $user_choice`
  }
  ```
  - `user_route=""` 가 비어 있음 (테스트 값 포함 금지)

- [ ] **sed 3줄** 원문 유지:
  ```bash
  sed -i 's/1.1T/1200/g' ${user}.txt
  sed -i 's/7T/7600/g' ${user}.txt
  sed -i 's/test1234/offchip/g' ${user}.txt
  ```

- [ ] **git 블록** 원문 보존 (svr_idr 별칭 + cd 복귀):
  ```bash
  now_pwd=`pwd`
  for x in `echo $yaml`
  do
    cd /root/user/${user}/myrepo
    cp $svr_idr/$x /root/user/${user}/myrepo/$x
    bash .git_upload.sh ${user} $x
  done
  cd "$now_pwd"
  ```

## 주의사항 최종 확인

- [ ] 로컬 실행(Windows Git Bash) 검증 완료
- [ ] 02 단독 실행 호환 확인 (인자 없을 때 대화형)
- [ ] EOF 입력 처리 (무한 루프 없이 오류 종료)
- [ ] Ctrl+C 중단 시 mktemp 정리 확인 (trap 동작)
- [ ] 상대경로/절대경로 혼용 여부 검토

## 커밋 메시지 양식

```
feat(awx-nodeinfo): 01/02 스크립트 + 문서 추가

- 01.AWX_nodeinfo_V2.sh: 노드정보 수집·분할·yml 생성
- 02.source_dhcp_pxe.sh: yml별 인벤토리 자동 등록
- test/run_tests.sh: 실제 실행 검증 하네스
- 문서: README/CHANGELOG/ARCHITECTURE/WORKFLOW/체크리스트
```

## 최종 체크 (커밋 전)

```bash
# 1. 빌드 검사
bash -n 01.AWX_nodeinfo_V2.sh 02.source_dhcp_pxe.sh test/run_tests.sh
shellcheck -S warning 01.AWX_nodeinfo_V2.sh 02.source_dhcp_pxe.sh

# 2. 테스트
bash test/run_tests.sh  # 랩 환경에서 실행

# 3. 파일 검증
grep -n '^repohost=""' 01.AWX_nodeinfo_V2.sh  # 확인: 모두 비어 있음
grep -n '^svr_dir=""' 01.AWX_nodeinfo_V2.sh

# 4. user() 함수 확인
sed -n '/^user() {/,/^}$/p' 01.AWX_nodeinfo_V2.sh  # 확인: user_route="" 인지

# 5. git 커밋
git status  # 변경 파일 확인
git diff 01.AWX_nodeinfo_V2.sh  # 최상단 변수 7개 + user() 만 확인
git add .
git commit -m "feat(awx-nodeinfo): …"
git push origin master
```


- [ ] **02 상단 변수 2개** `dhcp_infra_alias=""`, `pxe_infra_alias=""` 가 비어 있는지 확인 (현장 값 커밋 금지)

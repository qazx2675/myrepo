# affinity-del (임시 도구)

list.txt 의 VM 에서 ExtraConfig 의 sched.vcpuN.affinity 설정만 삭제합니다. 다른 설정은 건드리지 않습니다.

    ./run.sh -vc <vCenter> -dry-run   # 삭제 예정 목록만 출력
    ./run.sh -vc <vCenter>            # 실제 삭제

- 비밀번호: VC_PW 환경변수, 없으면 프롬프트. 계정은 -id (기본 administrator@vsphere.local)
- 빌드: vendor/ 만 사용 (오프라인). 첫 실행 시 run.sh 가 go build 수행
- 없는 VM 은 실패로 표시하고 계속 진행, 설정이 없는 VM 은 SKIP

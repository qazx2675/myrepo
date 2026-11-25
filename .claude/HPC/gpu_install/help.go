package main

// helpText 는 `bash gpu_install.sh -h` (또는 gpu_install_os8 -h) 가 출력하는 사용법이다.
const helpText = `gpu_install - 여러 GPU 서버에 NVIDIA 드라이버(.run)와 CUDA 에 맞는 DCGM 4 를 한 번에 설치합니다 (gossh/scp, 폐쇄망용)

사용법: bash gpu_install.sh [autofs경로] [list파일] [-p 동시대수]
  예) bash gpu_install.sh                                  (AUTOFS_PATH 와 ./list 사용, 10대씩)
  예) bash gpu_install.sh /autofs/nvidia ./list -p 5       (경로·list·동시 대수 지정)
  예) bash gpu_install.sh /autofs/nvidia fail_list.retry   (실패한 호스트만 다시)

[준비]
  1. gpu_install.sh 와 gpu_install_os8 을 같은 폴더에 두고 관리 서버에서 root 로 실행합니다.
  2. gpu_install.sh 맨 위 변수를 채웁니다.
       AUTOFS_PATH  .run 파일이 있는 autofs 디렉토리 (인자로 주면 인자가 우선)
       MOUNT_USER   마운트 판정 계정. 대상에서 /user/<계정> 이 붙으면 mount 방식, 아니면 scp 방식 (비우면 전부 scp)
       GOSSH        gossh 경로 (비우면 PATH 에서 찾음)
  3. 설치 파일 이름은 <autofs경로>/NVIDIA-Linux-x86_64-<버전>.run 이어야 합니다.
  4. 대상 서버: 관리 서버에서 root 로 키 접속, 현재 커널의 kernel-devel 과 gcc,
     DCGM rpm 이 있는 /etc/yum.repos.d/rhel.repo (baseurl=file://..., enabled=0)

[list 파일]  한 줄에 '호스트 [드라이버버전]', # 뒤는 주석
    gpu01                 버전 없음 → 실행할 때 .run 목록을 보여 주고 한 번만 묻고, 버전 없는 줄 전체에 적용
    gpu02 570.172.08      이 호스트는 이 버전으로 설치

[옵션]
  autofs경로   .run 파일 디렉토리 (기본 AUTOFS_PATH)
  list파일     설치 대상 목록 (기본 ./list)
  -p <N>       동시 처리 대수 (기본 10)
  -h           이 도움말

[진행 순서]  묻는 곳은 2·4 두 번뿐입니다
  1. 버전 확인     버전 없는 줄은 한 번 질문, list 의 버전마다 .run 파일이 있는지 확인
  2. 시작 경고     빨간 경고와 대상 목록을 보여 주고 y 를 입력해야 진행 (작업 가능한 장비인지 확인)
  3. 현황 수집     gossh 로 hostname, 현재 드라이버·CUDA, GPU, kernel-devel·gcc, nouveau, 마운트, DCGM 상태 조사
  4. 재설치 확인   이미 설치된 서버를 'hostname 현재버전 → 요청버전' 으로 보여 주고
                   a(전체 재설치) / s(골라서) / n(건너뜀). 같은 버전은 기본 건너뜀(DCGM 이 비정상이면 DCGM 만 복구)
  5. 드라이버 설치 mount: 대상에서 sh <autofs경로>/<run> -s
                   scp  : .run 을 대상 /tmp/gpu_install 로 보내 실행 후 삭제
                   실패하면 nvidia-dcgm·persistenced·fabricmanager 중지, nvidia 모듈 rmmod 후 1회 재시도
  6. DCGM 설치     nvidia-smi 의 CUDA 12/13 에 맞는 datacenter-gpu-manager-4 패키지 4종 (cuda12↔13 바뀌면 교체)
                   mount: rhel.repo 를 설치하는 동안만 enabled=1 (성공·실패·중단 모두 0 으로 복구)
                   scp  : rhel.repo baseurl 과 같은 관리 서버 경로에서 rpm 과 부족한 의존성 rpm 을 보내 설치
                   CUDA 가 12/13 이 아니면 DCGM 은 건너뜀. 끝나면 nvidia-dcgm enable·start, dcgmi 로 GPU 확인
  7. rc.local      드라이버·DCGM 까지 성공한 서버에서 /etc/rc.local 실행 (없으면 건너뜀, 실행 권한 없으면 bash 로,
                   최대 10분. 실패하면 'rc.local failed(rc=N)' 로 fail)
  8. 결과 출력     HOST | 방식 | 이전→설치버전 | CUDA | DCGM | dcgm상태 | 결과

[결과 파일]  실행한 현재 디렉토리에 생깁니다
  logs/<날짜시각>/<호스트>.log   호스트별 원격 출력 전체
  fail_list                      '호스트 요청버전 사유'
  fail_list.retry                '호스트 버전' (그대로 list 로 다시 실행)

[종료코드]
  0 모두 성공   1 실패한 호스트가 있거나 오류   2 gpu_install.sh 인자 오류

[주의]
  재부팅하지 않습니다. fabricmanager(NVSwitch)는 자동 설치하지 않고 경고만 합니다.
  모듈을 못 내리면 점유 프로세스(lsof)를 로그에 남기고 fail 처리하며, 프로세스를 kill 하지 않습니다.
`

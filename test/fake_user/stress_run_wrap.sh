#!/bin/bash
# stress_run.sh 메뉴 검증: USER_ROUTE 를 fake_user 로 바꾼 사본 + 인자만 출력하는 가짜 stress-test.
# 사용: bash test/fake_user/stress_run_wrap.sh   (입력은 아래 케이스에 고정)
cd "$(dirname "$0")/../.." || exit 2
W=/root/work/st-wrap; rm -rf $W; mkdir -p $W
sed "s|^USER_ROUTE=\"\"|USER_ROUTE=\"$PWD/test/fake_user\"|" stress_run.sh > $W/stress_run.sh
printf '#!/bin/bash\necho "FAKE stress-test args: $*"\n' > $W/stress-test; chmod +x $W/stress-test
echo "--- case1: user=2, 잘못된 단계 c, 빈값, aa, ab 순"
printf '2\nc\n\naa\nab\n' | bash $W/stress_run.sh $PWD/test/hosts_c.txt -retry 1 -er x.txt; echo "[exit=$?]"
echo "--- case2: 잘못된 user 번호 9"
printf '9\nb\n' | bash $W/stress_run.sh $PWD/test/hosts_c.txt; echo "[exit=$?]"
echo "--- case3: 입력파일 없음"
bash $W/stress_run.sh /nonexistent; echo "[exit=$?]"

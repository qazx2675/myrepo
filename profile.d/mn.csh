# /etc/profile.d/mn.csh - mn(디렉터리 안내) 의 cd 연동 (csh/tcsh)
#
# 스크립트는 부모 쉘의 현재 디렉터리를 바꿀 수 없으므로,
# mn 이 "$HOME/.mn_cd" 에 쓴 `cd "<경로>"` 한 줄을 mn 실행 직후 source 한다.
# mn 은 실행할 때마다 이 파일을 먼저 빈 파일로 다시 만들기 때문에,
# mn 이 오류로 끝나도 source 는 (빈 파일을) 정상 실행한다.
#
# 인자 처리: \!* 는 히스토리 치환으로 "입력한 단어 그대로"(따옴표 포함)를 다시 끼워 넣는다.
# 따라서 mn add HPC name "공백 있는 설명" 처럼 사용자가 따옴표를 직접 입력하면 유지된다.
# 단, 따옴표 없이 공백이 든 값은 csh 가 단어로 나눈다.
alias mn '/usr/local/bin/mn \!* ; source $HOME/.mn_cd'

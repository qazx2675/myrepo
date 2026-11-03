# /etc/profile.d/mn.sh - mn(디렉터리 안내) 의 cd 연동 (bash)
#
# 스크립트는 부모 쉘의 현재 디렉터리를 바꿀 수 없으므로,
# mn 이 "$HOME/.mn_cd" 에 쓴 `cd "<경로>"` 한 줄을 mn 실행 직후 source 한다.
# (mn 은 실행할 때마다 이 파일을 빈 파일로 다시 만든다.)
if [ -n "$BASH_VERSION" ]; then
    mn() { /usr/local/bin/mn "$@"; . "$HOME/.mn_cd"; }
fi

#!/bin/bash
# 가짜 custom_inventory.sh: 인자 yaml 의 줄 수로 <이름>_<N>ea.yml 을 SVR_DIR 에 만든다.
SVR_DIR=${SVR_DIR:-/root/work/st-svr}
y=$1
n=$(grep -c . "$y")
b=$(basename "$y" .yaml)
mkdir -p "$SVR_DIR"
cp "$y" "$SVR_DIR/${b}_${n}ea.yml"
echo "custom_inventory: $SVR_DIR/${b}_${n}ea.yml"

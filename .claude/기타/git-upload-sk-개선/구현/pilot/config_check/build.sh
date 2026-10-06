#!/bin/bash
# build.sh - 파일럿(config_check)의 setup/ 산출물을 틀(templates/)로 다시 만든다 (개발자용)
#   setup/ 에는 vars.manifest, setup_guide.sh(자체포함), update_v<버전>.sh(자체포함) 만 둔다.
#   사용: bash build.sh [--version 1.1.0]
HERE=$(cd "$(dirname "$0")" && pwd)
TPL="$HERE/../../templates"
VER=1.1.0
[ "$1" = "--version" ] && VER=$2
cd "$HERE" || exit 2
rm -f setup/*
cp vars.manifest setup/vars.manifest || exit 2
bash "$TPL/make_guide.sh" --out setup/setup_guide.sh || exit $?
bash "$TPL/make_update.sh" --old old/config_check.sh --new new/config_check.sh \
    --manifest vars.manifest --version "$VER" --file config_check.sh \
    --out "setup/update_v${VER}.sh" --yes || exit $?
ls -l setup

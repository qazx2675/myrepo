#!/bin/bash
# 가짜 .git_upload.sh: 인자만 로그에 남긴다.
echo "$(date +%T) git_upload args: $*" >> "${GIT_LOG:-/root/work/st-git/git_upload.log}"

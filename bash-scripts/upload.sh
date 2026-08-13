#!/bin/bash
# upload.sh - 다중 git 계정(멀티 유저) push 스크립트
# 사용자별 credential/원격 저장소를 선택해 안전하게 push
# 사용법: bash upload.sh

echo "사용할 git 계정을 선택하세요:"
echo "  1) user1"
echo "  2) user2"
read -p "번호 입력: " choice

case "$choice" in
    1)
        git_user="user1"
        git_email="user1@example.com"
        ;;
    2)
        git_user="user2"
        git_email="user2@example.com"
        ;;
    *)
        echo "잘못된 선택입니다."
        exit 1
        ;;
esac

git config user.name "$git_user"
git config user.email "$git_email"

read -p "커밋 메시지를 입력하세요: " commit_msg
[ -z "$commit_msg" ] && commit_msg="update"

git add -A
git commit -m "$commit_msg"

branch=$(git rev-parse --abbrev-ref HEAD)
echo "현재 브랜치: $branch 로 push 합니다."
git push origin "$branch"

echo "push 완료 (계정: $git_user)"

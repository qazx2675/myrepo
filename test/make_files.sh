#!/bin/bash
# 테스트 파일 생성: test/make_files.sh <출력폴더> <chunk_size(base64 문자)>
# 경계 파일은 비압축(난수)이라 base64 길이가 정확히 계산된다: 원본 3 bytes = base64 4자.
set -eu
OUT="$1"; CS="$2"
RAW=$(( CS / 4 * 3 ))   # 조각 1개에 정확히 들어가는 원본 크기
rm -rf "$OUT"; mkdir -p "$OUT"; cd "$OUT"
: > t00_empty.txt
printf 'x' > t01_one_byte.txt
head -c $((RAW - 1)) /dev/urandom > t02_edge_minus1.bin
head -c $RAW        /dev/urandom > t03_edge_exact.bin
head -c $((RAW + 1)) /dev/urandom > t04_edge_plus1.bin
head -c $((RAW * 3)) /dev/urandom > t05_chunk_x3.bin
head -c 1048576     /dev/urandom > t06_1MiB.bin
head -c 10485760    /dev/urandom > t07_10MiB.bin
printf 'a\tb\tc\r\nline2\n한글 줄: 값 # 주석 - '"'"'single'"'"' "double" {a: 1} [x, y]\nkey: value\n- item\n마지막 줄바꿈 없음' > "t08_special: #-'{}[] 한글.txt"
printf 'id,이름,메모\r\n1,"홍길동","a, b"\r\n2,김철수,"say ""hi"""\r\n3,탭\t포함,"줄\n바꿈"\r\n' > t09_data.csv
for i in $(seq 1 30000); do printf 'col1\tcol2\t한글 %d\r\n' "$i"; done > t10_crlf_tabs.txt
sha256sum -- * > ../SHA256SUMS.$(basename "$OUT")
ls -l

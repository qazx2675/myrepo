# AWX variables 크기 한도 측정

대상: AWX v24.6.1, http://192.168.0.58:30080, 인벤토리 ID 5.
측정일: 2026-11-17.

## 1. nginx 계층 (측정 완료, 인증 없음)

`PATCH /api/v2/inventories/5/` 를 **인증 없이** 보냈다.
인증이 없으므로 AWX 는 아무것도 바꾸지 않는다 (401).
nginx 가 본문을 먼저 검사하므로 본문 한도만 잴 수 있다.
본문은 `{"variables": "awx_copy_probe: \"<base64>\""}` 이다. 크기는 base64 문자 수다.

| base64 크기 | HTTP | 응답 시간(초) | timeout/무응답 | 해석 |
|---|---:|---:|---|---|
| 64 KiB | 401 | 0.017 | 없음 | nginx 통과, 인증 거부 |
| 128 KiB | 401 | 0.035 | 없음 | nginx 통과 |
| 256 KiB | 401 | 0.017 | 없음 | nginx 통과 |
| 512 KiB | 401 | 0.015 | 없음 | nginx 통과 |
| 1 MiB | 401 | 0.025 | 없음 | nginx 통과 |
| 2 MiB | 401 | 0.026 | 없음 | nginx 통과 |
| 4 MiB | 401 | 0.037 | 없음 | nginx 통과 |
| 8 MiB | 413 | 0.001 | 없음 | nginx 거부 (Request Entity Too Large) |
| 16 MiB | 413 | 0.000 | 없음 | nginx 거부 |
| 32 MiB | 413 | 0.001 | 없음 | nginx 거부 |
| 64 MiB | 413 | 0.001 | 없음 | nginx 거부 |

이진 탐색 결과: **본문 5,242,880 bytes(=5 MiB) 까지 통과, 5,242,881 bytes 부터 413.**
즉 nginx `client_max_body_size 5m` 이다.

## 2. 인증 후 AWX(Django) 계층 (미측정)

test.kim 비밀번호가 작업 환경에 전달되지 않아 인증 PATCH 측정을 하지 못했다.
이 부분은 **미완료**다. 아래 명령으로 측정한다 (끝나면 원본으로 복원한다):

```bash
./awx_copy --measure-limits --measure-max-mib 64 | tee docs/limits_auth.txt
```

출력: 64 KiB 부터 2배씩 PATCH → 실패하면 이진 탐색 → 크기, HTTP 코드, 응답 시간,
timeout 여부, 저장 후 GET 일치 여부 표. 마지막에 원복 일치 여부를 출력한다.
nginx 한도 때문에 결과는 5 MiB 이하로 나온다.
Django `DATA_UPLOAD_MAX_MEMORY_SIZE`(기본 2.5 MB) 가 적용되면 더 작을 수 있다.

## 3. chunk_size 기본값

- 기본값: **1,048,576 (1 MiB, base64 문자)**.
- nginx 한도 5 MiB × 0.5 = 2.5 MiB 이하. 조건 충족.
- Django 기본 한도 2.5 MB 가 적용돼도 × 0.5 = 1.25 MB 이하. 조건 충족.
- PATCH 본문 = chunk + backup_b64(원본 variables 의 base64) + 약 1 KB.
  원본 variables 가 크면 chunk_size 를 줄인다. 본문 예상치가 5 MiB 를 넘으면 업로더가 경고한다.
- 인증 측정 후 `측정 한도 × 0.5` 가 1 MiB 보다 작게 나오면 기본값을 낮춰야 한다.

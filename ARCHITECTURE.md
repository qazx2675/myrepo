# ARCHITECTURE

```
브라우저/앱창 (Three.js 프론트)  <-- REST -->  Go 서버 (127.0.0.1)  <--> ./data/*.json|csv
```

| 계층 | 위치 | 역할 |
|---|---|---|
| 프론트 | `index.html`, `js/`, `css/`, `vendor/` | 3D/2D 렌더링, 상태(store), JS 검증기 |
| 임베드 | `internal/webui` | `dist/` 를 `//go:embed` 로 내장 (빌드 시 루트에서 복사) |
| 서버 | `internal/server` | 정적 파일 + `/api/bootstrap`, `/api/save`, `/api/export/allocated-csv`, `/api/csv/validate` |
| 저장 | `internal/storage` | tmp 파일 + rename 원자적 저장 |
| 검증 | `internal/csvproc` | `validator.js` 의 Go 포팅 (센터 접두사, 48U 규칙, 충돌, 랙 자동 생성) |
| 실행 | `internal/launcher`, `cmd/datacenter-viewer` | Edge/Chrome `--app` 창 → 폴백 기본 브라우저 |

## 설계 원칙
- 저장 형식은 프론트 `store.exportLayoutJson()` / `exportDevicesCsv()` 그대로 (Go 는 불투명하게 저장).
- 프론트는 `/api/bootstrap` 의 `goBackend:true` 응답으로 Go 모드를 감지하고, 없으면 기존 브라우저 모드로 동작 (하위 호환).
- 검증 규칙의 기준(SSOT)은 `js/state/validator.js`. 규칙을 바꾸면 `internal/csvproc/allocator.go` 와 테스트도 함께 수정.

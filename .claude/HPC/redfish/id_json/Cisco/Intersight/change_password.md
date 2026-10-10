# Cisco Intersight (IMM, X210c M7) - 기존 BMC 계정 패스워드 변경

## 방법 A. GUI
해당 Local User 정책 편집 > 사용자 암호 재입력(기본 `admin` 도 정책에 `admin` 사용자 + admin 역할로 추가해야 암호 변경 가능) > 저장 > 연결된 서버 프로파일 Deploy.

## 방법 B. REST
Ansible `always_update_password: true` 의 동작을 그대로 따른다 (API 는 암호를 반환하지 않아 비교 불가).
1. `GET /iam/EndPointUserPolicies?$filter=Name eq '<POLICY_NAME>'&$expand=EndPointUserRoles($expand=EndPointRole,EndPointUser)`
2. 대상 사용자의 `EndPointUserRoles` 항목 삭제 (`DELETE /iam/EndPointUserRoles/<MOID>`)
3. 새 `Password` 로 `POST /iam/EndPointUserRoles` (create_account.md 5단계 body)
4. 서버 프로파일 Deploy (unverified).
PATCH `{"Password":"<NEW_PASSWORD>"}` 로 `EndPointUserRole` 의 암호만 갱신하는 방법은 소스에서 확인 못함(unverified) - 필요 시 먼저 시험.

## Ansible
`intersight_local_user_policy` 에 새 `password` 를 넣고 **`always_update_password: true`** (기본 false 라면 사용자가 새로 만들어질 때만 암호 적용).

## 이전 암호 재사용 금지
정책 `PasswordProperties.PasswordHistory` 0~5 (기본 5). 최근 5개 암호와 같은 값은 거부될 수 있음. 암호 규칙: 8~20자, 사용자명 불가, 4종 중 3종.

## 주의
- admin 암호를 바꾸려면 정책에 admin 을 넣으면 되며, 정책 미적용 시 FI admin 암호가 서버 vKVM 에서 통하지 않는다는 커뮤니티 설명 있음 (unverified).

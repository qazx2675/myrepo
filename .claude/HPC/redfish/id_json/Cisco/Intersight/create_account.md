# Cisco Intersight (IMM, X210c M7) - 새 BMC 계정 등록

플레이스홀더만 사용. 요청은 Intersight API Key 로 서명(HTTP Signature) 필요 - `curl` 직접보다 SDK/Ansible/`intersight-powershell` 권장.

## 방법 A. Intersight GUI
Policies > Create Policy > UCS Server > **Local User** 정책 생성 (Enforce Strong Password, Password History, 사용자 추가: 이름/역할/암호) -> 서버 프로파일(UCS Server Profile) 의 Management Configuration 에 연결 -> Deploy. KVM/접근 IP 는 **IMC Access** 정책(OOB 또는 Inband+VLAN, IP Pool).

## 방법 B. REST API (`https://intersight.com/api/v1`, 온프렘 어플라이언스는 해당 FQDN)
순서는 Ansible `intersight_local_user_policy` 소스 그대로.
1. 조직 Moid: `GET /organization/Organizations?$filter=Name eq 'default'&$select=Moid`
2. 정책: 
```
POST /iam/EndPointUserPolicies
{"Name":"<POLICY_NAME>","PasswordProperties":{"EnforceStrongPassword":true,"EnablePasswordExpiry":false,"PasswordHistory":5},
 "Organization":{"Moid":"<ORG_MOID>"}}
```
3. 사용자: `POST /iam/EndPointUsers {"Name":"<NEW_USER>","Organization":{"Moid":"<ORG_MOID>"}}` (이름 16자 이하)
4. 역할: `GET /iam/EndPointRoles?$filter=Name eq 'admin' and Type eq 'IMC'` (admin | readonly | user)
5. 연결:
```
POST /iam/EndPointUserRoles
{"EndPointUser":{"Moid":"<USER_MOID>","ObjectType":"iam.EndPointUser"},
 "EndPointRole":[{"Moid":"<ROLE_MOID>","ObjectType":"iam.EndPointRole"}],
 "Password":"<NEW_PASSWORD>","Enabled":true,
 "EndPointUserPolicy":{"Moid":"<POLICY_MOID>","ObjectType":"iam.EndPointUserPolicy"}}
```
 IPMI 계정이면 body 에 `"AccountTypes":[{"ObjectType":"iam.AccountTypeIpmi","ClassId":"iam.AccountTypeIpmi"}]` 추가 (Ansible 소스). 
6. 서버 프로파일에 정책 연결 + Deploy (unverified).

## 방법 C. Ansible
```yaml
- cisco.intersight.intersight_local_user_policy:
    api_private_key: "{{ api_private_key }}"
    api_key_id: "{{ api_key_id }}"
    name: <POLICY_NAME>
    local_users:
      - username: <NEW_USER>
        role: readonly          # admin | readonly | user
        password: "{{ vault_new_password }}"
```
`enforce_strong_password` 기본 true, `password_history` 기본 5, `purge` 기본 false.

## 제약
README 의 정책 절 (8~20자, 4종 중 3종, 사용자명 16자 이하, admin 은 이미 존재).

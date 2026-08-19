# clipSend

폐쇄망 환경에서 AI 검색 결과(코드, 명령어 등)를 신속하게 메일로 발송하는 오버레이 기반 Android 애플리케이션.

## 개요

회사의 폐쇄망 환경에서 AI 검색을 통해 얻은 코드나 명령어를 이메일로 전송할 때, 일일이 메일 앱을 켜고 작성해야 하는 불편함을 해결하기 위해 개발됨.

**핵심 기능**:
- 항상 켜져 있는 작은 오버레이 버튼
- 텍스트 붙여넣기 → 자동 발송
- 첫 줄을 자동으로 메일 제목으로 설정
- 여러 수신인 지원 (콤마 구분)

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우에는 설정변경후 랜덤한 서버 몇개를 확인해서 실제로 변경되었는지 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

### 1.1 요구사항
- Android Studio (Giraffe 이상 권장)
- JDK 17 이상
- Android SDK (API 34 이상)
- Gradle 8.x

### 1.2 빌드 단계

```bash
# 1. 프로젝트 디렉토리로 이동
cd clipSend

# 2. 앱 빌드 (Debug APK)
./gradlew assembleDebug

# 또는 Release APK (서명 필요)
./gradlew assembleRelease
```

빌드 완료 후 APK는 `app/build/outputs/apk/` 디렉토리에 생성됩니다.

### 1.3 오프라인 빌드 (폐쇄망)

이 프로젝트의 `gradle/wrapper/gradle-wrapper.jar`와 `build.gradle.kts`에 모든 필요한 의존성 버전이 명시되어 있습니다.
인터넷이 되는 환경에서 한 번 빌드한 후, Gradle 캐시(`~/.gradle`)를 함께 옮기면 폐쇄망에서도 빌드 가능합니다.

## APK 설치 방법

### 1. USB를 통한 설치 (권장)

```bash
# Android Studio의 device 연결 후
./gradlew installDebug

# 또는 adb 직접 사용
adb install app/build/outputs/apk/debug/app-debug.apk
```

### 1.5 APK 설치 방법 (2) 파일 탐색기를 통한 설치

- `app/build/outputs/apk/debug/app-debug.apk` 파일을 Android 기기로 복사
- 파일 관리자에서 해당 APK 파일 클릭 → 설치

### 1.6 APK 설치 방법 (3) 번들 설치 (Google Play 형식)

```bash
./gradlew bundleRelease
# aab 파일이 생성되며, Google Play Console을 통해 배포
```

## 2. 사용 방법

### 2.1 초기 설정 및 사용법

#### 앱 실행 후 초기 설정

앱을 처음 실행하면 다음 화면이 나타납니다:

- **발신 사용자**: Gmail 주소 입력 (예: `aisoki2675@gmail.com`)
- **수신 사용자**: 수신인 Gmail 주소 입력
  - **여러 명 지정**: 콤마(`,`)로 구분 (예: `a@gmail.com, b@gmail.com`)
  - **주의**: API 활성화된 계정만 발신자로 사용 가능

#### Gmail API 비밀번호 설정

- **발신 Gmail 주소**: API 활성화된 Gmail 계정
- **앱 비밀번호**: 일반 비밀번호 대신 Gmail 앱 비밀번호 사용
  - [Google 계정 보안 페이지](https://myaccount.google.com/security)에서 "앱 비밀번호" 생성
  - 기기: "메일" 선택 → 16자리 비밀번호 복사해서 입력

#### 권한 설정

처음 실행 시 다음 권한을 허용해야 합니다:
- **클립보드 접근**: 붙여넣기 기능 사용
- **오버레이 표시**: 항상 표시 오버레이 버튼
- **인터넷**: Gmail 서버 연결

#### 설정 저장

모든 정보 입력 후 **"저장"** 버튼을 눌러 설정 완료.

### 2.2 기본 흐름

1. **오버레이 버튼 활성화**
   - 앱을 최소화한 상태에서 화면 가장자리에 작은 버튼이 표시됨
   - 이 버튼을 누르면 입력 필드가 나타남

2. **텍스트 붙여넣기**
   - 오버레이에서 텍스트 필드를 누르고 **붙여넣기** 수행
   - **반드시 붙여넣기로 입력** (직접 타이핑 불가)

3. **자동 제목 설정**
   - 첫째 줄이 자동으로 메일 제목이 됨
   - 예시:
     ```
     [코드 요청] Python factorial 함수
     def factorial(n):
         return 1 if n <= 1 else n * factorial(n - 1)
     ```
     → 제목: `[코드 요청] Python factorial 함수`
     → 본문: 함수 코드

4. **발송**
   - **"발송"** 버튼 클릭 → 설정된 수신인들에게 자동으로 메일 발송
   - 전송 완료 후 입력 필드 자동 초기화

### 2.3 예시 사용 사례

**시나리오**: AI 검색에서 얻은 명령어를 팀원들에게 공유

```
Linux 시스템 로그 정리 명령어
find /var/log -name "*.log" -mtime +30 -delete
```

오버레이에 붙여넣으면:
- **제목**: `Linux 시스템 로그 정리 명령어`
- **본문**: `find /var/log -name "*.log" -mtime +30 -delete`
- **수신인**: 설정에 입력된 모든 팀원 (콤마로 구분된 주소들)

## 3. 옵션별 상세 설명

앱의 설정 화면에서 발신자, 수신인, 앱 비밀번호 등을 옵션으로 관리할 수 있습니다. 각 항목은 앱 UI 내 설명에 따릅니다.

## 4. 문서별 고유 설명

### 4.1 개요

회사의 폐쇄망 환경에서 AI 검색을 통해 얻은 코드나 명령어를 이메일로 전송할 때, 일일이 메일 앱을 켜고 작성해야 하는 불편함을 해결하기 위해 개발됨.

### 4.2 주요 기능

| 기능 | 설명 |
|---|---|
| **오버레이 버튼** | 항상 표시되는 작은 버튼 (배터리 사용량 최소화) |
| **빠른 발송** | 붙여넣기만으로 즉시 메일 발송 |
| **자동 제목 추출** | 첫 줄을 자동으로 메일 제목으로 설정 |
| **다중 수신인** | 콤마로 구분하여 여러 수신인 지정 가능 |
| **Gmail API 연동** | Google의 공식 Gmail API 사용 (안전한 인증) |
| **앱 비밀번호 지원** | 일반 비밀번호 노출 방지 |
| **히스토리 기능** | 전송된 메일 목록 확인 가능 |
| **설정 화면** | 발신자, 수신인, 비밀번호 수정 용이 |

### 4.3 기술 스택

- **언어**: Kotlin
- **UI**: Jetpack Compose
- **메일 전송**: Gmail API v1
- **인증**: OAuth 2.0 (앱 비밀번호)
- **데이터 저장**: Android SharedPreferences
- **오버레이**: WindowManager (TYPE_APPLICATION_OVERLAY)
- **최소 API**: 26 (Android 8.0)
- **타겟 API**: 34 (Android 14)

### 4.4 문제 해결

#### 1. "메일 발송 실패" 오류

**원인 및 해결**:
- 발신 Gmail 계정에 2단계 인증 활성화 여부 확인
- 앱 비밀번호가 16자리인지 확인
- [Google 계정 보안](https://myaccount.google.com/security)에서 "앱 비밀번호" 재생성

#### 2. 오버레이 버튼이 보이지 않음

**원인 및 해결**:
- 설정 → 앱 권한 → 다른 앱 위에 표시 → clipSend 활성화 확인
- 앱을 완전히 종료했을 수 있음 → 재실행

#### 3. 붙여넣기가 동작하지 않음

**원인 및 해결**:
- 설정 → 앱 권한 → 클립보드 접근 → clipSend 활성화 확인
- 오버레이 텍스트 필드를 길게 눌러 붙여넣기 메뉴 확인

#### 4. 폐쇄망 빌드 실패

**원인 및 해결**:
- Gradle 캐시가 없음 → 인터넷 연결 환경에서 한 번 빌드 후 `~/.gradle` 디렉토리 복사
- `build.gradle.kts`의 저장소(repository) 설정 확인
- 필요한 라이브러리: `androidx.compose.ui`, `com.google.api-client`

### 4.5 보안 주의사항

- **앱 비밀번호 보관**: Gmail 앱 비밀번호는 앱 내부 저장소(SharedPreferences)에 암호화되어 저장
- **클립보드 권한**: 사용자가 허용해야만 클립보드 텍스트에 접근
- 폐쇄망 사용: 공개 WiFi에서는 사용 권장하지 않음 (Gmail 인증 정보 노출 위험)
- 정기 비밀번호 변경: 팀 내 공용 발신 계정 사용 시 정기적으로 앱 비밀번호 재생성

### 4.6 라이선스

개인 프로젝트 (비상업용)

### 4.7 디렉토리 구조

```
clipSend/
├── README.md               # 이 문서
├── CLAUDE.md                # Claude Code용 프로젝트 안내 문서
├── vm-param-allinone-plan.md / vm-param-fix-plan.md  # (다른 프로젝트에서 옮겨온) 계획 메모
├── build.gradle.kts          # 최상위 Gradle 빌드 설정
├── settings.gradle.kts       # Gradle 모듈 구성
├── gradle.properties         # Gradle 속성 설정
├── gradlew / gradlew.bat     # Gradle 래퍼 실행 스크립트
├── gradle/                   # Gradle 래퍼 설정 및 버전 카탈로그(libs.versions.toml)
└── app/                      # 앱 모듈
    ├── build.gradle.kts      # 앱 모듈 Gradle 빌드 설정
    └── src/
        ├── main/             # 앱 소스 코드(Kotlin), 매니페스트, 리소스(res/)
        ├── test/             # 단위 테스트
        └── androidTest/      # 계측(instrumentation) 테스트
```

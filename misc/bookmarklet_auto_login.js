// bookmarklet_auto_login.js - 브라우저 북마클릿: 로그인 폼 자동 입력 스크립트 (여러 버전 통합, 최종본은 querySelector[type=] 방식)
// 사용법: 아래 코드를 브라우저 북마크의 URL 필드에 "javascript:" 접두로 등록 후 로그인 페이지에서 클릭

javascript:(function(){
    /* 아이디와 비밀번호를 입력하세요 */
    const myID = '사용자아이디';
    const myPW = '사용자비밀번호';

    /* 입력창 찾기 (ID나 Password 타입을 찾습니다) */
    const idField = document.querySelector('input[type="text"], input[type="email"]');
    const pwField = document.querySelector('input[type="password"]');

    if (idField) idField.value = myID;
    if (pwField) pwField.value = myPW;

    /* 로그인 버튼 자동 클릭 (사이트별 셀렉터 필요시 아래로 대체) */
    const btn = document.querySelector('button[type="submit"]');
    if (btn) btn.click();
})();

// --- 이전 버전 (클래스명 기반, 사이트별 커스터마이징 필요) ---
// javascript:(function(){
//   const idClass = '.input_id';
//   const pwClass = '.input_pw';
//   const btnClass = '.btn_login';
//   document.querySelector(idClass).value = '사용자아이디';
//   document.querySelector(pwClass).value = '사용자비밀번호';
//   document.querySelector(btnClass).click();
// })();

// --- 이전 버전 (name 속성 기반) ---
// javascript:(function(){
//   const idName = 'userid';
//   const pwName = 'password';
//   document.querySelector(`[name="${idName}"]`).value = '사용자아이디';
//   document.querySelector(`[name="${pwName}"]`).value = '사용자비밀번호';
// })();

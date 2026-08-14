# kickstart_password_hash_check.py
# 킥스타트 파일에 적힌 crypt 해시값과 추측한 비밀번호가 일치하는지 확인하는 스크립트
import crypt

# 킥스타트 파일에 적힌 해시값 (예시)
hashed_val = "$6$rounds=65536$randomsalt$hashedpassword..."

# 내가 추측하는 비밀번호
my_guess = "mypassword123"

# 확인 작업: 추측한 비번을 동일한 salt로 다시 해싱했을 때 값이 같은지 비교
salt = "$".join(hashed_val.split("$")[:4]) if hashed_val.startswith("$6$rounds=") else "$".join(hashed_val.split("$")[:3])
result = crypt.crypt(my_guess, salt)

if result == hashed_val:
    print("비밀번호 일치")
else:
    print("비밀번호 불일치")

import os
import re

base_dir = r"c:\Users\qazx2\myrepo\.git\myrepo\.claude\VM"

for root, dirs, files in os.walk(base_dir):
    if 'vendor' in dirs:
        dirs.remove('vendor')
    if 'README.md' in files:
        filepath = os.path.join(root, 'README.md')
        with open(filepath, 'r', encoding='utf-8') as f:
            content = f.read()
        
        # We look for the standard instruction:
        # 폐쇄망 환경이므로 반드시 외부 네트워크 접근 없이 로컬 패키지만을 사용하도록 `-mod=vendor` 플래그를 추가하여 빌드해야 합니다.
        #    ```bash
        #    go build -mod=vendor -o <name> [args]
        #    ```
        
        # Or similar blocks
        
        # A broader regex that replaces go build -mod=vendor inside bash blocks under "바이너리 빌드" or similar
        
        # Let's just do a simpler string replacement for the text that we know we put in the previous checkpoints.
        
        original_text = "폐쇄망 환경이므로 반드시 외부 네트워크 접근 없이 로컬 패키지만을 사용하도록 `-mod=vendor` 플래그를 추가하여 빌드해야 합니다.\n   ```bash\n   go build -mod=vendor"
        
        new_text = "폐쇄망 환경이므로 반드시 외부 네트워크 접근 없이 로컬 패키지(`vendor`)만을 사용하도록, 빌드 코드가 미리 구성된 `setup.sh` 스크립트를 실행해야 합니다.\n   ```bash\n   bash setup.sh\n   # (또는 ./setup.sh)"
        
        # Let's try replacing the specific go build command with bash setup.sh
        # We will use regex to find go build -mod=vendor ... inside bash block
        pattern = re.compile(r"폐쇄망 환경이므로 반드시 외부 네트워크 접근 없이 로컬 패키지만을 사용하도록 `-mod=vendor` 플래그를 추가하여 빌드해야 합니다\.\s*```bash\s*go build -mod=vendor.*?\n\s*```", re.DOTALL)
        
        replacement = r"폐쇄망 환경이므로 반드시 외부 네트워크 접근 없이 로컬 패키지(`vendor`)만을 사용하도록, 빌드 코드가 미리 구성된 `setup.sh` 스크립트를 실행해야 합니다.\n   ```bash\n   bash setup.sh\n   ```"
        
        new_content, count = pattern.subn(replacement, content)
        
        if count > 0:
            with open(filepath, 'w', encoding='utf-8') as f:
                f.write(new_content)
            print(f"Updated: {filepath}")
        else:
            # Maybe it doesn't have the exact text. Let's do a fallback replace for just go build -mod=vendor
            fallback_pattern = re.compile(r"```bash\s*go build -mod=vendor -o ([^\s]+)(.*?)\n\s*```", re.DOTALL)
            def fallback_repl(match):
                return "```bash\n   bash setup.sh\n   ```"
            
            new_content2, count2 = fallback_pattern.subn(fallback_repl, content)
            if count2 > 0:
                with open(filepath, 'w', encoding='utf-8') as f:
                    f.write(new_content2)
                print(f"Updated (fallback): {filepath}")


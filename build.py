import os
import subprocess
import re

base_dir = os.path.dirname(__file__)
src_path = os.path.join(base_dir, 'src', 'awx_host_checker.js')
dist_dir = os.path.join(base_dir, 'dist')
os.makedirs(dist_dir, exist_ok=True)
dist_path = os.path.join(dist_dir, 'bookmarklet.txt')

def strip_comments(code):
    result = []
    i = 0
    l = len(code)
    while i < l:
        c = code[i]
        if c in ('"', "'", '`'):
            q = c
            result.append(c)
            i += 1
            while i < l:
                cur = code[i]
                result.append(cur)
                if cur == '\\':
                    i += 1
                    if i < l:
                        result.append(code[i])
                elif cur == q:
                    break
                i += 1
            i += 1
        elif c == '/' and i + 1 < l and code[i+1] == '/':
            i += 2
            while i < l and code[i] != '\n':
                i += 1
            result.append('\n')
            if i < l:
                i += 1
        elif c == '/' and i + 1 < l and code[i+1] == '*':
            i += 2
            while i + 1 < l and not (code[i] == '*' and code[i+1] == '/'):
                i += 1
            i += 2
            result.append(' ')
        else:
            result.append(c)
            i += 1
    return ''.join(result)

def minify_to_bookmarklet(src_file, out_file):
    with open(src_file, 'r', encoding='utf-8') as f:
        code = f.read()

    # 1. Strip all JavaScript comments cleanly without touching string literals
    clean = strip_comments(code)

    # 2. Trim trailing/leading whitespace per line, ignore completely empty lines
    lines = [line.strip() for line in clean.split('\n')]
    non_empty = [line for line in lines if line]

    # 3. Join with newline to preserve ASI (Automatic Semicolon Insertion)
    clean_code = '\n'.join(non_empty)

    # 4. Remove HTML comments if any
    clean_code = re.sub(r'<!--.*?-->', '', clean_code)

    # 5. URL-encode dangerous characters for bookmarklet URLs:
    # '%' -> '%25', '#' -> '%23', '\n' -> '%0A'
    # Note: replacing '%' first is critical so '%23'/'%0A' are not double-encoded!
    encoded = clean_code.replace('%', '%25').replace('#', '%23').replace('\r', '').replace('\n', '%0A')

    url = 'javascript:' + encoded

    with open(out_file, 'w', encoding='utf-8') as f:
        f.write(url)

    print(f"Built {os.path.basename(out_file)}! Original: {len(code):,} chars, Bookmarklet: {len(url):,} chars")

print(">>> [1/2] Building bookmarklet dist files...")
minify_to_bookmarklet(src_path, dist_path)
prev_src_path = os.path.join(base_dir, 'src', 'awx_host_checker_prev.js')
prev_dist_path = os.path.join(dist_dir, 'bookmarklet_prev.txt')
if os.path.exists(prev_src_path):
    minify_to_bookmarklet(prev_src_path, prev_dist_path)

print(">>> [2/2] Compiling Go cross-checker-agent binary...")
agent_src_dir = os.path.join(base_dir, 'agent')
agent_exe_path = os.path.join(dist_dir, 'cross-checker-agent.exe')
if os.path.exists(agent_src_dir) and any(f.endswith('.go') for f in os.listdir(agent_src_dir)):
    res = subprocess.run(['go', 'build', '-o', agent_exe_path, './agent'], cwd=base_dir, capture_output=True, text=True)
    if res.returncode == 0:
        print(f"Built binary: {agent_exe_path} ({os.path.getsize(agent_exe_path):,} bytes)")
    else:
        print(f"Go build failed:\n{res.stderr}")
else:
    print("Agent source not yet ready, skipping Go compilation.")

print(">>> Build finished successfully.")

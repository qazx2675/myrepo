import os
import subprocess
import urllib.parse
import re

base_dir = os.path.dirname(__file__)
src_path = os.path.join(base_dir, 'src', 'awx_host_checker.js')
dist_dir = os.path.join(base_dir, 'dist')
os.makedirs(dist_dir, exist_ok=True)
dist_path = os.path.join(dist_dir, 'bookmarklet.txt')

def minify_to_bookmarklet(src_file, out_file):
    with open(src_file, 'r', encoding='utf-8') as f:
        code = f.read()

    lines = code.split('\n')
    clean_lines = []
    for line in lines:
        s = line.strip()
        if not s or s.startswith('//'):
            continue
        clean_lines.append(s)

    minified = ' '.join(clean_lines)
    minified = re.sub(r'\s+', ' ', minified)
    # Remove HTML comments
    minified = re.sub(r'<!--.*?-->', '', minified)
    url = 'javascript:' + minified

    with open(out_file, 'w', encoding='utf-8') as f:
        f.write(url)

    print(f"Built {os.path.basename(out_file)}! Original: {len(code)} chars, Bookmarklet: {len(url)} chars")

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

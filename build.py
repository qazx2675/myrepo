import os
import urllib.parse
import re

src_path = os.path.join(os.path.dirname(__file__), 'src', 'awx_host_checker.js')
dist_dir = os.path.join(os.path.dirname(__file__), 'dist')
os.makedirs(dist_dir, exist_ok=True)
dist_path = os.path.join(dist_dir, 'bookmarklet.txt')

with open(src_path, 'r', encoding='utf-8') as f:
    code = f.read()

lines = code.split('\n')
clean_lines = []
for line in lines:
    s = line.strip()
    if s.startswith('//'):
        continue
    clean_lines.append(s)

minified = ' '.join(clean_lines)
minified = re.sub(r'\s+', ' ', minified)

url = 'javascript:' + urllib.parse.quote(minified)

with open(dist_path, 'w', encoding='utf-8') as f:
    f.write(url)

print(f"Built bookmarklet! Original: {len(code)} chars, Minified: {len(minified)} chars, URL: {len(url)} chars")


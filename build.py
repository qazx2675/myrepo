import os
import urllib.parse
import re

src_path = os.path.join(os.path.dirname(__file__), 'src', 'awx_host_checker.js')
dist_dir = os.path.join(os.path.dirname(__file__), 'dist')
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

minify_to_bookmarklet(src_path, dist_path)
prev_src_path = os.path.join(os.path.dirname(__file__), 'src', 'awx_host_checker_prev.js')
prev_dist_path = os.path.join(dist_dir, 'bookmarklet_prev.txt')
if os.path.exists(prev_src_path):
    minify_to_bookmarklet(prev_src_path, prev_dist_path)


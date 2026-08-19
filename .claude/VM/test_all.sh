#!/bin/bash
sshpass -p asdf ssh -o StrictHostKeyChecking=no root@192.168.0.60 '
  find "/root/VM" -name setup.sh | while read script; do
    dir=$(dirname "$script")
    echo "----------------------------------------"
    echo "Building in $dir..."
    cd "$dir"
    bash setup.sh
  done
'

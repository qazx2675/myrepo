#!/usr/bin/env bash
# check_dns_type.sh — /etc/resolv.conf에 설정된 DNS 서버들의 소프트웨어 종류를
# SSH 접속 없이 CHAOS 클래스 쿼리(version.bind/version.server)로 빠르게 확인한다.
# BIND는 보통 실제 버전 문자열을 반환하고, PowerDNS/dnsmasq는 각자 다른 응답을 준다.
# 방화벽/설정으로 CHAOS 쿼리를 막아둔 서버(Google DNS 등)는 응답이 비어있을 수 있다 — 그 경우
# 서버 담당자에게 직접 확인해야 한다.
set -euo pipefail

for ns in $(awk '/^nameserver/{print $2}' /etc/resolv.conf); do
    echo "== $ns =="
    version=$(dig +short +time=2 +tries=1 version.bind chaos txt "@$ns" 2>/dev/null || true)
    if [ -z "$version" ]; then
        version=$(dig +short +time=2 +tries=1 version.server chaos txt "@$ns" 2>/dev/null || true)
    fi
    if [ -n "$version" ]; then
        echo "  버전/식별 문자열: $version"
    else
        echo "  (CHAOS 쿼리 응답 없음 — 서버가 막아뒀거나 SSH로 직접 확인 필요)"
    fi
done

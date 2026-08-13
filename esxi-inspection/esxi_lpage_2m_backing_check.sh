# ESXi VM Large Page(2MB) backing 상태 pdsh 기반 일괄 조회

(
echo "HOSTNAME WorldID 2m_backings 2m_backs_succeeded 2m_locating_succeeded 2m_mappings"
pdsh -w ^kdh.txt "for wid in \$(esxcli vm process list | grep 'World ID' | awk -F: '{print \$2}'); do
  vsish -e get /memory/lpage/vmLPage/\$wid
done"
) 

# pwsh 경유 버전
pwsh -c "echo 'WorldID 2m_backings 2m_backs_succeeded 2m_locating_succeeded 2m_mappings'; for wid in \$(esxcli vm process list | grep 'World ID' | awk -F: '{print \$2}');"

# ESXi NUMA Rebalance / Mem MinFreePct 점검

esxcli system settings advanced list -o /Numa/Rebalance
esxcli system settings advanced list -o /Mem/MinFreePct
esxcli system settings advanced list | grep -i minfree
esxcli system settings advanced list -o /Mem/MinFreePct 2>&1
vsish -e get /memory/comprehensive | grep -i free

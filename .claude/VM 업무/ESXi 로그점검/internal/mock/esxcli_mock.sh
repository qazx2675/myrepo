#!/bin/bash
# Mock script for ESXi esxcli command on Rocky Linux
case "$*" in
    *"storage core device list"*)
        echo "naa.6000000000000000000000000001234"
        echo "   Display Name: Mock Storage Device"
        echo "   Status: on"
        ;;
    *"network nic list"*)
        echo "Name    PCI Device    Driver  Admin Status  Link Status  Speed  Duplex  MAC Address         MTU  Description"
        echo "------  ------------  ------  ------------  -----------  -----  ------  -----------------  ----  -------------------------------------------------"
        echo "vmnic0  0000:01:00.0  ixgben  Up            Up           10000  Full    00:50:56:01:02:03  1500  Intel Corporation 82599 10 Gigabit Dual Port Network Connection"
        ;;
    *)
        echo "esxcli mock: Command not implemented ($*)"
        ;;
esac

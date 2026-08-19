#!/bin/bash
# Mock script for ESXi localcli command on Rocky Linux
case "$*" in
    *"hardware ipmi sel list"*)
        if [ -f "/var/run/log/ipmi/sel" ]; then
            cat "/var/run/log/ipmi/sel"
        else
            echo "No IPMI SEL entries found."
        fi
        ;;
    *)
        echo "localcli mock: Command not implemented ($*)"
        ;;
esac

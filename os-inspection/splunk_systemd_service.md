# Splunk Enterprise systemd 서비스 파일

```
[Unit]
Description=Systemd service file for Splunk Enterprise
After=network.target

[Service]
Type=forking
RemainAfterExit=False
User=root
Group=root
ExecStart=/opt/splunk/bin/splunk start --no-prompt -...
```

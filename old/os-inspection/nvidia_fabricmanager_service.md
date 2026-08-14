# NVIDIA Fabric Manager systemd 서비스 점검/재기동

## 1. 서비스 파일 확인/수정
```
vi /usr/lib/systemd/system/nvidia-fabricmanager.service
```
[Service] 섹션 PIDFile 값:
```
PIDFile=
PIDFile=/run/nvidia-fabricmanager/nv-fabricmanager.pid
```

## 2. 재기동
```
systemctl daemon-reload
systemctl restart nvidia-fabricmanager
systemctl status nvidia-fabricmanager
```

## 3. 로그 확인
```
sudo journalctl -u nvidia-fabricmanager.service -b --no-pager | tail -n 50
```

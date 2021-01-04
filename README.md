# IP KVM

## Hardware requiments
- Raspberry Pi 4 Model B or Compute Module 4
  - At least 1GB RAM
- USB-HDMI capture module
  - UVC compatible

## Software requiments
- Raspberry Pi OS (based on Debian buster)
- Go (checked on 1.15)

## Install
Settings for USB OTG
```bash
echo "dtoverlay=dwc2" | sudo tee -a /boot/config.txt
cat << 'EOS' | sudo tee /etc/modules-load.d/ipkvm.conf
dwc2
libcomposite
EOS
```

Disable USB suspend
```bash
sudo sed -i -e '1 s/$/ usbcore.autosuspend=-1/' /boot/cmdline.txt
sudo reboot
```

Install GStreamer
```bash
sudo apt update
sudo apt install gstreamer1.0-omx gstreamer1.0-alsa gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad gstreamer1.0-tools libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev
```

Install ipkvm
```bash
mkdir -p $(go env GOPATH)/src/github.com/msawahara/ipkvm
git clone https://github.com/msawahara/ipkvm.git $(go env GOPATH)/src/github.com/msawahara/ipkvm
(cd $(go env GOPATH)/src/github.com/msawahara/ipkvm && go install)
```

Register systemd service
```bash
cat << EOS | sudo tee /etc/systemd/system/ipkvm.service
[Unit]
Description=KVM over IP service
After=network.target

[Service]
Type=simple
ExecStartPre=$(which modprobe) uvcvideo
ExecStart=$(go env GOPATH)/bin/ipkvm
WorkingDirectory=$(go env GOPATH)/src/github.com/msawahara/ipkvm

[Install]
WantedBy=multi-user.target
EOS
```
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now ipkvm
```

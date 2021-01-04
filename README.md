# IP KVM

## Features
- Remote Video
  - Video and Audio capture from HDMI
  - Using WebRTC (H.264 + Opus)
  - Hardware enconding (using Gstreamer OpenMax plugins)
- Remote Control
  - Connect to target device via USB
  - Supported Functions
    - Keyboard
    - Mouse
    - Touch screen
    - Gamepad
  - Keyboard and mouse are support boot protocol
  - Mouse supports absolute and relative position reporting
  - Gamepad input on your browse using the Gamepad API

## Hardware requiments
- Raspberry Pi 4 Model B or Compute Module 4
  - At least 1GB RAM
- USB-HDMI capture module
  - UVC compatible

## Software requiments
- Raspberry Pi OS (based on Debian buster)
- Go (checked on 1.15)

## Implementation status
| Feature | Status | Remarks |
| --- | --- | --- |
| Video | work, but need improvement | There is a problem with resoltuions 1920x1080 and 800x600. (Resolution must be a multiple of 16)|
| Audio | OK | |
| Keyboard | OK | some key codes are undefined |
| Mouse | OK | |
| Touch screen | OK | |
| Gamepad | work, but need improvement | Buttons and axes are working fine. Hat switch is not tested and will not working. |

## Install
### Preparation
```bash
# Enable USB OTG
echo "dtoverlay=dwc2" | sudo tee -a /boot/config.txt
cat << 'EOS' | sudo tee /etc/modules-load.d/ipkvm.conf
dwc2
libcomposite
EOS

# Disable USB suspend
sudo sed -i -e '1 s/$/ usbcore.autosuspend=-1/' /boot/cmdline.txt

# Install GStreamer
sudo apt update
sudo apt install gstreamer1.0-omx gstreamer1.0-alsa gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad gstreamer1.0-tools libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev

# Reboot to apply settings
sudo reboot
```


### Install ipkvm
```bash
# Download and install
mkdir -p $(go env GOPATH)/src/github.com/msawahara/ipkvm
git clone https://github.com/msawahara/ipkvm.git $(go env GOPATH)/src/github.com/msawahara/ipkvm
(cd $(go env GOPATH)/src/github.com/msawahara/ipkvm && go install)

# Register service unit
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

# Start service
sudo systemctl daemon-reload
sudo systemctl enable --now ipkvm
```

The KVM console can be accessed at `http://<ip-addr>:1323/`.
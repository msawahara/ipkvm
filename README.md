# IP KVM

## Install

USB OTG用モジュールの設定
```bash
echo "dtoverlay=dwc2" | sudo tee -a /boot/config.txt
cat << 'EOS' | sudo tee /etc/modules-load.d/ipkvm.conf
dwc2
libcomposite
uvcvideo
EOS
```

gstreamer
```bash
apt update
apt install gstreamer1.0-omx gstreamer1.0-alsa gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad gstreamer1.0-tools libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev
```

disable usb suspend
```bash
sudo sed -i -e '1 s/$/ usbcore.autosuspend=-1/' /boot/cmdline.txt
```
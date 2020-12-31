# ipkvm

## Install

USB OTG用モジュールの設定
```bash
echo "dtoverlay=dwc2" | sudo tee -a /boot/config.txt
cat << 'EOS' | sudo tee /etc/modules-load.d/usb-otg.conf
dwc2
libcomposite
EOS
```

ALSAの設定
```bash
cat << 'EOS' | sudo tee /etc/asound.conf
pcm.!default {
type asym
playback.pcm "hw:CARD=MS2109,DEV=0"
capture.pcm "dsnoop:CARD=MS2109,DEV=0"
}
EOS
```
package usbgadget

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type USBGadgetDevice struct {
	ConfigDir string
	Device    string
}

type USBGadgetMouse struct {
	X      int
	Y      int
	Device USBGadgetDevice
}

type USBGadgetKeyboard struct {
	Device USBGadgetDevice
}

type USBGadgetFunction struct {
	Type             string
	Protocol         int
	SubClass         int
	ReportLength     int
	ReportDescriptor []byte
}

type USBGadgetStringDescriptor struct {
	SerialNumber string
	Manufacturer string
	Product      string
}

type USBGadget struct {
	Name          string
	MaxPacketSize int
	IdVendor      int
	IdProduct     int
	UsbVersion    int
	DeviceVesion  int
	Strings       map[int]*USBGadgetStringDescriptor
	Functions     map[string]*USBGadgetFunction
}

func (d *USBGadgetDevice) Get() (string, error) {
	if len(d.Device) != 0 {
		return d.Device, nil
	}
	data, _ := ioutil.ReadFile(d.ConfigDir + "/dev")
	ids := strings.Split(strings.TrimRight(string(data), "\n"), ":")
	major, _ := strconv.Atoi(ids[0])
	minor, _ := strconv.Atoi(ids[1])

	files, _ := ioutil.ReadDir("/dev")
	for _, file := range files {
		if (file.Mode() & os.ModeCharDevice) == 0 {
			continue
		}
		name := "/dev/" + file.Name()
		stat := syscall.Stat_t{}
		syscall.Stat(name, &stat)
		majorDev := int64(stat.Rdev / 256)
		minorDev := int64(stat.Rdev % 256)
		if major == int(majorDev) && minor == int(minorDev) {
			d.Device = name
			break
		}
	}

	if len(d.Device) == 0 {
		return "", errors.New("device not found")
	}

	return d.Device, nil
}

func (k *USBGadgetKeyboard) Send(code []int, altKey, ctrlKey, metaKey, shiftKey bool) {
	dev, err := k.Device.Get()
	if err != nil {
		return
	}

	modifier := byte(0)

	if ctrlKey {
		modifier |= 1
	}
	if shiftKey {
		modifier |= 2
	}
	if altKey {
		modifier |= 4
	}
	if metaKey {
		modifier |= 8
	}

	report := make([]byte, 8)
	report[0] = modifier // Modifier
	report[1] = 0        // Reserved
	for i, c := range code {
		if i > 5 {
			break
		}
		report[2+i] = byte(c) // Keycodes
	}

	ioutil.WriteFile(dev, report, 0600)
}

func (m *USBGadgetMouse) Send(buttons, x, y int) {
	dev, err := m.Device.Get()
	if err != nil {
		return
	}

	dx := int8(math.Max(math.Min(float64(x-m.X), 127), -128))
	dy := int8(math.Max(math.Min(float64(y-m.Y), 127), -128))

	report := make([]byte, 3)
	report[0] = byte(buttons & 0x07)
	report[1] = byte(dx)
	report[2] = byte(dy)

	ioutil.WriteFile(dev, report, 0600)

	m.X = x
	m.Y = y
}

func (g USBGadget) AddMouse(name string) *USBGadgetMouse {
	f := new(USBGadgetFunction)
	f.Type = "hid"
	f.Protocol = 2
	f.SubClass = 1
	f.ReportLength = 8
	f.ReportDescriptor = []byte{
		0x05, 0x01, 0x09, 0x02, 0xa1, 0x01, 0x09, 0x01, 0xa1, 0x00, 0x05, 0x09, 0x19, 0x01, 0x29, 0x03,
		0x15, 0x00, 0x25, 0x01, 0x95, 0x03, 0x75, 0x01, 0x81, 0x02, 0x95, 0x01, 0x75, 0x05, 0x81, 0x01,
		0x05, 0x01, 0x09, 0x30, 0x09, 0x31, 0x15, 0x81, 0x25, 0x7f, 0x75, 0x08, 0x95, 0x02, 0x81, 0x06,
		0xc0, 0xc0,
	}
	g.AddFunction(name, f)

	m := new(USBGadgetMouse)
	m.Device.ConfigDir = "/sys/kernel/config/usb_gadget/" + g.Name + "/configs/c.1/" + fmt.Sprintf("%s.%s", f.Type, name)
	return m
}

func (g USBGadget) AddKeyboard(name string) *USBGadgetKeyboard {
	f := new(USBGadgetFunction)
	f.Type = "hid"
	f.Protocol = 1
	f.SubClass = 1
	f.ReportLength = 8
	f.ReportDescriptor = []byte{
		0x05, 0x01, 0x09, 0x06, 0xa1, 0x01, 0x05, 0x07, 0x19, 0xe0, 0x29, 0xe7, 0x15, 0x00, 0x25, 0x01,
		0x75, 0x01, 0x95, 0x08, 0x81, 0x02, 0x95, 0x01, 0x75, 0x08, 0x81, 0x03, 0x95, 0x05, 0x75, 0x01,
		0x05, 0x08, 0x19, 0x01, 0x29, 0x05, 0x91, 0x02, 0x95, 0x01, 0x75, 0x03, 0x91, 0x03, 0x95, 0x06,
		0x75, 0x08, 0x15, 0x00, 0x25, 0x65, 0x05, 0x07, 0x19, 0x00, 0x29, 0x65, 0x81, 0x00, 0xc0,
	}
	g.AddFunction(name, f)

	k := new(USBGadgetKeyboard)
	k.Device.ConfigDir = "/sys/kernel/config/usb_gadget/" + g.Name + "/configs/c.1/" + fmt.Sprintf("%s.%s", f.Type, name)
	return k
}

func (g USBGadget) AddFunction(name string, f *USBGadgetFunction) {
	g.Functions[name] = f
}

func (g USBGadget) Start() {
	gadgetDir := "/sys/kernel/config/usb_gadget/" + g.Name

	os.Mkdir(gadgetDir, 0755)
	ioutil.WriteFile(gadgetDir+"/bMaxPacketSize0", []byte(strconv.Itoa(g.MaxPacketSize)), 0644)
	ioutil.WriteFile(gadgetDir+"/idVendor", []byte(strconv.Itoa(g.IdVendor)), 0644)
	ioutil.WriteFile(gadgetDir+"/idProduct", []byte(strconv.Itoa(g.IdProduct)), 0644)
	ioutil.WriteFile(gadgetDir+"/bcdUSB", []byte(strconv.Itoa(g.UsbVersion)), 0644)
	ioutil.WriteFile(gadgetDir+"/bcdDevice", []byte(strconv.Itoa(g.DeviceVesion)), 0644)

	for l, s := range g.Strings {
		stringsDir := gadgetDir + "/strings/" + fmt.Sprintf("0x%04x", l)

		os.Mkdir(stringsDir, 0755)
		ioutil.WriteFile(stringsDir+"/serialnumber", []byte(s.SerialNumber), 0644)
		ioutil.WriteFile(stringsDir+"/manufacturer", []byte(s.Manufacturer), 0644)
		ioutil.WriteFile(stringsDir+"/product", []byte(s.Product), 0644)
	}

	configDir := gadgetDir + "/configs/c.1"
	os.Mkdir(configDir, 0755)

	for n, f := range g.Functions {
		functionDir := gadgetDir + "/functions/" + fmt.Sprintf("%s.%s", f.Type, n)

		os.Mkdir(functionDir, 0755)
		ioutil.WriteFile(functionDir+"/protocol", []byte(strconv.Itoa(f.Protocol)), 0644)
		ioutil.WriteFile(functionDir+"/subclass", []byte(strconv.Itoa(f.SubClass)), 0644)
		ioutil.WriteFile(functionDir+"/report_length", []byte(strconv.Itoa(f.ReportLength)), 0644)
		ioutil.WriteFile(functionDir+"/report_desc", f.ReportDescriptor, 0644)

		os.Symlink(functionDir, configDir+fmt.Sprintf("/%s.%s", f.Type, n))
	}

	files, _ := ioutil.ReadDir("/sys/class/udc")
	udc := filepath.Base(files[0].Name())

	ioutil.WriteFile(gadgetDir+"/UDC", []byte(udc), 0644)
}

func (g USBGadget) Stop() {
	gadgetDir := "/sys/kernel/config/usb_gadget/" + g.Name
	configDir := gadgetDir + "/configs/c.1"

	// detach from usb device controller
	ioutil.WriteFile(gadgetDir+"/UDC", []byte("\n"), 0644)

	// remove functions
	for n, f := range g.Functions {
		functionDir := gadgetDir + "/functions/" + fmt.Sprintf("%s.%s", f.Type, n)
		os.Remove(configDir + fmt.Sprintf("/%s.%s", f.Type, n))
		os.RemoveAll(functionDir)
	}

	// remove config
	os.RemoveAll(configDir)

	// remove gadget
	os.RemoveAll(gadgetDir)
}

func NewUSBGadget(name string) *USBGadget {
	g := new(USBGadget)
	g.Name = name
	g.MaxPacketSize = 64
	g.IdVendor = 0x1d6b     // The Linux Foundation
	g.IdProduct = 0x0104    // Multifunction Composite Gadget
	g.UsbVersion = 0x0200   // USB 2.0
	g.DeviceVesion = 0x0100 // v.1.0.0
	g.Strings = map[int]*USBGadgetStringDescriptor{}
	g.Strings[0x0409] = &USBGadgetStringDescriptor{
		SerialNumber: "00000000",
		Manufacturer: "The Linux Foundation",
		Product:      "Generic USB Device",
	}
	g.Functions = map[string]*USBGadgetFunction{}
	return g
}
